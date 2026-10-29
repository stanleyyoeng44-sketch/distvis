# Clocks normal: lecture six events

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The six modeled events produced the lecture timestamps a=1, b=2, c=3, d=4, e=1, and f=5. Comparing b and e reported L(b)>L(e), while their vectors were concurrent [neither event has a data-message path leading to the other]. Comparing b and f reported a causal order, and the Lamport order agreed. This demonstrates that scalar order alone cannot prove causality.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `5dc078ea-f3a4-4026-84eb-850f8cb30847`; recorded `2026-10-29T09:17:54.125Z`.
- [Platform replay](http://localhost:3000/#experiment/ae9b023a-a3e4-4514-bf54-1bb44c24496b/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 47; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | {"lamport":2,"vector":[2,0,0],"total":6} |
| node-2 | {"lamport":4,"vector":[2,2,0],"total":13} |
| node-3 | {"lamport":5,"vector":[2,2,2],"total":17} |
| Independent graph check | 6 events; 11 vector-causal pairs; broken send/receive edges [] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run logical-clocks normal-six-events
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

1. On **node-1**, run `Application.Local` with `{"label":"a"}`.
2. On **node-3**, run `Application.Local` with `{"label":"e"}`.
3. On **node-1**, run `Application.Send` with `{"to":"node-2","label":"b","receiveLabel":"c"}`.
4. On **node-2**, run `Application.Send` with `{"to":"node-3","label":"d","receiveLabel":"f"}`.
5. Verify `{"node":"node-1","path":"events.0.timestamp","op":"==","value":{"lamport":1,"vector":[1,0,0],"total":3}}`.
6. Verify `{"node":"node-1","path":"events.1.timestamp","op":"==","value":{"lamport":2,"vector":[2,0,0],"total":6}}`.
7. Verify `{"node":"node-2","path":"events.0.timestamp","op":"==","value":{"lamport":3,"vector":[2,1,0],"total":10}}`.
8. Verify `{"node":"node-2","path":"events.1.timestamp","op":"==","value":{"lamport":4,"vector":[2,2,0],"total":13}}`.
9. Verify `{"node":"node-3","path":"events.0.timestamp","op":"==","value":{"lamport":1,"vector":[0,0,1],"total":5}}`.
10. Verify `{"node":"node-3","path":"events.1.timestamp","op":"==","value":{"lamport":5,"vector":[2,2,2],"total":17}}`.
11. On **node-1**, run `Application.CompareEvents` with `{"left":{"$state":{"node":"node-1","path":"events.1.timestamp"}},"right":{"$state":{"node":"node-3","path":"events.0.timestamp"}}}`. In the manual form, replace each `$state` reference with the actual current value from `node-1.events.1.timestamp`, `node-3.events.0.timestamp`. The runner resolves these references automatically; do not type `$state` into the application form.
12. Verify `{"node":"node-1","path":"lastCompare.vector","op":"==","value":"concurrent"}`.
13. Verify `{"node":"node-1","path":"lastCompare.agrees","op":"==","value":true}`.
14. Verify `{"node":"node-1","path":"lastCompare.lamport","op":"==","value":"a>b"}`.
15. On **node-1**, run `Application.CompareEvents` with `{"left":{"$state":{"node":"node-1","path":"events.1.timestamp"}},"right":{"$state":{"node":"node-3","path":"events.1.timestamp"}}}`. In the manual form, replace each `$state` reference with the actual current value from `node-1.events.1.timestamp`, `node-3.events.1.timestamp`. The runner resolves these references automatically; do not type `$state` into the application form.
16. Verify `{"node":"node-1","path":"lastCompare.vector","op":"==","value":"happens-before"}`.
17. Verify `{"node":"node-1","path":"lastCompare.agrees","op":"==","value":true}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
