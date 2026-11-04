# RA design: four messages, no Release

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

A single three-node acquisition generated two Request calls, two Ack calls, and zero Release calls. Its four-message cost is lower than the six-message Lamport control recorded in the Lamport experiment folder. Deferred replies carry the permission that becomes available on exit, so a separate release broadcast is unnecessary. The improvement saves messages but does not tolerate an unreachable member.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `0120ce39-976a-4499-8a8b-5a5579092526`; recorded `2026-10-29T09:25:59.507Z`.
- [Platform replay](http://localhost:3000/#experiment/6291c014-e0f4-4a2b-958f-a384cee22aa9/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 52; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 1; completed 1; wait 174.0 ms; algorithm messages 2; role idle |
| node-2 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| node-3 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| Full history | maximum simultaneous critical nodes = 1 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run ricart-agrawala variant-message-count
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

1. On **node-1**, run `Application.Acquire` with `{"holdMs":200}`.
2. Wait up to 20 seconds for `{"node":"node-1","path":"completed","op":"==","value":1}`.
3. Verify `{"messages":{"method":"MutexService.Request","op":"==","value":2}}`.
4. Verify `{"messages":{"method":"MutexService.Ack","op":"==","value":2}}`.
5. Verify `{"messages":{"method":"MutexService.Release","op":"==","value":0}}`.
6. Verify `{"atMostOne":{"path":"role","value":"critical"}}` — No overlapping critical sections in reported history.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
