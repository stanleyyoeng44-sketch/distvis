# RA partition: no replay after healing

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

A blocked link prevented one required reply, leaving the requester outside its critical section. Healing did not replay the dropped request, so it remained waiting during the later observation. This is expected because this teaching implementation has no retransmission or recovery protocol. Mutual exclusion was preserved at the expense of progress.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `a4edab5f-e88d-4d55-9ac4-95706b613609`; recorded `2026-10-29T09:25:14.634Z`.
- [Platform replay](http://localhost:3000/#experiment/51aaf8e4-e84e-493a-a049-bfafe670eccb/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 38; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 0; completed 0; wait 0.0 ms; algorithm messages 2; role waiting |
| node-2 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| node-3 | entries 0; completed 0; wait 0.0 ms; algorithm messages 0; role idle |
| Full history | maximum simultaneous critical nodes = 0 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run ricart-agrawala partition
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

1. Apply network fault `{"kind":"link","from":"node-1","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
2. On **node-1**, run `Application.Acquire` with `{"holdMs":200}`.
3. Wait 4 seconds while the real nodes continue running.
4. Verify `{"node":"node-1","path":"entries","op":"==","value":0}`.
5. Restore all links. This does not recover crashed processes or replay dropped messages.
6. Wait 2 seconds while the real nodes continue running.
7. Verify `{"node":"node-1","path":"entries","op":"==","value":0}`.
8. Verify `{"atMostOne":{"path":"role","value":"critical"}}` — No overlapping critical sections in reported history.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
