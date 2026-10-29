# Clocks variant: omit receive merge

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The receiver was deliberately configured to omit the receive-side maximum merge. A send with Lamport value 2 then caused a receive labeled 1, and the two vectors incorrectly appeared concurrent. The independent checker detected this broken send-to-receive edge. CompareEvents.agrees alone remained true because it only checks the implication from the supplied vector relation; the actual recorded message edge is the evidence that the modified clock algorithm is wrong.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `a7ffc329-97cd-48ee-b58f-8f735b707fcf`; recorded `2026-10-29T09:18:33.286Z`.
- [Platform replay](http://localhost:3000/#experiment/c5e86c67-d7cd-4abd-9276-624d005f09ea/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 33; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | {"lamport":2,"vector":[2,0,0],"total":6} |
| node-2 | {"lamport":1,"vector":[0,1,0],"total":4} |
| node-3 | {"lamport":0,"vector":[0,0,0],"total":0} |
| Independent graph check | 3 events; 1 vector-causal pairs; broken send/receive edges [["node-1:2","node-2:1"]] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run logical-clocks variant-no-merge
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

1. On **node-2**, run `Application.Variant` with `{"omitReceiveMerge":true}`.
2. On **node-1**, run `Application.Local` with `{"label":"a"}`.
3. On **node-1**, run `Application.Send` with `{"to":"node-2","label":"b","receiveLabel":"c"}`.
4. Verify `{"node":"node-2","path":"clock.lamport","op":"==","value":1}`.
5. On **node-1**, run `Application.CompareEvents` with `{"left":{"$state":{"node":"node-1","path":"events.1.timestamp"}},"right":{"$state":{"node":"node-2","path":"events.0.timestamp"}}}`. In the manual form, replace each `$state` reference with the actual current value from `node-1.events.1.timestamp`, `node-2.events.0.timestamp`. The runner resolves these references automatically; do not type `$state` into the application form.
6. Verify `{"node":"node-1","path":"lastCompare.vector","op":"==","value":"concurrent"}`.
7. Verify `{"node":"node-1","path":"lastCompare.lamport","op":"==","value":"a>b"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
