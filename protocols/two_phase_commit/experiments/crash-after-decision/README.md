# 2PC crash: durable commit completes

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The coordinator saved commit and then crashed before notifying participants. The participants remained uncertain with unchanged balances while the coordinator was down. On recovery, the saved decision caused commit notifications to resume, yielding balances 70/130. This contrasts with the preceding case: the durable decision write, not a participant receiving Commit, defines the commit point.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `521c0713-14f1-45af-bc27-c5ea78424075`; recorded `2026-10-29T09:21:45.061Z`.
- [Platform replay](http://localhost:3000/#experiment/763a3abd-561d-4d0b-be38-b5c16ba12411/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 72; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | role TM; transaction T1; phase/state finished; decision commit; balance 0 |
| node-2 | role RM; transaction T1; phase/state committed; decision ; balance 70 |
| node-3 | role RM; transaction T1; phase/state committed; decision ; balance 130 |
| Participant balance sum | 200 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run two_phase_commit crash-after-decision
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

1. On **node-1**, run `Application.Transfer` with `{"from":"node-2","to":"node-3","amount":30,"pauseAfterDecisionMs":30000}`.
2. Wait up to 20 seconds for `{"node":"node-1","path":"decision","op":"==","value":"commit"}`.
3. Crash **node-1** using the node fault control.
4. Wait up to 20 seconds for `{"node":"node-2","path":"state","op":"==","value":"uncertain"}`.
5. Verify `{"node":"node-2","path":"balance","op":"==","value":100}`.
6. Recover **node-1** using the node fault control.
7. Wait 2 seconds while the real nodes continue running.
8. Wait up to 20 seconds for `{"node":"node-1","path":"phase","op":"==","value":"finished"}`.
9. Verify `{"node":"node-2","path":"balance","op":"==","value":70}`.
10. Verify `{"node":"node-3","path":"balance","op":"==","value":130}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
