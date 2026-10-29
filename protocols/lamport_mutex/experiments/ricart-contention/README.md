# Lamport variant regression - three competing nodes

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Three nodes each completed three acquisitions in Ricart–Agrawala mode. All nine operations finished without overlapping critical-section reports, and the trace contained thirty-six algorithm messages. No Release calls were sent. Random delays between requests change the precise timing but not the all-peer permission requirement.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `a2871177-ea11-45a2-8c12-b120fa4070f1`; recorded `2026-10-29T09:24:44.200Z`.
- [Platform replay](http://localhost:3000/#experiment/64aaf2f3-3370-43ef-8240-74e4be88f2df/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 364; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 3; completed 3; wait 1847.9 ms; algorithm messages 12; role idle |
| node-2 | entries 3; completed 3; wait 1713.4 ms; algorithm messages 12; role idle |
| node-3 | entries 3; completed 3; wait 1670.0 ms; algorithm messages 12; role idle |
| Full history | maximum simultaneous critical nodes = 1 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run lamport_mutex ricart-contention
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
2. Use simultaneous input for **node-1**: `Application.Auto({"count":3})`; **node-2**: `Application.Auto({"count":3})`; **node-3**: `Application.Auto({"count":3})`.
3. Wait up to 30 seconds for `{"node":"*","path":"completed","value":3}`.
4. Wait up to 20 seconds for `{"node":"*","path":"autoRunning","value":false}`.
5. Verify `{"node":"*","path":"role","value":"idle"}`.
6. Verify `{"atMostOne":{"path":"role","value":"critical"}}`.
7. Verify `{"messages":{"op":"==","value":36}}`.
8. Verify `{"messages":{"method":"MutexService.Release","op":"==","value":0}}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
