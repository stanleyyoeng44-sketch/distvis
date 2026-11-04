# Lamport + vector clocks: two views of the same events

**Lecture references:** lec3.pdf, §§3.1–3.4. This module deliberately runs **both** clock algorithms together so you can compare their answers for the same event trace.

Read [the shared Go tutorial](../CLOCKS.md), then [main.go](main.go), [clocks.go](clocks.go), [protocol.go](protocol.go), [rpc.go](rpc.go), and [protocol_test.go](protocol_test.go). The shared guide explains the module/checksum files too. The source explains exported types, pointers, slice copying, mutexes, RPCs, persistence and error handling as they appear.

## Algorithm rules

Every node begins with Lamport `L=0` and a zero vector of length N. Coordinates follow `members`, the ordered membership list from DistVis.

For a **local event** or a **send event** at node i:

```text
L = L + 1
V[i] = V[i] + 1
```

A data message piggybacks both the scalar and vector. For its **receive event** at node j:

```text
L = max(local L, received L) + 1
V[k] = max(local V[k], received V[k]) for every coordinate k
V[j] = V[j] + 1
```

For every event we additionally record the lecture's total-order number:

```text
total = N * L + i
```

Here i is the **zero-based membership index**, not the last digit or a string sort of the node ID. This works correctly for node-10/node-11/node-12 too.

## Precisely what counts as an event?

The model includes **Local**, **Send**, and **Deliver** (a receive). Each counts once, updating both clocks together.

DistVis commands, Read, Compare, state reports and net/rpc acknowledgements are **control/observation traffic, not modeled events**. The acknowledgement contains no logical timestamp and is not merged at the sender. The UI still shows that real RPC response; it is transport confirmation, not a second application message.

This distinction is essential to match the lecture's one-way message diagram. To model a request/reply application instead, replies would need their own send/receive events and clock merging. Likewise, a human observing an event then issuing a command does not inject that external causal history into these vectors. Claims about vector causality refer to the selected **data-event graph**, not every action of the processes or observer.

## Reproduce the six-event lecture example

Start a fresh **three-node Docker** run. Wait for all input forms. Perform the following actions; complete each Send before proceeding to the next data message:

1. On node-1, **Application.Local**: `{"label":"a"}`.
2. On node-3, **Application.Local**: `{"label":"e"}`.
3. On node-1, **Application.Send**: `{"to":"node-2","label":"b","receiveLabel":"c","payload":"m1"}`.
4. On node-2, **Application.Send**: `{"to":"node-3","label":"d","receiveLabel":"f","payload":"m2"}`.

The resulting timestamps must be:

| Event | Node | Kind | Lamport | Vector | N*L+i |
|---|---|---|---:|---|---:|
| a | node-1 | local | 1 | [1,0,0] | 3 |
| b | node-1 | send | 2 | [2,0,0] | 6 |
| c | node-2 | receive | 3 | [2,1,0] | 10 |
| d | node-2 | send | 4 | [2,2,0] | 13 |
| e | node-3 | local | 1 | [0,0,1] | 5 |
| f | node-3 | receive | 5 | [2,2,2] | 17 |

Use **Application.Read** (no fields) to see each node's clock and events. Read does not increment anything, so it cannot disturb the example.

## Compare causal order

**Application.Compare** accepts two JSON vector arrays of the current membership size:

```json
{"left":[2,0,0],"right":[2,2,2]}
```

Result: **happens-before** (`b → f`). Every left coordinate is <= its right coordinate and at least one is smaller.

```json
{"left":[2,0,0],"right":[0,0,1]}
```

Result: **concurrent** (`b ∥ e`). Some coordinates are smaller, others larger. The fact that `L(e)=1 < L(b)=2` does not prove any causal edge.

Other results are **happens-after** (reverse comparison) and **equal**. Equality is not strict happens-before and is not labeled concurrency. Comparing the same timestamp twice is the simplest equal case. Arbitrary user-entered vectors are compared numerically; the action does not authenticate that they represent real events.

The lecture's scalar total-order sort is `a, e, b, c, d, f`. That is a deterministic **linear order extending causality**, not a new chain of happens-before edges. In particular, tie-breaking a and e does not make a cause e. This distinction corrects misleading causal-arrow wording in the lecture's total-order narrative.

## Other experiments

- Submit Local simultaneously on node-1 and node-3 using DistVis's concurrent-input option. Both increment their own component; the resulting vectors are incomparable. Concurrency is about the absence of a data-message causal path, not perfect physical simultaneity.
- Send in both directions concurrently. The service releases its mutex before peer calls, so the two receive handlers can make progress instead of deadlocking.
- Block a link, then Send. The send event stays recorded. The returned `delivery.confirmed` is false with an error if no acknowledgement arrives. A send can exist without a receive, and a lost acknowledgement can hide a receive that did occur.
- Heal the link and send again with new labels. This is a **new send event**, not an automatic retry of the previous message.
- Crash and recover a logical-clock node within the same run. It reloads counters/history before serving inputs; its next event has a higher counter and a fresh ID. A new run begins from zeros.

