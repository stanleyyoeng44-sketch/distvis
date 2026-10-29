# Quorum crash: majority still available

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Node-5 crashed while four replicas remained reachable. A majority write and read still completed, showing that one failure does not exhaust a three-of-five quorum. After recovery, node-5's own read queried peers and repaired its local value. Recovery does not depend on an invisible global store: the ordinary quorum operations carry the data.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `6dd9f85c-5501-4920-9266-3d9a8f332dfa`; recorded `2026-10-29T09:26:16.949Z`.
- [Platform replay](http://localhost:3000/#experiment/f1bfad0a-6bea-45b3-990f-410fbfae6ded/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 173; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 65 node-1 | x='1'; version {"counter":1,"writer":"node-1"}; 357.1 ms; stale=False |
| seq 112 node-3 | x='1'; version {"counter":1,"writer":"node-1"}; 343.5 ms; stale=False |
| seq 172 node-5 | x='1'; version {"counter":1,"writer":"node-1"}; 349.0 ms; stale=False |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run quorum-kv crash
```

For a manual run, select this protocol, create a fresh Docker experiment with the settings below, wait for every input form, then follow the numbered steps. Pause/replay in the browser only changes what you see; the real processes continue running.

```json
{
  "runtime": "docker",
  "nodeCount": 5,
  "latency": 80,
  "bandwidth": 128,
  "seed": 42
}
```

1. Crash **node-5** using the node fault control.
2. On **node-1**, run `Application.Put` with `{"key":"x","value":"1"}`.
3. On **node-3**, run `Application.Get` with `{"key":"x"}`.
4. Verify `{"node":"node-3","path":"lastGet.item.value","op":"==","value":"1"}`.
5. Recover **node-5** using the node fault control.
6. Wait 2 seconds while the real nodes continue running.
7. On **node-5**, run `Application.Get` with `{"key":"x"}`.
8. Verify `{"node":"node-5","path":"lastGet.item.value","op":"==","value":"1"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
