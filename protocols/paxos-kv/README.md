# Paxos replicated state machine KV

Implements [LAB1 §5.4](../../docs/LAB1.en.md#54-paxos-replicated-state-machine-kv). A **replicated state machine** [nodes applying the same chosen operations in the same order] uses one independent Paxos instance [one agreement decision] for each numbered log slot. Both Put and Get occupy slots.

## Implementation

- [main.go](main.go) initializes proposer, acceptor, and learner roles [request a decision, vote on it, and apply a chosen decision] on every node and restores saved state.
- [protocol.go](protocol.go) implements Prepare, Accept, Decide, ordered execution, retries, and catch-up.
- [protocol_test.go](protocol_test.go) verifies durable promises, rejection after storage failure, ordered reads, hole blocking, conflicting-decision rejection, and reuse of an already accepted value.

### One slot

1. The proposer chooses the lowest slot it has not yet applied. With a complete known prefix, this is the lowest unknown decision. It generates a unique ballot [proposal identifier] as `round*16 + membershipIndex`, where indices are 1..12 and the round counter survives restart.
2. It sends Prepare to all nodes and waits for a majority. An acceptor saves its new promise before replying; its response includes any previously accepted ballot and value.
3. The proposer must adopt the value with the highest accepted ballot among those replies. It may use its own operation only if no prior accepted value was reported.
4. It sends Accept and waits for a majority of successful durable votes. At that point the value is chosen. Decide announces the result; it does not create the decision retroactively.
5. If a different operation won, the proposer advances to the next slot and retries its own operation. If its operation won, it replies only after execution in slot order.

Requests go to all peers concurrently, and majority collection does not wait for the slowest peer. Rejections cause a higher ballot and randomized backoff [a small varying pause before retrying] within the bounded operation deadline. Different nodes can propose concurrently; local application operations are serialized [one at a time] so one process does not race its own client sequence.

### Ordered execution and recovery

Decisions after a hole [an earlier slot with an unknown decision] are stored but not executed. Receiving Prepare for a later slot schedules independent no-op proposals for holes. A no-op [an operation with no data effect] is only a candidate: the usual Prepare rule recovers an already accepted/chosen operation instead. Once the missing prefix is decided, execution advances through all consecutive decisions.

The stable state includes promises, accepted values, decisions, applied prefix, KV contents, proposer counters, and remembered operation results. Save happens before promises, votes, or client success. Failure to save stops further promises and votes; recovery must read the durable file rather than treating an unreadable file as empty state.

## Application inputs

| Method | Fields | Meaning |
|---|---|---|
| `Application.Put` | `key`, `value`, optional `pauseMs` (0..1000) | Propose a write. The optional pause occurs after selecting the slot, allowing reproducible contention. |
| `Application.Get` | `key` | Propose an ordered read; unlike Read below, this takes a log slot. |
| `Application.Read` | none | Inspect local state only. It is not a linearizable application read. |
| `Application.Amnesia` | `enabled` | Before proposals, deliberately disable stable writes on this node for the unsafe design experiment. |

Successful Put/Get results contain `id`, `slot`, `value`, and `found`. An operation can return an error after approximately six seconds of bounded retries; a phase already in progress can add a small amount beyond that deadline. A timed-out proposal might still be chosen, so failure does not promise rollback [undoing all effects].

## State fields

| Field | Meaning |
|---|---|
| `role` | Every node is a proposer, acceptor, and learner. |
| `phase` | Latest local activity: idle, prepare, accept, completed, or failed. Background catch-up can also change it. |
| `log` | Per-slot `promised`, `accepted`, `value`, and `decided`. |
| `applied` | Highest consecutive executed slot. |
| `values` | KV contents after applying that prefix. |
| `lastOperation` | Latest completed/failed local application result. |
| `conflicts` | Slots in which the local application's operation lost to another value. |
| `messagesSent` | Outgoing internal RPC attempts; local calls and transport responses are excluded. |
| `amnesia` | Unsafe persistence variant enabled in this process. |
| `storageError` | Stable-storage failure that prevents further successful voting. |

## Reproduction

```sh
go -C protocols/paxos-kv test -race ./...
go -C protocols/paxos-kv vet ./...
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run paxos-kv
```

All six scenarios use five real Docker nodes. See [scenarios.json](scenarios.json) and the scenario evidence index below for exact partitions, inputs, exported records, measured observations, and discussion. The amnesia variant and persistence control deliberately use majorities {1,2,3} then {3,4,5}, making node-3 the only common acceptor.

## Discussion

**Why must a proposer reuse an older accepted value?** Two majorities intersect [share at least one node], so a later Prepare must encounter evidence compatible with an already chosen value. Selecting the highest accepted ballot preserves that value even when not every acceptor learned Decide. Ignoring the accepted value could choose a second value for the same slot.

**Why does Get occupy a slot?** A local read could observe a replica that has not yet applied a completed write. Choosing the read in the same ordered log places it after the relevant earlier writes before it executes. This provides the read-ordering mechanism required for linearizability [completed operations respect real-time precedence].

**Why does the losing write not disappear?** A proposer separates the value that must complete the current slot from the operation its client originally requested. It helps the selected value finish and retries its own operation in a later slot. The contention experiment verifies both keys and a conflicts increment, not just one successful response.

**How can proposing no-op recover real data?** A majority that has already chosen the missing slot intersects the majority answering the recovery Prepare. Its accepted value takes priority over the candidate no-op. Only a genuinely unchosen hole can safely receive a no-op.

**Why is persistence part of safety?** If the single common acceptor forgets its promise and vote, two different majorities can each choose a different value for slot 1. The unsafe experiment records exactly that outcome. With persistence enabled, the new write goes into slot 2 instead: changing a later value is legal, rewriting a chosen slot is not.

## Limits

Fixed membership, trusted internal RPC peers, no Multi-Paxos leader optimization, log compaction, snapshots, or dynamic membership. The demonstration accepts slots 1..1000 and has bounded retries; it is not a production long-running service. Decide delivery is not retried forever; later Prepare traffic triggers hole recovery. A quiet recovered node may therefore remain behind until another operation runs. Client retries receive new local operation IDs; the service does not claim end-to-end exactly-once execution for an external client's repeated submissions. The unsafe Amnesia mode deliberately invalidates safety and must never be enabled in the control runs.

## Verified experiment evidence

One folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.

- [Paxos KV normal: reads also occupy slots](experiments/normal/README.md)
- [Paxos KV: simultaneous slot-one proposals](experiments/competing-writes/README.md)
- [Paxos KV crash: no-op hole recovery](experiments/crash-catchup/README.md)
- [Paxos KV partition: minority cannot commit](experiments/partition/README.md)
- [Paxos KV variant: forgetful intersection accepts conflicting slot](experiments/variant-amnesia/README.md)
- [Paxos KV control: intersection remembers chosen value](experiments/persistence-control/README.md)