## State, delivery and durability

Every event gets an ID `node-id:local-event-count`. User labels can repeat; use IDs/message IDs to associate exact events. `messageID` associates a send and its receive. The payload is ordinary lesson data (max 512 bytes); labels must contain non-whitespace text and be at most 128 bytes.

`events` contains the latest **64** local records. Complete earlier reports/messages remain in the platform's immutable run history. Clock vectors continue to account for all events even when the local history is truncated.

Counters and history are saved via `sdk.Save` **before** publishing an event or sending/acknowledging a message. `sdk.Load` restores them on recovery. File-not-found means first boot; other load errors stop startup rather than resetting counters. Storage failure during a run stops further event creation instead of pretending persistence succeeded. Save/report failure is not rollback: an increment may have been recorded even if the application command reports failure.

`LastDelivery` describes the latest completed outbound attempt; overlapping sends can finish in a different order than they started. Each command reply also contains its own event and delivery outcome. A successful Send command means the **send was recorded**; inspect `delivery.confirmed` separately. There are no hidden delivery retries or exactly-once guarantees.

Snapshots allocate fresh vector/history storage when updated. Without these copies, Go slices could share backing arrays and a later event could retroactively rewrite an earlier timestamp. Read snapshots therefore remain safe while other RPCs run concurrently.

## Additional application controls

[controls.go](controls.go) provides `Auto(count)` [a bounded random sequence of local/send events; default 8, maximum 20], `CompareEvents(left,right)` [compare complete Lamport/vector/total timestamps], and `Variant(omitReceiveMerge)` [deliberately omit the maximum merge before a receive increment]. Select the variant before events. The original `Compare` remains available for entering just two vectors.

State now also reports `role`, `autoRunning`, `messagesSent`, and `lastCompare`. CompareEvents reports the vector relation, scalar order, total order, and `agrees`: whether a vector-causal relation implies the corresponding scalar order. It does not authenticate user-entered timestamps or compare them against the real message graph. In the broken variant the vectors themselves can be wrong, so the independent evidence checker also verifies every recorded send/receive edge.

## Discussion

**Does a smaller Lamport number prove causality?** No: b has Lamport 2 and e has Lamport 1, but their vectors are concurrent. Lamport order is necessary for a causal edge [one event can affect the other through local order and messages], not sufficient to establish one. The six-event experiment verifies both cases.

**What do equal Lamport values mean?** The three first local events all have value 1 and pairwise incomparable vectors. The total-order tie-break still sorts them, but that ordering is a convention based on node identity. It does not describe physical time or invent a message path.

**Why merge before incrementing on receive?** The receiver must include the sender's known history before recording its own new event. Without the maximum merge, a send at scalar 2 can cause a receive stamped 1, and their vectors can falsely appear concurrent. The variant and the independent message-edge check show this failure directly.

**What does the automatic-event check establish?** It reconstructs causal paths from local event order and recorded messages, then checks both directions of the vector-clock equivalence and that Lamport order agrees with every causal path. The scenario also calls CompareEvents on actual recorded timestamps from all three nodes, not prewritten expected vectors. The safe automatic trace had no violations. Random generation changes the exact trace, so passing one run is evidence rather than a proof for every possible execution.

## Boundaries

- Fixed membership, 2–12 nodes. Recovery validates ordered membership and node identity before restoring state.
- Bounded integer counters preserve exact display in the browser and avoid unsigned overflow. Reaching the conservative limit returns an error rather than wrapping.
- No physical clock synchronization, causal delivery buffer, consensus, distributed lock or total-order broadcast. Assigning a total-order timestamp does not itself deliver events in that order.
- No Byzantine-message validation/authentication or arbitrary external-causality tracking. Internal RPC peers are trusted participants in the lesson.
- Transport duplication would be another modeled receipt; the module does not implement application-level deduplication or retransmission.

## Tests

```sh
go -C protocols/logical-clocks test -race -count=1 ./...
go -C protocols/logical-clocks vet ./...
```

Tests reproduce all six lecture timestamps exactly, compare all four vector relations, check max-before-increment and immutable snapshots, reject malformed vectors/overflow, check persistence ordering and restore, fail closed on storage errors, stress concurrent events/history bounds, and verify real net/rpc send/receive behavior without an extra acknowledgement event. The acceptance script repeats the trace in Docker and verifies node recovery.

## Verified experiment evidence

One folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.

- [Clocks normal: lecture six events](experiments/normal-six-events/README.md)
- [Clocks: equal Lamport concurrent events](experiments/equal-concurrent/README.md)
- [Clocks: automatic random events](experiments/auto-invariant/README.md)
- [Clocks crash: durable counters](experiments/crash-recovery/README.md)
- [Clocks partition: local events continue](experiments/partition/README.md)
- [Clocks variant: omit receive merge](experiments/variant-no-merge/README.md)
