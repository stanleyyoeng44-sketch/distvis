# 2PC variant: YES without durable prepared record

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Node-2 deliberately skipped saving its prepared record before voting YES. It then crashed after the coordinator had recorded commit and restarted without its participation record. Node-3 credited 30, while node-2 retained 100 and rejected commit because its prepared record was missing, leaving a total of 230. The coordinator could not finish, demonstrating that retrying cannot reconstruct a forgotten promise safely.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `471491e4-cf53-445c-b649-ff473cf3295b`; recorded `2026-10-29T09:22:06.457Z`.
- [Platform replay](http://localhost:3000/#experiment/9cf8ad37-5ccb-4a4a-8fb3-e0687036442a/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 73; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | role TM; transaction T1; phase/state decided; decision commit; balance 0 |
| node-2 | role RM; transaction ; phase/state idle; decision ; balance 100 |
| node-3 | role RM; transaction T1; phase/state committed; decision ; balance 130 |
| Participant balance sum | 230 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run two_phase_commit variant-no-prepare-log
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

1. On **node-2**, run `Application.Variant` with `{"skipPrepareSave":true}`.
2. On **node-1**, run `Application.Transfer` with `{"from":"node-2","to":"node-3","amount":30,"pauseAfterDecisionMs":4000}`.
3. Wait up to 20 seconds for `{"node":"node-1","path":"decision","op":"==","value":"commit"}`.
4. Crash **node-2** using the node fault control.
5. Recover **node-2** using the node fault control.
6. Wait 2 seconds while the real nodes continue running.
7. Wait up to 20 seconds for `{"node":"node-3","path":"state","op":"==","value":"committed"}`.
8. Verify `{"node":"node-2","path":"balance","op":"==","value":100}`.
9. Verify `{"node":"node-3","path":"balance","op":"==","value":130}`.
10. Verify `{"node":"node-1","path":"phase","op":"==","value":"decided"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
