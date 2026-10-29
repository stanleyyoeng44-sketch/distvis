# Berkeley: discard outlier vote

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Node-3 was set to +10000 ms before a round, and the coordinator excluded its measurement from the average. The retained offsets were approximately 0 and 750 ms, giving a mean near 375 ms instead of approximately 3583 ms. Node-3 still received a correction of about −9625 ms: exclusion removes its influence on the target, not its eligibility to be corrected. The recording shows the correction scheduled, not its completion; at this slew rate the large correction needs roughly 96 seconds.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `085ba2da-689f-4f8d-9fc6-98e412d7822d`; recorded `2026-10-29T09:16:44.573Z`.
- [Platform replay](http://localhost:3000/#experiment/eede1acf-d1ae-447c-8693-655dd333cf7f/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 52; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | round 1; error -18.40 ms; last adjustment 374.44 ms; remaining 356.65 ms |
| node-2 | round 1; error 713.37 ms; last adjustment -374.44 ms; remaining -374.44 ms |
| node-3 | round 1; error 10000.12 ms; last adjustment -9661.09 ms; remaining -9661.09 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run berkeley outlier
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
2. On **node-3**, run `Application.SetClock` with `{"offsetMS":10000}`.
3. On **node-1**, run `Application.Sync` with `{}`.
4. Verify `{"node":"node-1","path":"lastRound.samples.2.included","op":"==","value":false}`.
5. Verify `{"node":"node-1","path":"lastRound.meanMS","op":"<","value":500}`.
6. Verify `{"node":"node-3","path":"lastAdjMs","op":"<","value":-9000}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
