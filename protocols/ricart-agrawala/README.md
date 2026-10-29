# Ricart–Agrawala mutual exclusion

Implements [LAB1 §4.4](../../docs/LAB1.en.md#44-ricart-agrawala). **Mutual exclusion** [at most one node executes the protected section at a time] requires a timestamped permission from every other member, but no explicit Release broadcast.

## Algorithm and code

[main.go](main.go) selects `ricart-agrawala` by default. [protocol.go](protocol.go) uses the same tested permission implementation as [the Lamport comparison](../lamport_mutex/README.md), while removing remote queue storage, the queue-head entry check, and Release messages in this mode. The separate standalone directory makes the selected protocol unambiguous for submission. [protocol_test.go](protocol_test.go) verifies request ordering, stale replies, concurrent inputs, safety reports, and message counts.

1. A requester selects one Lamport timestamp [an increasing logical event counter] and broadcasts Request with that same request identity to every peer.
2. A peer replies immediately unless it is in its critical section or waiting on its own smaller `(timestamp,numeric node ID)` request. In those cases it defers permission [holds the reply until its own operation exits].
3. The requester enters only after every distinct peer acknowledges that exact request timestamp. Old or duplicate replies cannot satisfy a different request.
4. On exit, the node publishes that it no longer holds the critical section, then sends its deferred replies. Those replies replace a separate release message.

A three-node acquisition uses `2(n−1)=4` algorithm messages: two Request and two Ack. The RPC library also sends empty transport responses; those arrows are not additional algorithm steps. Compare the [Lamport six-message control](../lamport_mutex/experiments/lamport-count/README.md).

## Inputs

| Method | Fields | Effect |
|---|---|---|
| `Acquire` | `holdMs`, default 2000, range 1..120000 | Accept one asynchronous request; observe entries/completed for actual progress. |
| `Auto` | `count`, default 5, range 1..100 | Run that many acquisitions, holding each for 1000 ms with a random 100..599 ms gap. |
| `Configure` | `algorithm` | Select lamport or ricart-agrawala before any protocol activity, on every node consistently. Not needed for the default RA scenarios. |
| `Read` | none | Inspect and republish local state. |

These methods are registered under `Application`. A second local request is rejected while a prior acquisition or automatic sequence owns the node. The implementation does not promise multiple local goroutines [independently scheduled tasks] can bypass that reservation.

## State fields

`role` is idle, waiting, or critical; `operation` additionally distinguishes releasing and completed. `clock` is the current Lamport counter; `myTS` identifies the pending request. `acksHave`, `acksNeed`, and `missingAcks` explain why entry is blocked; `deferred` lists withheld permissions. `entries`, `completed`, and `lastWaitMs` give progress and measured waiting time. `messagesSent` counts outgoing algorithm requests, `sendErrors` counts failures, and `lastError` explains the most recent error. `queue` is empty in RA mode; `algorithm`, `autoRunning`, `operationRunning`, and `reportSeq` make the configuration and report order visible.

## Reproduction

```sh
go -C protocols/ricart-agrawala test -race ./...
go -C protocols/ricart-agrawala vet ./...
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run ricart-agrawala
```

The six experiments in [scenarios.json](scenarios.json) cover normal concurrent requests, an idle-peer crash, a blocked link and healing, five-node random load with exponential FIFO delay, the same load with message reordering, and the four-message design comparison. The scenario folders below contain the exact input/fault steps and exported evidence. The independent checker replays all state reports, not only the final state, to look for overlapping holders.

## Discussion

**Why can Release be removed?** A peer's deferred reply is the permission that becomes available after its own higher-priority request finishes. Sending that reply is enough to let the waiting requester advance. The distributed request queue used by Lamport is removed as well, so there is no remote queue entry that needs an explicit release.

**Why does one uninvolved crash block everyone?** Every acquisition needs all other members' permissions, not just a majority. A crashed idle peer still belongs to that required set. Timing out its request does not authorize entry.

**Does exponential jitter reorder messages?** Not with the platform's default FIFO policy [each directed link delivers in send order]. Jitter adds variable delay but later messages wait behind earlier messages on that link. The separate unordered experiment explicitly enables reordering and tests exact request IDs plus deferred permissions under that delivery policy.

**Why is a consistent request timestamp important?** Every peer must compare the same request identity against competing requests. If a requester assigns different priorities to different outgoing messages, peers can disagree about which request has precedence. This implementation chooses the request timestamp once and copies it into every Request.

**What does the design comparison improve?** It reduces the per-acquisition algorithm count from 3(n−1) to 2(n−1). It does not reduce the number of required participants. The partition and crash experiments therefore still block, despite the lower message cost.

## Limits

No persistent lock state, retransmission, membership changes, or restart-safe request identities. Crash/recovery correctness is not claimed: use a fresh run after the blocking demonstration. Healing does not replay dropped messages. The unordered experiment validates a concrete execution with reliable eventual delivery, not arbitrary loss, malicious peers, or every possible schedule. Both implementations follow the lab's deferred-permission rule; the Lamport comparison explicitly documents its difference from the classic immediate-reply presentation.

## Verified experiment evidence

One folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.

- [RA normal: simultaneous node-1 and node-3](experiments/normal-contention/README.md)
- [RA crash: uninvolved peer blocks](experiments/crash/README.md)
- [RA partition: no replay after healing](experiments/partition/README.md)
- [RA random load: exponential FIFO](experiments/jitter-fifo/README.md)
- [RA random load: unordered messages](experiments/jitter-unordered/README.md)
- [RA design: four messages, no Release](experiments/variant-message-count/README.md)
