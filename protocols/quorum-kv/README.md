# Quorum replicated key-value store

Implements [LAB1 §5.2](../../docs/LAB1.en.md#52-quorum-replication). A **replica** [one node's copy of the data] stores each key with a version `(counter, writer ID)`. A **quorum** [the required number of distinct responding replicas] is configured separately for writes, W, and reads, R; defaults are a majority [more than half] of the fixed membership.

## Implementation

- [main.go](main.go) registers the application and internal RPC services [methods callable on another node], restores durable state, and chooses the default quorum sizes.
- [protocol.go](protocol.go) contains version comparison, durable replica operations, concurrent requests, and application methods.
- [protocol_test.go](protocol_test.go) checks that delayed writes cannot replace newer versions and that storage failure never produces a successful acknowledgement.

A write first queries R replicas for their versions, chooses a greater counter, persists its counter [saves it so a restart cannot reuse it], and sends the value to every replica concurrently. It returns after W acknowledgements, without waiting for slower replicas. A read queries R replicas, selects the greatest version, then writes it back to W replicas before replying; this **read repair** [copying the selected version back to replicas] prevents later reads from going backward after a read observes a partially completed write. Every node includes itself at most once in a quorum.

These extra query/write-back phases deliberately strengthen the simple one-round teaching store. With a uniform fixed configuration satisfying `W+R>N`, crash-recovery persistence, unique version tags, and successful quorums, this follows the majority-register approach for each key. Do not infer a transaction across multiple keys, or correctness while different nodes use incompatible configurations. `Mode` is an experiment control, not a distributed reconfiguration protocol [a coordinated method for safely changing membership or quorum rules].

### Timeout semantics

A remote call has a four-second observation timeout. The remote operation can still finish after that timeout; the result is unknown, not rolled back. Both normal writes and repaired reads can partially update replicas before failing. Shared RPC connections are not closed to cancel an individual call.

## Application inputs

| Method | Fields | Meaning |
|---|---|---|
| `Application.Put` | `key`, `value` | Query versions, write, and wait for W acknowledgements. |
| `Application.Get` | `key`, optional `minCounter` | Read R versions, repair to W replicas, and return the selected value. |
| `Application.Mode` | `w`, `r` | Set this node's quorum sizes, each in 1..N. Configure all nodes alike before measured operations. |
| `Application.Read` | none | Inspect local state without pretending this is a quorum read. |

`minCounter` is an optional diagnostic supplied by the caller. `lastGet.stale=true` means the returned counter is below that known lower bound. A partitioned replica cannot know an inaccessible global latest value; the implementation does not fabricate such knowledge. Compare the complete `(counter,writer)` tag when two writers have equal counters; `minCounter` alone is intentionally a limited diagnostic.

## State fields

| Field | Meaning |
|---|---|
| `role` | `replica`. |
| `n`, `w`, `r` | Membership size and local quorum thresholds. |
| `values` | Locally stored values and their version tags. |
| `messagesSent` | Internal Fetch/Store request attempts, excluding self-calls and transport replies. |
| `lastPut`, `lastGet` | Key, selected/written item, responding nodes, elapsed milliseconds, success, and diagnostic note. |
| `lastGet.stale` | Whether the selected counter is below the caller's `minCounter`. |
| `lastError` | Most recent operation failure; timeout does not prove nonexecution. |

## Reproduce the observations

From the repository root:

```sh
go -C protocols/quorum-kv test -race ./...
go -C protocols/quorum-kv vet ./...
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run quorum-kv
```

Use Docker, not the built-in reference simulation [which does not execute this Go code]. The complete settings and inputs are in [scenarios.json](scenarios.json); measured reports and exports are in the scenario folders listed in the evidence index below. The slow-replica scenario compares W=3 and W=5 with a 3000 ms link, while the unsafe variant isolates node-1 with W=R=1.

## Discussion

**Why must W+R exceed N?** If W+R>N, a set of W nodes and a set of R nodes cannot be disjoint, so a later version query meets at least one replica that acknowledged the write. Replica version comparison rejects older updates. For concurrent writes and reads that observe incomplete writes, intersection alone is not the whole argument; the write's version-query phase and read's write-back phase preserve ordering as well.

**Why not always wait for all replicas?** Waiting for a majority avoids the slowest replica when enough other nodes respond. W=N makes a single slow or failed replica delay or prevent successful acknowledgement. The measured implementation has two phases per operation, so its fast-path time is not the homework's approximate one-round 200 ms figure.

**What happens in the minority partition?** It cannot form a configured three-of-five quorum and therefore cannot complete a normal operation. The majority component remains available. This chooses consistency over availability during the split, but failed operations may still have partial effects.

**What does W=R=1 sacrifice?** A writer and reader can consult disjoint replicas, so a completed write need not be visible to a later read. The unsafe experiment deliberately produces that case and marks it stale relative to a caller-provided counter. It is a consequence of the selected quorum configuration, not an unnoticed implementation failure.

## Limits

Fixed membership; no atomic multi-key transactions, automatic background reconciliation, deletion tombstones, external client deduplication, or safe dynamic reconfiguration. A recovered replica catches up when it participates in later reads/writes, not immediately through a startup broadcast. Internal peers are trusted [not malicious]; this is not a Byzantine-fault-tolerant store. Storage errors stop further successful operations until restart rather than acknowledging unsaved state. Practical counter exhaustion is outside the short bounded lab runs.

## Verified experiment evidence

One folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.

- [Quorum normal: sequential write and read](experiments/normal/README.md)
- [Quorum: fastest majority versus all replicas](experiments/slow-replica/README.md)
- [Quorum crash: majority still available](experiments/crash/README.md)
- [Quorum partition: minority unavailable](experiments/partition/README.md)
- [Quorum variant: W=R=1 stale read](experiments/variant-nonintersection/README.md)
