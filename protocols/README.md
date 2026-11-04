# Lab 1 submission

This is the entry point for the selected Lab 1 assignment ([Chinese requirements](../docs/LAB1.md), [English translation](../docs/LAB1.en.md)). Both versions were checked: the Chinese Berkeley section additionally requires drift during coordinator downtime and renewed convergence after recovery. The submission contains **nine protocols** and **53 verified Docker experiments** [executions of the submitted Go programs in real containers, not the built-in reference simulation]. Each experiment has exactly one named folder under its protocol, containing its report and evidence.

## Selected protocols and requirement coverage

The assignment's introductory “at least six” wording is less restrictive than its module-selection table. This submission follows the full table: three clocks, two mutual-exclusion algorithms, Bully election, one replication algorithm, and two consensus/commit algorithms.

| Module | Selected protocol | Experiments | Required evidence |
|---|---|---:|---|
| L03 clocks | [Cristian](christian/README.md) | 5 | Normal, server crash/recovery, asymmetric latency, server delay, zero/half/full RTT variants |
| L03 clocks | [Berkeley](berkeley/README.md) | 5 | Periodic convergence, outlier rejection, coordinator crash, blocked link, jump/slew variant |
| L03 clocks | [Lamport + vector clocks](logical-clocks/README.md) | 6 | Six-event example, equal concurrent events, automatic events, crash/recovery, blocked link, omitted-merge variant |
| L04 mutual exclusion | [Lamport](lamport_mutex/README.md) | 7 | Concurrent requests, idle-peer crash, blocked link, six/four-message comparison, long hold, contention regression |
| L04 mutual exclusion | [Ricart–Agrawala](ricart-agrawala/README.md) | 6 | Concurrent requests, idle-peer crash, blocked link, exponential FIFO and unordered load, message-cost comparison |
| L04 election | [Bully](bully_election/README.md) | 6 | Automatic startup, automatic crash detection, exact seven-node case, two crashes, partition, automatic recovery takeover |
| L05 replication | [Quorum KV](quorum-kv/README.md) | 5 | Normal, slowest-replica comparison, crash/recovery, minority partition, nonintersecting-quorum stale read |
| L05 consensus | [Paxos KV](paxos-kv/README.md) | 6 | Ordered Put/Get, competing writes, crash/catch-up, minority partition, amnesia variant and persistence control |
| L05 commit | [Two-phase commit](two_phase_commit/README.md) | 7 | Two normal transfers, veto, crashes before/after commit point, partition, missing-prepare-log variant and safe control |

The spelling `christian/` is retained to avoid breaking the existing platform association; the algorithm's name is **Cristian**. `logical-clocks/` is the combined Lamport/vector implementation; `lamport_mutex/` is the mutual-exclusion implementation. Other pre-existing directories are examples or earlier prototypes, **not additional selected submissions**. In particular, `berkeley_new/`, `lamport/`, and `randa/` are not the canonical versions listed above. They were not deleted or silently presented as completed alternatives.

## Folder contract

```text
protocols/<selected-protocol>/
  main.go, protocol.go, other Go files
  go.mod, go.sum
  README.md                    implementation, state, inputs, discussion, limits
  scenarios.json              all reproducible scenario definitions
  experiments/<scenario-id>/
    README.md                  observation, actual measurements, manual steps
    scenario.json              exact settings, inputs, faults and assertions
    run.json                   complete DistVis export, including executed source
    checks.json                scenario and independent evidence-check results
```

[lab-evidence-index.json](lab-evidence-index.json) lists all 53 selected run IDs, experiment IDs, event counts, and request counts. The selected exports were checked against the current non-test Go source files. A code change does not rewrite an old execution; later modifications must be rerun before claiming the old export validates them.

## Read and reproduce

1. Start with a protocol's README for the algorithm, application interface, state fields, and discussion answers.
2. Follow its **Verified experiment evidence** links to each scenario README. Each has a 3–5 sentence observation, measured values, exact settings, numbered manual steps, and the full JSON export.
3. For automatic replay, run from the repository root:

```sh
npm start
# In another terminal, with Docker available:
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run paxos-kv
# Or one scenario:
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run paxos-kv crash-catchup
```

The runner discovers the current application input schemas [the methods and fields exposed by each real node], waits for command results, and checks state/history assertions. It preserves node-specific platform copies rather than dropping them. If platform code was edited separately, compare it with the directory first: synchronization changes the code used by future runs. Experiment evidence folders are omitted from the runner's uploaded build files to avoid recursively embedding old exports in new exports.

The platform only allows one active run. The runner waits rather than terminating an unrelated experiment. Fixed seed controls network randomness, but real Go scheduling and random application load can change the precise trace; the stated properties and measured ranges are the reproducibility target, not identical timestamps.

## Verification

For each selected folder:

```sh
go -C protocols/<selected-protocol> test -race ./...
go -C protocols/<selected-protocol> vet ./...
```

The race detector [a tool for detecting unsynchronized shared-memory access] and unit tests supplement, rather than replace, real Docker evidence. The [independent verifier](../scripts/verify-lab-evidence.py) recomputes checks from the exported histories: mutex permission/entry ordering and overlap, actual FIFO/unordered delivery, both directions of vector-clock causality against the message graph, Paxos majority-acceptance points and ordered state-machine execution, per-transaction 2PC agreement and balance conservation, clock arithmetic, Bully message counts, and Quorum version ordering. Run `python3 -I scripts/check-lab-submission.py` for the submission audit; it re-executes these checks instead of trusting saved pass flags, and compares all executed node snapshots and module dependencies with current source. `checks.json` distinguishes the ordinary scenario assertions from these additional checks. It does not claim that an automated invariant exists for every aspect of every protocol.

Unsafe variants pass only when they exhibit the intended failure: omitted logical-clock merge breaks a causal edge, nonintersecting quorums expose a stale read, Paxos amnesia produces conflicting slot-1 decisions, and missing 2PC prepare persistence breaks balance conservation. Their normal controls must pass the corresponding safety expectations.

## Report reconciliation and remaining boundaries

[report/report.odt](../report/report.odt) was read by extracting its OpenDocument XML [the structured document text inside the ODT archive], then updated with an audited replacement section. It opens successfully in LibreOffice. The historical draft and embedded screenshots remain labeled as historical, while the current protocol/experiment READMEs are the canonical reports. The original ODT and old compiled executables were preserved locally under `.lab-backups/` rather than discarded. The excluded report folder was not read.

Important limitations are explicit, not hidden to imply a stronger implementation: the mutex examples do not support restart-safe lock recovery; Bully can elect one coordinator per disconnected component; Berkeley has a fixed coordinator with durable identity/round recovery but no replacement election; Quorum KV has additional query/repair phases; Paxos KV is a bounded per-slot implementation without a Multi-Paxos optimization; and 2PC remains blocking while its coordinator is unavailable. These limits are compatible with the stated teaching scope and are discussed alongside the evidence.

**Grading claim:** the selected requirement set has implementation, reports, reproducible steps, and observed evidence. Passing these tests does not prove all possible distributed executions correct, and no grade is guaranteed; the instructor determines marks. The submission does not claim unimplemented optimizations or hide expected failures as successful normal operation.
