# Bully election

Implements [LAB1 §4.2](../../docs/LAB1.en.md#42-bully-election-required). The goal is to select the **highest live numeric node ID** as coordinator [the node others recognize as the leader], assuming the relevant nodes can communicate within the timeout.

## Implementation

- [main.go](main.go) starts an automatic election and periodic failure detection through `OnStart` [the runtime hook executed after local message handling is ready].
- [protocol.go](protocol.go) implements Election, OK responses, Coordinator announcements, and manual input.
- [failure_detector.go](failure_detector.go) probes the known coordinator and starts another election after suspicion or an expired wait for an announcement.
- [protocol_test.go](protocol_test.go) checks numeric ID ordering, highest-node victory, and cancellation of obsolete campaigns [an election attempt that has already been superseded].

A node sends Election to every higher ID concurrently. Each higher node answers OK and either campaigns itself or reannounces that it is already coordinator. Receiving at least one OK makes the initiator wait for a Coordinator announcement; if that wait expires, automatic failure detection retries. If no higher node answers within three seconds, the initiator declares itself coordinator and broadcasts Coordinator to all members. A higher Coordinator announcement invalidates any older local campaign.

Startup waits about one second before campaigning, allowing peer services to register. A recovered node performs the same startup election, so a recovered highest ID takes over automatically. A coordinator responding to an election reannounces to **all** members, not only the initiator; this repairs disagreement after a partition is healed and a new election is requested.

## Inputs and state

| Application method | Fields | Effect |
|---|---|---|
| `StartElection` | none | Accept an asynchronous election request [the election continues after the input returns]. |
| `Auto` | `enabled` | Enable or disable subsequent periodic failure detection on this node; does not cancel an election already running. |
| `Read` | none | Return and publish current state. |

| State field | Meaning |
|---|---|
| `node`, `id` | Node name and numeric election priority. |
| `role` | follower, candidate, or coordinator. |
| `coordinator` | Locally recognized coordinator, possibly empty during election. |
| `electionRound` | Local generation used to ignore obsolete campaigns; not a global consensus term. |
| `electionsWon`, `backedDown` | Local victories and withdrawals. |
| `sent` | Election requests, OK responses, and Coordinator request attempts. Heartbeat probes and empty transport replies are excluded. Read can refresh the final count after background sends. |
| `lastError` | Diagnostic field; a timeout normally triggers an election rather than terminating the process. |

The counter is local process state and resets after a crash. For exact totals, count the exported Election/Coordinator request envelopes and successful Election responses carrying `ok:true`. The automatic RPC response to Coordinator is not a fourth election message type.

## Reproduction

```sh
go -C protocols/bully_election test -race ./...
go -C protocols/bully_election vet ./...
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run bully_election
```

[scenarios.json](scenarios.json) defines six Docker experiments: five-node automatic startup, automatic re-election after coordinator crash, the exact seven-node lecture case, two highest-node failures, a partition, and recovery takeover. Each scenario has its own measured README and export in the evidence index below. The original report's manually triggered startup/recovery and eight-node substitution are historical observations, not evidence for these corrected requirements.

## Discussion

**Why does the highest surviving node win?** Every lower candidate asks higher IDs first and steps aside when one answers. The highest reachable live node has no higher responsive competitor, so it announces itself. Numeric ID comparison matters: node-10 must outrank node-2.

**Why can message work grow as O(n²)?** If many nodes begin together, each can contact many higher IDs, so the number of Election/OK exchanges grows with the sum 1+2+...+(n−1). Coordinator broadcasts add more messages. The trace illustrates those exchanges; a single execution does not measure asymptotic growth experimentally.

**Can a timeout prove that the coordinator crashed?** No: an overloaded process, delayed packet, or blocked link can produce the same silence. Timeouts create suspicion [a reason to try another coordinator], not proof. Therefore separate network components can each elect a leader, as the partition experiment demonstrates.

**Why does the old coordinator take over after recovery?** Recovery reruns the election algorithm and numeric priority still favors the highest ID. This is reasonable when IDs intentionally encode preferred capacity or administrative priority. It can be disruptive when the highest node repeatedly becomes slow or unavailable, because the group repeatedly changes coordinators.

**Does healing alone guarantee immediate agreement?** No: restoring links does not replay dropped Coordinator messages. The documented experiment explicitly requests a new election after healing; the highest coordinator then reannounces to every member. Bully is an election demonstration, not a partition-safe replicated-state consensus service.

## Limits

Fixed membership, trusted peers, bounded timeout assumptions, no durable application state, fencing tokens [identifiers a storage system uses to reject an obsolete leader], or consensus-backed leadership leases. Two components may both have a coordinator; do not use that state alone to authorize conflicting database writes. Background heartbeat traffic is diagnostic and distinct from the lecture's three election message types. Exact timing and the number of redundant campaigns vary with real scheduling.

## Verified experiment evidence

One folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.

- [Bully normal: automatic five-node election](experiments/normal-startup/README.md)
- [Bully crash: automatic failure detection](experiments/crash-auto/README.md)
- [Bully: seven-node lecture trace](experiments/seven-node-lecture/README.md)
- [Bully: two highest nodes crash](experiments/two-crashes/README.md)
- [Bully partition: two component coordinators](experiments/partition/README.md)
- [Bully variant: recovered highest node takes over](experiments/variant-recovery-takeover/README.md)
