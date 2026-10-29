# Lamport mutual exclusion — verified experiments

**Executed:** 2026-10-29, using real Docker Go nodes. **Result:** all seven scenarios passed, twice; the exports below are the second pass, after removing the stale compiled executable from the source snapshot. No active runs were left behind. The implementation is a teaching protocol, not a production distributed lock.

Protocol: [open in local DistVis](http://127.0.0.1:3000/#protocol/4e6d7318-63ad-4ef3-856d-700b5598a22f/experiments).

Implementation, state definitions, discussion answers, and manual reproduction: [protocol README](../../protocols/lamport_mutex/README.md). Executable steps and assertions [conditions checked against the events]: [scenarios.json](../../protocols/lamport_mutex/scenarios.json).

## Common configuration and provenance

- Three nodes, Docker, 80 ms fixed latency, 128 KiB/s bandwidth per directed link, seed 42.
- All listed runs used protocol revision **2**, source SHA-256 [the saved file-set fingerprint] `e3a8b4376960b9731c1005b1cbc387e0df5a03b20e420d745a6eedf456e3f99b`.
- Every exported Go file was compared byte-for-byte with the current local Go implementation and tests. The platform is now at revision **3** after adding the binary-ignore rule; its Go files are identical to the tested revision 2.
- [Machine-readable summary](summary-2e6dce4a-bee5-450f-b103-1cec9cf89d70.json) contains run IDs, history links, event sequence numbers, measured times, faults, and final states.
- The exported JSON files preserve complete events and code snapshots; previous experiments and the original report exports remain unchanged.

Run all scenarios from the repository root:

```sh
node scripts/scenarios.mjs run lamport_mutex
```

Export and independently verify the newest completed run of every scenario:

```sh
node scripts/export-lamport-evidence.mjs 4e6d7318-63ad-4ef3-856d-700b5598a22f
```

The exporter refuses to overwrite an existing run export. To generate a new evidence set, first execute new runs. The protocol ID is machine-local; use the ID shown by the scenario runner if reproducing on another installation.

## Results

| Scenario | Result | Algorithm messages | Exported run |
| --- | --- | ---: | --- |
| A: node-1/node-3 concurrent acquisitions | Both completed; node-1 entered first; no overlap | 12 | [2e6dce4a](runs/2e6dce4a-bee5-450f-b103-1cec9cf89d70/run.json) |
| B: idle node-3 crashes | node-2 stayed waiting for node-3 | 3 attempts | [6b686149](runs/6b686149-62d5-4e0b-ade7-97474d444207/run.json) |
| C: node-2 ↔ node-3 blocked, then healed | node-2 did not enter before or after healing | 3 attempts | [758cd9c8](runs/758cd9c8-819b-4df1-952c-f7d937c74d1f/run.json) |
| D control: one Lamport acquisition | Completed; 2 Request + 2 Ack + 2 Release | 6 | [c6b766e8](runs/c6b766e8-a690-4bd1-b163-0aab39d02b50/run.json) |
| D variant: one Ricart–Agrawala acquisition | Completed; 2 Request + 2 Ack; no Release | 4 | [a467ce4b](runs/a467ce4b-bce7-4c4f-8e33-3973fa0f6e7f/run.json) |
| Regression: 15000 ms hold | Accepted promptly; held and completed without application timeout | 6 | [b39ae41e](runs/b39ae41e-1de8-429a-bb18-33b01651d13a/run.json) |
| Regression: Ricart–Agrawala repeated contention | Three nodes completed three acquisitions each; no overlap | 36 | [03509414](runs/03509414-e905-4a76-9f6a-ff8ef650b214/run.json) |

“Algorithm messages” counts outgoing RPC request envelopes [messages carrying calls to `MutexService.Request`, `.Ack`, or `.Release`]. Automatic RPC responses and cancellation messages are excluded. In failure cases, the count includes failed transmission attempts. The final `messagesSent` on **each node**, including nodes that only replied, matched the recorded sends in every export.

## A. Normal concurrent acquisition

`Acquire(holdMs=2000)` was submitted concurrently to node-1 and node-3; both requests received timestamp 1, so the numeric node ID broke the tie. Node-1 waited **170.976 ms** and entered first; node-3 waited **2259.764 ms**. The entire recorded history had at most one critical node at every state event, both requesters completed once, and every queue emptied. This matched the expected ordering and mutual exclusion [no simultaneous execution of the protected section], with **12 algorithm messages** across the two cycles.

The application calls returned acceptance after approximately **4.21 ms** each; those returns did not mean the critical sections had finished. First entry is event **67** (node-1); its exit is event **76**; node-3 enters at **94** and exits at **104**. These event numbers are specific to this exported run.

## B. Crash of an uninvolved peer

Node-3 was crashed while idle, and `Acquire` was then accepted on node-2 in approximately **6.44 ms**. Node-2 received node-1's permission but stayed at `entries=0`, `acksHave=1`, `acksNeed=2`, and `missingAcks=["node-3"]`. After about three seconds, `lastError` reported `MutexService.Request to node-3: no reply within 3s; remote outcome unknown`; observation continued beyond ten seconds without any entry. This matched the algorithm's requirement for every peer's acknowledgement; the application did not misleadingly time out after acceptance.

This corrects the old combined report's node names: the original `report/lamportmutex_scenB.json` also used **node-3 crashed / node-2 requesting**. A finite experiment does not observe “forever”; the implementation's unconditional wait and absence of retransmission explain why the request cannot finish while that acknowledgement remains missing.

## C. Blocked link followed by healing

Both directions of the node-2/node-3 link were blocked before node-2 requested entry. Node-2 remained waiting, identified node-3 in `missingAcks` and `lastError`, and never entered the critical section. The links were healed at event **37**, but the operation remained blocked during the following four-second observation because the discarded request was not sent again. This matched the expected limitation: restoring connectivity does not itself supply a permission that the protocol never obtained.

This is a direct-link failure, not a complete network split: node-1 retained connections to both other nodes. The protocol sends directly to peers and does not route the missing request through node-1.

## D. Design variant: remove the explicit release round correctly

In the default mode, one node-1 acquisition produced **2 requests, 2 acknowledgements, and 2 releases**, giving `3(n−1)=6` for three nodes. In a fresh run, all nodes were configured to `ricart-agrawala` before requesting; the same acquisition produced **2 requests and 2 acknowledgements with zero releases**, giving `2(n−1)=4`. The alternative also removes the remote-request queue and queue-head entry condition, so a missing explicit release cannot leave a stale queue entry blocking progress. Deferred acknowledgements [permission replies sent only after the current holder leaves] replace the release round; the optimization saves messages but still requires every peer to reply.

The additional three-node contention run completed **nine acquisitions**, with **36 algorithm messages**, no release calls, and no overlapping critical sections. It verifies that the alternative also handles competing requests rather than merely passing the one-request count demonstration.

## Timeout and reporting regression checks

A **15000 ms hold** returned acceptance in **7.49 ms** and subsequently completed with no application error. Node-1 remained critical after eleven seconds; the recorded interval between critical entry and exit was approximately **14.991 seconds**. These are coordinator observation times [when DistVis observed the reports], not a direct measurement of the Go timer; scheduling and transport make them slightly different from the requested hold duration.

Every state report's `reportSeq` increased on its node in all seven runs. All final message counters matched recorded outgoing algorithm calls; no non-fault run had an outgoing-call error. These checks address the original bugs where an old snapshot could appear after a newer one and an acknowledgement sender could keep displaying a zero counter.

## Local verification

- `go test -race -count=3 ./...`: passed the full test suite three times.
- `go vet ./...`: passed.
- `go test -race -run 'TestConcurrentCyclesAndMessageCounts|TestReportPublicationHoldsStateLock' -count=20 ./...`: passed twenty repetitions of the concurrency/report checks.
- All seven real Docker scenarios passed twice; the final evidence set above excludes the obsolete compiled binary from the source snapshot.
- The separate export verifier checked application results, per-node report ordering, absence of overlapping critical sections, per-node counter/event agreement, expected message totals, and exact Go source identity.

## Scope and discussion answers

**Why not ignore a crashed peer?** This algorithm relies on permission from every other member. Skipping a peer would change the condition that prevents simultaneous entry. The crash scenario intentionally demonstrates loss of progress while preserving exclusion, not fault-tolerant recovery.

**Why do RPC response arrows not count toward `3(n−1)`?** That formula counts protocol-level Request, Ack, and Release messages. Here each is implemented as an RPC [remote method invocation], whose library sends an additional empty response. Count algorithm request envelopes consistently for both implementations; do not count all diagram arrows as textbook messages.

**How does this relate to classic Lamport?** Classic Lamport normally uses immediate replies with reliable FIFO communication [messages on each directed connection stay in order]. This lab-oriented implementation retains deferred permissions as described in the handout while also showing the queue and explicit releases. The Ricart–Agrawala variant removes both the release traffic and the queue dependency; this distinction is documented rather than treating the two formulations as identical.

**What is not verified or implemented?** Crash/recovery safety, persistent state, automatic retries, changing group membership, and general progress on lossy networks are outside scope. A restarted process forgets its queue, clock, and selected mode. These successful tests demonstrate specific executions and regressions, not a proof covering every possible schedule; use fresh runs after the fault demonstrations.
