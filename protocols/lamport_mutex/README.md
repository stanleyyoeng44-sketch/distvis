# Lamport mutual exclusion

This protocol demonstrates **mutual exclusion** [at most one node executes the protected section at a time]. It addresses [Lab 1 §4.3](../../docs/LAB1.en.md#43-lamport-mutual-exclusion), including the comparison with Ricart–Agrawala [the related algorithm that avoids explicit release messages].

## Start here

1. Start DistVis with `npm start` and make sure Docker is available.
2. From the repository root, run `node scripts/scenarios.mjs run lamport_mutex` to create/reuse the protocol and its experiments, synchronize the source, and execute all seven scenarios in [scenarios.json](scenarios.json).
3. Alternatively, import this directory in the browser, create a **three-node Docker experiment**, and follow the manual steps below. Docker executes this Go code; the built-in simulation does not.
4. For local checks, run `go -C protocols/lamport_mutex test -race ./...` and `go -C protocols/lamport_mutex vet ./...` from the repository root. The race detector checks unsynchronized access to shared memory. The existing module replacement points to this repository's SDK [the library that connects Go nodes to DistVis].

The scenario runner synchronizes local files into the platform. Do not use it to overwrite independently edited platform code or node-specific implementations: compare/merge those first. It leaves completed run histories available in the platform. Export a completed run from its history page; exports contain the exact code and events for that run.

## Implementation and algorithm scope

- [main.go](main.go) registers the application and peer RPC services [methods callable on another node], initializes state, and connects background workers to runtime shutdown.
- [protocol.go](protocol.go) contains the request queue, clock updates, acknowledgement handling, application methods, and the alternative algorithm.
- [protocol_test.go](protocol_test.go) checks entry conditions, stale/duplicate messages, report ordering, input validation, message counts, background operation completion, and repeated concurrent acquisitions in both modes.

Every request has an identity `(timestamp, node ID)`. A **Lamport timestamp** [an event counter, not elapsed or wall-clock time] increases on a local event; receiving timestamp `t` sets it to `max(local, t) + 1`. Requests are ordered by timestamp, then by the numeric part of the node ID, so a tie puts node-2 before node-10.

The default `lamport` mode implements the lab-oriented version:

1. Add the local request to the sorted queue and send `Request` to every peer.
2. A receiver adds the request to its queue. If it is critical, or waiting for its own earlier request, it defers the `Ack` [keeps the permission reply until its own protected operation has finished]. Otherwise it sends `Ack` immediately.
3. Enter only when every peer has acknowledged this exact request **and** the local request is first in the queue.
4. Report leaving the critical section before sending permissions to other nodes. Remove the local request, flush deferred acknowledgements, and send `Release` to every peer.
5. A receiver removes only the exact request identified by `Release.ForTS`. It records released timestamps so a delayed request cannot reappear after its release.

Classic Lamport mutual exclusion usually sends immediate replies and relies on reliable FIFO communication [messages on each directed connection arrive in send order]. The lab text emphasizes deferred replies. This implementation retains that rule plus the Lamport queue and explicit releases; it is a teaching variant, not a claim that classic Lamport requires Ricart–Agrawala's reply rule.

In `ricart-agrawala` mode the priority/deferral rule is retained, but remote requests are **not** stored in a distributed queue, entry requires all acknowledgements without a queue-head check, and no `Release` messages are sent. Deferred acknowledgements themselves permit the next requester to proceed. Removing only `Release` from the default mode would be incorrect because old queue entries would remain.

All nodes must choose the same algorithm before sending any requests. `Configure` rejects changes after protocol activity; peer messages also carry the algorithm and mismatched modes are rejected. This is a local configuration guard, not a distributed configuration agreement protocol. Use a fresh run for each comparison; a recovered process starts with the default mode.

## Application inputs

| Method | Inputs | Meaning of successful return |
| --- | --- | --- |
| `Application.Acquire` | `holdMs`: 1–120000 milliseconds; 0/omitted means 2000 | A single operation was **accepted**, not that the lock was acquired or released. Observe `operation`, `entries`, and `completed`. |
| `Application.Auto` | `count`: 1–100; 0/omitted means 5 | A background sequence was accepted. Each cycle holds for 1000 ms, with a random 100–599 ms gap between cycles. Observe `autoRunning` and `completed`. |
| `Application.Configure` | `algorithm`: `lamport` or `ricart-agrawala` | This node selected a mode before any request activity. Configure every node before starting an acquisition. |
| `Application.Read` | none | Returns and republishes the current full state. |

An accepted acquisition reserves the node immediately, before its background worker starts. Another `Acquire`, or an overlapping `Auto`, is rejected. Manual `Acquire` is also rejected between cycles of an active `Auto` sequence.

A successful `command_result` for `Acquire` means **accepted**. The full operation continues independently and does not occupy a long-running application call subject to the platform's approximately ten-second deadline. A fifteen-second hold is valid. This is a deliberate change from the original implementation, whose application call waited for the entire cycle.

## State fields

| Field | Explanation |
| --- | --- |
| `node`, `algorithm` | This node's identifier and selected algorithm. |
| `reportSeq` | Strictly increasing report number within one process lifetime. All snapshots are published under the same state lock, so old snapshots cannot overwrite newer ones. Resets on process restart. |
| `clock` | Current Lamport event counter. |
| `role` | `idle`, `waiting`, or `critical`. Only `critical` counts as holding the protected section. |
| `myTS` | Current request's timestamp; zero when not requesting/holding. |
| `acksHave`, `acksNeed` | Distinct permissions received for this request and number required (`n−1`). |
| `missingAcks` | Peer IDs still awaited while waiting. Empty when no request is waiting. |
| `queue` | Sorted requests in the default mode. Empty in Ricart–Agrawala mode. |
| `deferred` | Nodes whose permission replies are being withheld until local release. |
| `entries` | Number of critical-section entries. |
| `completed` | Number of local acquisition cycles that reached the end of release processing. This is **not** proof that every remote node received a release; also check `lastError`/`sendErrors` and remote queues. |
| `lastWaitMs` | Elapsed time between starting the most recently entered request and its entry; not a continuously updated waiting timer. |
| `messagesSent` | Algorithm RPC **attempts** made by this node, including failed attempts; excludes automatic RPC responses and cancellation envelopes. Counter changes are published immediately. |
| `sendErrors` | Cumulative number of failed/timed-out outgoing calls. |
| `lastError` | Most recently observed error, including method and peer for outgoing failures. Cleared at the start of a new accepted acquisition; cumulative `sendErrors` is not reset. |
| `operation` | `none`, `waiting`, `critical`, `releasing`, `completed`, or `stopped`. `releasing` means local exclusion has ended but outgoing release processing is still finishing. |
| `operationRunning` | Whether a local acquisition worker is still active. |
| `autoRunning` | Whether an automatic acquisition sequence still owns this node, including the gaps between cycles. |

`Ack.ForTS` ensures an old acknowledgement never approves a newer request. Duplicate acknowledgements count only once per peer, duplicate requests do not duplicate queue/deferred entries, and releases name an exact request rather than removing whichever request happens to belong to a node.

## How to count messages

For **one completed acquisition across an otherwise idle group of n nodes**:

- Default mode: `(n−1) Request + (n−1) Ack + (n−1) Release = 3(n−1)`.
- Ricart–Agrawala: `(n−1) Request + (n−1) Ack = 2(n−1)`.

Sum the **increase** in `messagesSent` over **all nodes**, including peers that send acknowledgements. A three-node default-mode acquisition produces 6 algorithm messages; the alternative produces 4. For two completed default-mode acquisitions, expect 12.

Every algorithm message is an RPC call. The RPC library sends a separate response even when its body is empty; those response arrows are not additional algorithm steps. An error-free isolated default-mode acquisition therefore normally produces 6 request envelopes and 6 response envelopes. `Ack` is an explicit protocol call, **not** the empty response to `Request`. For rigorous counts, filter `send` events to `payload.type == "RPCRequest"` and the three `MutexService.*` methods; exclude application calls, responses, and cancellation messages.

## Required experiments and reproduction steps

Use three nodes, Docker, 80 ms fixed latency, 128 KiB/s bandwidth per directed link, and seed 42. These are defaults in every saved scenario. A fixed seed does not make Go scheduling deterministic, so inspect actual request timestamps rather than assuming that concurrently submitted calls necessarily get equal timestamps.

### A. Concurrent requests (`normal`)

1. In a fresh default-mode run, submit `Acquire(holdMs=2000)` concurrently on node-1 and node-3.
2. Both application calls should quickly return accepted/waiting state.
3. Inspect their `Request` timestamps. The lower `(timestamp, numeric node ID)` has priority; if both timestamps are 1, node-1 goes first.
4. Wait for each requester's `completed=1` and all queues to empty. Check the **entire state history**, not only the final state, for at most one `role=critical` at every event.
5. Count 12 algorithm messages across the two acquisitions.

### B. Uninvolved node crash (`crash`)

1. In a fresh run, crash **node-3** while it is idle.
2. Submit `Acquire(holdMs=2000)` on **node-2**.
3. Observe node-2 waiting with `entries=0`, `acksHave=1`, `acksNeed=2`, and `missingAcks=["node-3"]`.
4. After the outgoing call's three-second observation timeout, `lastError` names node-3. Wait beyond ten seconds: the accepted operation is still waiting and has not entered.
5. End the run. Finite observation establishes that no entry occurred during this interval; indefinite blocking follows from the missing-reply wait and absence of retries, not from observing forever.

The old prose in the combined report used different node names. The original saved crash JSON used **node-3 crashed / node-2 requesting**; these steps and the replacement report use the same names.

### C. Blocked link and healing (`partition`)

1. In a fresh run, block both directions between node-2 and node-3.
2. Start `Acquire` on node-2. Other nodes remain online, but node-2 cannot collect node-3's permission.
3. Wait for the error mentioning node-3 and verify `entries=0`.
4. Heal the links, then observe for four more seconds. The lost request is not replayed and the operation stays waiting.
5. End the run. Do not claim that restoring connectivity automatically restores protocol progress.

This uses the lab's blocked-link failure option; it is not a complete split into two disconnected groups because node-1 still has direct links to both nodes. The protocol does not relay missing requests through node-1.

### D. Design comparison (`lamport-count`, `ricart-count`)

1. Fresh default-mode run: acquire once on node-1, then wait for completion and empty queues. Count 2 requests, 2 acknowledgements, and 2 releases: **6** algorithm messages.
2. Another fresh run: call `Configure(algorithm="ricart-agrawala")` on **all three nodes**, waiting for each configuration result before requesting.
3. Repeat the same acquisition and hold time on node-1. Count 2 requests, 2 acknowledgements, and **zero releases**: **4** algorithm messages.
4. Explain why the queue/entry rule also changes in the alternative, and why the saved round of messages does not remove the requirement to contact every peer.

### Additional regression checks

- `long-hold`: accept a 15000 ms hold promptly, verify the node remains critical after eleven seconds, and then observe completion without an application deadline error.
- `ricart-contention`: configure the alternative on all nodes, start three `Auto(count=3)` sequences concurrently, and verify all nine acquisitions complete without overlapping critical sections or release messages (36 algorithm messages).

Run one scenario with `node scripts/scenarios.mjs run lamport_mutex <id>`. Automated steps/assertions are in [scenarios.json](scenarios.json). Current measured observations and exported histories are in the **Verified experiment evidence** folders below. The older combined experiment report is historical and is not the canonical evidence for the current code.

## Discussion and limitations

**Why does one unrelated failure block everyone?** Every new acquisition needs a permission from every other member. A missing permission prevents entry, even if that peer never wanted the protected section. Fewer replies would change the algorithm's safety argument, so this demonstration intentionally does not skip failed nodes.

**Why are both permission and ordering visible?** Permission prevents a requester from proceeding before peers have considered its request. Timestamp ordering gives competing requests a consistent priority. In the default teaching mode, deferral reinforces that ordering while the queue makes it observable; the Ricart–Agrawala comparison removes the queue dependency with the release messages.

**Does a timeout prove the peer did nothing?** No. The protocol reports an outgoing call after waiting three seconds, but the underlying SDK call can continue until its own deadline. A response may be lost after the remote method executed. The caller never reads the reply after its observation timeout and never treats timeout as permission or as a successful rollback.

**Does this support recovery or lossy-network progress?** No persistence [saving protocol state across process restarts], retransmission, membership changes, or restart-safe request identities are implemented. Lost requests, acknowledgements, or releases can block later progress. Healing does not replay lost messages. A recovered node loses its clock, queue, and selected mode; correctness across crash/recovery is not claimed. Start a fresh run after the fault demonstration rather than resuming old requests as if recovery were supported.

**What happens on runtime shutdown?** Background waits observe cancellation and stop. Shutdown is not a distributed release, so the last role is not rewritten as a false successful unlock. The stopped run retains its history; a new run begins independently.

**What do passing tests establish?** The recorded scenarios and automated tests demonstrate specific executions and regressions [previously failing cases checked again after changes]. They are not a mathematical proof for all schedules or a production-ready distributed lock implementation.

## Verified experiment evidence

One folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.

- [Lamport A - concurrent requests](experiments/normal/README.md)
- [Lamport B - idle peer crash](experiments/crash/README.md)
- [Lamport C - blocked link and healing](experiments/partition/README.md)
- [Lamport D - six messages per acquisition](experiments/lamport-count/README.md)
- [Lamport D variant - Ricart Agrawala four messages](experiments/ricart-count/README.md)
- [Lamport regression - fifteen second hold](experiments/long-hold/README.md)
- [Lamport variant regression - three competing nodes](experiments/ricart-contention/README.md)
