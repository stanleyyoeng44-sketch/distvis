# Clocks: equal Lamport concurrent events

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Each of three nodes generated its first local event without exchanging data messages. All Lamport values were 1, while the vectors were [1,0,0], [0,1,0], and [0,0,1], which are pairwise incomparable. The numeric total-order encoding produced 3, 4, and 5, equivalent to ordering the pairs (1,node-1), (1,node-2), and (1,node-3). That tie-break order has no physical or causal meaning.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `7ff081fc-6826-418c-af5c-e4ccd204e427`; recorded `2026-10-29T09:18:00.161Z`.
- [Platform replay](http://localhost:3000/#experiment/afd82763-b2b7-46d7-b33f-7d8560dcf8d6/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 31; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | {"lamport":1,"vector":[1,0,0],"total":3} |
| node-2 | {"lamport":1,"vector":[0,1,0],"total":4} |
| node-3 | {"lamport":1,"vector":[0,0,1],"total":5} |
| Independent graph check | 3 events; 0 vector-causal pairs; broken send/receive edges [] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run logical-clocks equal-concurrent
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

1. Use simultaneous input for **node-1**: `Application.Local({"label":"first"})`; **node-2**: `Application.Local({"label":"first"})`; **node-3**: `Application.Local({"label":"first"})`.
2. Verify `{"node":"*","path":"clock.lamport","op":"==","value":1}`.
3. On **node-1**, run `Application.CompareEvents` with `{"left":{"$state":{"node":"node-1","path":"events.0.timestamp"}},"right":{"$state":{"node":"node-2","path":"events.0.timestamp"}}}`. In the manual form, replace each `$state` reference with the actual current value from `node-1.events.0.timestamp`, `node-2.events.0.timestamp`. The runner resolves these references automatically; do not type `$state` into the application form.
4. Verify `{"node":"node-1","path":"lastCompare.vector","op":"==","value":"concurrent"}`.
5. Verify `{"node":"node-1","path":"lastCompare.agrees","op":"==","value":true}`.
6. Verify `{"node":"node-1","path":"lastCompare.lamport","op":"==","value":"equal"}`.
7. On **node-1**, run `Application.CompareEvents` with `{"left":{"$state":{"node":"node-1","path":"events.0.timestamp"}},"right":{"$state":{"node":"node-3","path":"events.0.timestamp"}}}`. In the manual form, replace each `$state` reference with the actual current value from `node-1.events.0.timestamp`, `node-3.events.0.timestamp`. The runner resolves these references automatically; do not type `$state` into the application form.
8. Verify `{"node":"node-1","path":"lastCompare.vector","op":"==","value":"concurrent"}`.
9. Verify `{"node":"node-1","path":"lastCompare.agrees","op":"==","value":true}`.
10. Verify `{"node":"node-1","path":"lastCompare.lamport","op":"==","value":"equal"}`.
11. On **node-1**, run `Application.CompareEvents` with `{"left":{"$state":{"node":"node-2","path":"events.0.timestamp"}},"right":{"$state":{"node":"node-3","path":"events.0.timestamp"}}}`. In the manual form, replace each `$state` reference with the actual current value from `node-2.events.0.timestamp`, `node-3.events.0.timestamp`. The runner resolves these references automatically; do not type `$state` into the application form.
12. Verify `{"node":"node-1","path":"lastCompare.vector","op":"==","value":"concurrent"}`.
13. Verify `{"node":"node-1","path":"lastCompare.agrees","op":"==","value":true}`.
14. Verify `{"node":"node-1","path":"lastCompare.lamport","op":"==","value":"equal"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
