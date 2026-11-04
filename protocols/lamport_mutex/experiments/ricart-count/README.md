# Lamport D variant - Ricart Agrawala four messages

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

All nodes selected Ricart–Agrawala before any requests, and one acquisition completed without an explicit Release call. Two Request and two Ack calls gave four algorithm messages, matching 2(n−1). This reduction requires removing the distributed queue dependency as well as the Release message. Deleting Release alone from Lamport mode would leave stale queue entries that prevent later progress.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `cf8236dd-6cdb-43e9-aef1-32549d8c79ba`; recorded `2026-10-29T09:24:18.785Z`.
- [Platform replay](http://localhost:3000/#experiment/696da9a5-96d9-41ea-9e37-25d25e7b7973/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 61; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 1; completed 1; wait 173.6 ms; algorithm messages 2; role idle |
| node-2 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| node-3 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| Full history | maximum simultaneous critical nodes = 1 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run lamport_mutex ricart-count
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

1. Use simultaneous input for **node-1**: `Application.Configure({"algorithm":"ricart-agrawala"})`; **node-2**: `Application.Configure({"algorithm":"ricart-agrawala"})`; **node-3**: `Application.Configure({"algorithm":"ricart-agrawala"})`.
2. On **node-1**, run `Application.Acquire` with `{"holdMs":2000}`.
3. Wait up to 20 seconds for `{"node":"node-1","path":"completed","value":1}`.
4. Verify `{"messages":{"method":"MutexService.Request","op":"==","value":2}}`.
5. Verify `{"messages":{"method":"MutexService.Ack","op":"==","value":2}}`.
6. Verify `{"messages":{"method":"MutexService.Release","op":"==","value":0}}`.
7. Verify `{"messages":{"op":"==","value":4}}`.
8. Verify `{"atMostOne":{"path":"role","value":"critical"}}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
