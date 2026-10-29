# RA normal: simultaneous node-1 and node-3

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Node-1 and node-3 submitted requests concurrently, and the smaller timestamp-and-node-ID pair entered first. The other node waited until its deferred permission [a reply withheld until the higher-priority request finishes] arrived. Both operations completed, and the independent history check found no overlapping critical sections. No Release message was required.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `d92c98f3-afb4-45bc-9e94-f87967543d30`; recorded `2026-10-29T09:24:58.521Z`.
- [Platform replay](http://localhost:3000/#experiment/a3d3a576-7200-440d-8a5c-d729089196ad/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 91; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 1; completed 1; wait 172.0 ms; algorithm messages 3; role idle |
| node-2 | entries 0; completed 0; wait 0.0 ms; algorithm messages 2; role idle |
| node-3 | entries 1; completed 1; wait 2260.4 ms; algorithm messages 3; role idle |
| Full history | maximum simultaneous critical nodes = 1 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run ricart-agrawala normal-contention
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

1. Use simultaneous input for **node-1**: `Application.Acquire({"holdMs":2000})`; **node-3**: `Application.Acquire({"holdMs":2000})`.
2. Wait up to 20 seconds for `{"node":["node-1","node-3"],"path":"completed","op":"==","value":1}`.
3. Verify `{"atMostOne":{"path":"role","value":"critical"}}` — No overlapping critical sections in reported history.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
