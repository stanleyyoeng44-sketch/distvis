# Quorum variant: W=R=1 stale read

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Every node was configured with W=R=1, and node-1 was isolated before writing x=1. Node-1 acknowledged its own write, while node-3 read its old local version. The read reported stale=true relative to the supplied minCounter=1, demonstrating the cost of nonintersecting quorums. The stale flag is a caller-relative diagnostic, not impossible knowledge of the latest value on an unreachable replica.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `aaca5306-1249-4d96-93e4-74870854e0bb`; recorded `2026-10-29T09:26:34.652Z`.
- [Platform replay](http://localhost:3000/#experiment/910ada43-63a6-47ce-8288-74202194744f/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 103; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 43 node-1 | x='1'; version {"counter":1,"writer":"node-1"}; 9.1 ms; stale=False |
| seq 59 node-3 | x=''; version {"counter":0,"writer":""}; 0.1 ms; stale=True |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run quorum-kv variant-nonintersection
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

1. On **node-1**, run `Application.Mode` with `{"w":1,"r":1}`.
2. On **node-2**, run `Application.Mode` with `{"w":1,"r":1}`.
3. On **node-3**, run `Application.Mode` with `{"w":1,"r":1}`.
4. On **node-4**, run `Application.Mode` with `{"w":1,"r":1}`.
5. On **node-5**, run `Application.Mode` with `{"w":1,"r":1}`.
6. Apply network fault `{"kind":"link","from":"node-1","to":"node-2","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
7. Apply network fault `{"kind":"link","from":"node-1","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
8. Apply network fault `{"kind":"link","from":"node-1","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
9. Apply network fault `{"kind":"link","from":"node-1","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
10. On **node-1**, run `Application.Put` with `{"key":"x","value":"1"}`.
11. On **node-3**, run `Application.Get` with `{"key":"x","minCounter":1}`.
12. Verify `{"node":"node-3","path":"lastGet.stale","op":"==","value":true}`.
13. Verify `{"node":"node-3","path":"lastGet.item.version.counter","op":"==","value":0}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
