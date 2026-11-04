# RA crash: uninvolved peer blocks

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Crashing the idle node-2 prevented node-1 from collecting every permission. Node-1 remained waiting with entries=0 and explicitly listed node-2 in missingAcks. This matches Ricart–Agrawala's crash-stop limitation [a failed process is not removed from membership]. A timeout is never treated as permission to enter.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `ea805f80-9426-4cb8-bd9f-9c85f3369e72`; recorded `2026-10-29T09:25:06.549Z`.
- [Platform replay](http://localhost:3000/#experiment/e125cdf2-5c60-4016-9822-3e38ae6ca54b/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 38; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 0; completed 0; wait 0.0 ms; algorithm messages 2; role waiting |
| node-2 | entries 0; completed 0; wait 0.0 ms; algorithm messages 0; role idle |
| node-3 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| Full history | maximum simultaneous critical nodes = 0 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run ricart-agrawala crash
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

1. Crash **node-2** using the node fault control.
2. On **node-1**, run `Application.Acquire` with `{"holdMs":200}`.
3. Wait 4 seconds while the real nodes continue running.
4. Verify `{"node":"node-1","path":"role","op":"==","value":"waiting"}`.
5. Verify `{"node":"node-1","path":"entries","op":"==","value":0}`.
6. Verify `{"node":"node-1","path":"missingAcks","op":"includes","value":"node-2"}`.
7. Verify `{"atMostOne":{"path":"role","value":"critical"}}` — No overlapping critical sections in reported history.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
