# 2PC crash: before durable decision aborts

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The coordinator crashed after both participants voted YES but before recording a decision. Participants became uncertain [prepared and unable to learn the outcome] and kept their accounts locked. On recovery, the coordinator found no saved decision and durably chose abort. The final balances remained 100/100, matching presumed abort before the commit point.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `b77b97bf-ad4b-4182-8224-ec233b0a7c2a`; recorded `2026-10-29T09:21:34.295Z`.
- [Platform replay](http://localhost:3000/#experiment/971f85c5-af08-417f-a3e5-c52925102969/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 71; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | role TM; transaction T1; phase/state finished; decision abort; balance 0 |
| node-2 | role RM; transaction T1; phase/state aborted; decision ; balance 100 |
| node-3 | role RM; transaction T1; phase/state aborted; decision ; balance 100 |
| Participant balance sum | 200 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run two_phase_commit crash-before-decision
```

For a manual run, select this protocol, create a fresh Docker experiment with the settings below, wait for every input form, then follow the numbered steps. Pause/replay in the browser only changes what you see; the real processes continue running.

```json
{
  "runtime": "docker",
  "nodeCount": 3,
  "latency": 80,
  "bandwidth": 128,
  "seed": 42
}
```

1. On **node-1**, run `Application.Transfer` with `{"from":"node-2","to":"node-3","amount":30,"pauseBeforeDecisionMs":30000}`.
2. Wait up to 20 seconds for `{"node":"node-1","path":"votes.node-3","op":"==","value":"yes"}`.
3. Crash **node-1** using the node fault control.
4. Wait up to 20 seconds for `{"node":"node-2","path":"state","op":"==","value":"uncertain"}`.
5. Recover **node-1** using the node fault control.
6. Wait 2 seconds while the real nodes continue running.
7. Wait up to 20 seconds for `{"node":"node-1","path":"phase","op":"==","value":"finished"}`.
8. Verify `{"node":"node-1","path":"decision","op":"==","value":"abort"}`.
9. Verify `{"node":"node-2","path":"balance","op":"==","value":100}`.
10. Verify `{"node":"node-3","path":"balance","op":"==","value":100}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
