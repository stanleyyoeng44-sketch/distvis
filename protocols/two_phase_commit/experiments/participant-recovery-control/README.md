# 2PC control: prepared log survives participant crash

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The participant-crash experiment was repeated with the prepared-state write enabled. Node-2 recovered its pending debit and could honor the coordinator's saved commit. Both participants finished with balances 70/130 and the coordinator reached finished. Repeated commit notifications did not apply the debit twice, because the committed state is durable and duplicate application is rejected.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `e2b342eb-cde7-4cdf-9145-ad1351d03c7f`; recorded `2026-10-29T09:22:14.967Z`.
- [Platform replay](http://localhost:3000/#experiment/c7be7db7-727f-4c45-8036-7ebd302595f0/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 84; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

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
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run two_phase_commit participant-recovery-control
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

1. On **node-1**, run `Application.Transfer` with `{"from":"node-2","to":"node-3","amount":30,"pauseAfterDecisionMs":4000}`.
2. Wait up to 20 seconds for `{"node":"node-1","path":"decision","op":"==","value":"commit"}`.
3. Crash **node-2** using the node fault control.
4. Recover **node-2** using the node fault control.
5. Wait 2 seconds while the real nodes continue running.
6. Wait up to 20 seconds for `{"node":"node-1","path":"phase","op":"==","value":"finished"}`.
7. Verify `{"node":"node-2","path":"balance","op":"==","value":70}`.
8. Verify `{"node":"node-3","path":"balance","op":"==","value":130}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
