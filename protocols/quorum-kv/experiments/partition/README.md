# Quorum partition: minority unavailable

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The two-node component could not collect three replies and its operation failed. The three-node component continued writing and reading, and a read from the former minority succeeded after healing. This demonstrates consistency-oriented availability [only the component able to form an intersecting quorum can complete operations]. A timeout does not roll back a partially delivered write.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `13abd5a6-e7b5-4ebc-bf04-f358b6386344`; recorded `2026-10-29T09:26:25.102Z`.
- [Platform replay](http://localhost:3000/#experiment/1ee9dec6-f06a-4736-81cc-102a834bfd31/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 171; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 75 node-3 | x='majority'; version {"counter":1,"writer":"node-3"}; 362.0 ms; stale=False |
| seq 113 node-4 | x='majority'; version {"counter":1,"writer":"node-3"}; 344.9 ms; stale=False |
| seq 169 node-1 | x='majority'; version {"counter":1,"writer":"node-3"}; 349.4 ms; stale=False |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run quorum-kv partition
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

1. Apply network fault `{"kind":"link","from":"node-1","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
2. Apply network fault `{"kind":"link","from":"node-1","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
3. Apply network fault `{"kind":"link","from":"node-1","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
4. Apply network fault `{"kind":"link","from":"node-2","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
5. Apply network fault `{"kind":"link","from":"node-2","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
6. Apply network fault `{"kind":"link","from":"node-2","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
7. On **node-1**, run `Application.Put` with `{"key":"minority","value":"no"}`. Expect an error [the required failure observation], not successful completion.
8. On **node-3**, run `Application.Put` with `{"key":"x","value":"majority"}`.
9. On **node-4**, run `Application.Get` with `{"key":"x"}`.
10. Verify `{"node":"node-4","path":"lastGet.item.value","op":"==","value":"majority"}`.
11. Restore all links. This does not recover crashed processes or replay dropped messages.
12. On **node-1**, run `Application.Get` with `{"key":"x"}`.
13. Verify `{"node":"node-1","path":"lastGet.item.value","op":"==","value":"majority"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
