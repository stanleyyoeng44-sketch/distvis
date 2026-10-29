# Quorum normal: sequential write and read

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

A write on node-1 completed after collecting its configured write quorum [the required number of acknowledgements]. A read on a different node returned the same value and completed a write-back [replicating the selected version before replying]. With N=5 and W=R=3, every read quorum intersects every completed write quorum. The implementation also performs a version-query phase before writing, so it has more network rounds than a one-round teaching simplification.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `463aee55-b2df-461f-8668-4a96eb238d17`; recorded `2026-10-29T09:26:03.741Z`.
- [Platform replay](http://localhost:3000/#experiment/67fe7561-61a2-4b8c-9d2f-c8acee09d0e9/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 129; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 72 node-1 | x='1'; version {"counter":1,"writer":"node-1"}; 347.9 ms; stale=False |
| seq 126 node-3 | x='1'; version {"counter":1,"writer":"node-1"}; 339.5 ms; stale=False |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run quorum-kv normal
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

1. On **node-1**, run `Application.Put` with `{"key":"x","value":"1"}`.
2. On **node-3**, run `Application.Get` with `{"key":"x","minCounter":1}`.
3. Verify `{"node":"node-3","path":"lastGet.item.value","op":"==","value":"1"}`.
4. Verify `{"node":"node-3","path":"lastGet.stale","op":"==","value":false}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
