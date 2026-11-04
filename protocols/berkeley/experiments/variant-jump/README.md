# Berkeley variant: jump versus slew

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The smooth run scheduled a negative correction on node-3 and initially left a nonzero slewMsLeft. After resetting the same offsets, jump mode applied approximately −750 ms at once and left no pending correction. The paired before/after readings at the same adjustment instant measured a decrease in jump mode, while smooth mode left that instantaneous reading unchanged. Smooth mode instead slows a fast clock while keeping its rate positive, which preserves monotonicity [later readings remain greater than earlier readings].

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `819f169a-204c-430f-9f2e-575b971795f4`; recorded `2026-10-29T09:17:37.567Z`.
- [Platform replay](http://localhost:3000/#experiment/b8a4f0da-fc71-4398-abb6-5fd9ebf754c5/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 133; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | round 2; error 750.29 ms; last adjustment 750.39 ms; remaining 0.00 ms |
| node-2 | round 2; error 750.42 ms; last adjustment -0.38 ms; remaining 0.00 ms |
| node-3 | round 2; error 749.44 ms; last adjustment -750.01 ms; remaining 0.00 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run berkeley variant-jump
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

1. On **node-1**, run `Application.Auto` with `{"enabled":false}`.
2. On **node-1**, run `Application.Sync` with `{}`.
3. Verify `{"node":"node-3","path":"slewMsLeft","op":"<","value":-500}`.
4. Verify `{"node":"node-3","path":"clockAfterAdjustmentMS","op":"==","value":{"$state":{"node":"node-3","path":"clockBeforeAdjustmentMS"}}}`.
5. Wait 9 seconds while the real nodes continue running.
6. On **node-1**, run `Application.ConfigureClock` with `{"offsetMS":0}`.
7. On **node-2**, run `Application.ConfigureClock` with `{"offsetMS":750}`.
8. On **node-3**, run `Application.ConfigureClock` with `{"offsetMS":1500}`.
9. On **node-1**, run `Application.SetMode` with `{"step":true}`.
10. On **node-2**, run `Application.SetMode` with `{"step":true}`.
11. On **node-3**, run `Application.SetMode` with `{"step":true}`.
12. On **node-1**, run `Application.Sync` with `{}`.
13. Verify `{"node":"node-3","path":"slewMsLeft","op":"==","value":0}`.
14. Verify `{"node":"node-3","path":"lastAdjMs","op":"<","value":-600}`.
15. Verify `{"node":"node-3","path":"clockAfterAdjustmentMS","op":"<","value":{"$state":{"node":"node-3","path":"clockBeforeAdjustmentMS"}}}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
