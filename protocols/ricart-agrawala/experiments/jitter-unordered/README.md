# RA random load: unordered messages

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The same five-node load ran with deliberate message reordering enabled. The exported delivery history contained actual per-link order reversals, not merely a setting permitting them; acknowledgements still named their exact request timestamp and each node counted only distinct peers. All operations completed without overlapping critical-section reports in this execution. This is evidence for the tested schedule, not a claim of progress under arbitrary message loss or node recovery.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `c9af5fef-8b91-402f-8880-b1902de63c17`; recorded `2026-10-29T09:25:42.280Z`.
- [Platform replay](http://localhost:3000/#experiment/b5fe57fd-c7b1-42df-a34a-ce0befebd3e3/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 722; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 2; completed 2; wait 4894.1 ms; algorithm messages 16; role idle |
| node-2 | entries 2; completed 2; wait 5038.6 ms; algorithm messages 16; role idle |
| node-3 | entries 2; completed 2; wait 5474.1 ms; algorithm messages 16; role idle |
| node-4 | entries 2; completed 2; wait 5307.6 ms; algorithm messages 16; role idle |
| node-5 | entries 2; completed 2; wait 5312.8 ms; algorithm messages 16; role idle |
| Full history | maximum simultaneous critical nodes = 1 |
| Actual delivery-order reversals | 19 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run ricart-agrawala jitter-unordered
```

For a manual run, select this protocol, create a fresh Docker experiment with the settings below, wait for every input form, then follow the numbered steps. Pause/replay in the browser only changes what you see; the real processes continue running.

```json
{
  "runtime": "docker",
  "nodeCount": 5,
  "latency": 80,
  "bandwidth": 128,
  "seed": 42,
  "delayModel": "exponential",
  "jitter": 80,
  "network": {
    "ordering": "unordered",
    "reorderRate": 0.5,
    "reorderMin": 100,
    "reorderMax": 400
  }
}
```

1. Use simultaneous input for **node-1**: `Application.Auto({"count":2})`; **node-2**: `Application.Auto({"count":2})`; **node-3**: `Application.Auto({"count":2})`; **node-4**: `Application.Auto({"count":2})`; **node-5**: `Application.Auto({"count":2})`.
2. Wait up to 60 seconds for `{"node":"*","path":"completed","op":"==","value":2}`.
3. Verify `{"atMostOne":{"path":"role","value":"critical"}}` — No overlapping critical sections in reported history.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
