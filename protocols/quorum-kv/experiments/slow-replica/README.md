# Quorum: fastest majority versus all replicas

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The node-1 to node-5 link was delayed by 3000 ms. With W=3, the write finished without waiting for that slow replica; with W=5, it had to wait for the delayed acknowledgement. The measured times are listed below and should not be read as exact deterministic latencies. The fast case includes this implementation's version-query round, while the slow case illustrates the extra cost of requiring every replica.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `11ce421e-b32b-4c4a-bb00-f81be2da4acc`; recorded `2026-10-29T09:26:08.840Z`.
- [Platform replay](http://localhost:3000/#experiment/f8c6c967-83f9-439a-aa7d-5a912a3e1e6f/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 133; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 62 node-1 | x='1'; version {"counter":1,"writer":"node-1"}; 354.9 ms; stale=False |
| seq 132 node-1 | x='2'; version {"counter":2,"writer":"node-1"}; 3278.0 ms; stale=False |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run quorum-kv slow-replica
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

1. Apply network fault `{"kind":"link","from":"node-1","to":"node-5","latency":3000}`. Omitted link delay/bandwidth use this experiment's defaults.
2. On **node-1**, run `Application.Put` with `{"key":"x","value":"1"}`.
3. Verify `{"node":"node-1","path":"lastPut.waitedMs","op":"<","value":2000}`.
4. On **node-1**, run `Application.Mode` with `{"w":5,"r":3}`.
5. On **node-1**, run `Application.Put` with `{"key":"x","value":"2"}`.
6. Verify `{"node":"node-1","path":"lastPut.waitedMs","op":">=","value":3000}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
