# Lamport regression - fifteen second hold

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

A 15000 ms hold was accepted promptly rather than keeping the application RPC open for its entire duration. The node was still critical after eleven seconds and later completed normally. This confirms that application acceptance and lock completion are separate observable events. The replay still contained no overlapping critical sections.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `731cf150-3abc-4b59-817d-39add1b8ff89`; recorded `2026-10-29T09:24:25.197Z`.
- [Platform replay](http://localhost:3000/#experiment/5d53e4bd-a06d-45f6-8f98-6c171acb364f/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 68; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 1; completed 1; wait 172.5 ms; algorithm messages 4; role idle |
| node-2 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| node-3 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| Full history | maximum simultaneous critical nodes = 1 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run lamport_mutex long-hold
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

1. On **node-1**, run `Application.Acquire` with `{"holdMs":15000}`.
2. Wait up to 20 seconds for `{"node":"node-1","path":"role","value":"critical"}`.
3. Wait 11 seconds while the real nodes continue running.
4. Verify `{"node":"node-1","path":"role","value":"critical"}`.
5. Wait up to 20 seconds for `{"node":"node-1","path":"completed","value":1}`.
6. Wait up to 20 seconds for `{"node":"*","path":"queue","value":[]}`.
7. Verify `{"node":"node-1","path":"operationRunning","value":false}`.
8. Verify `{"atMostOne":{"path":"role","value":"critical"}}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
