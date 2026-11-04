# Clocks: automatic random events

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Each node generated a bounded sequence of randomly selected local and send events. CompareEvents used actual recorded timestamp pairs, and the independent checker reconstructed all causal paths from local order and messages rather than trusting the vectors themselves. Both directions of the vector-clock equivalence held, and every causal pair had a strictly increasing Lamport value in the safe run. The exact random trace can differ on replay; the required invariant [a property that must remain true] is the same.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `176f4f3a-9dba-4526-8767-64e924d3821a`; recorded `2026-10-29T09:18:05.387Z`.
- [Platform replay](http://localhost:3000/#experiment/49eeffea-acc3-42e4-bd7f-c7c949c88e9b/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 162; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | {"lamport":13,"vector":[9,6,9],"total":39} |
| node-2 | {"lamport":15,"vector":[9,11,9],"total":46} |
| node-3 | {"lamport":13,"vector":[7,6,11],"total":41} |
| Independent graph check | 31 events; 369 vector-causal pairs; broken send/receive edges [] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run logical-clocks auto-invariant
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

1. Use simultaneous input for **node-1**: `Application.Auto({"count":6})`; **node-2**: `Application.Auto({"count":6})`; **node-3**: `Application.Auto({"count":6})`.
2. Wait up to 30 seconds for `{"node":"*","path":"autoRunning","op":"==","value":false}`.
3. On **node-1**, run `Application.Read` with `{}`.
4. On **node-2**, run `Application.Read` with `{}`.
5. On **node-3**, run `Application.Read` with `{}`.
6. On **node-1**, run `Application.CompareEvents` with `{"left":{"$state":{"node":"node-1","path":"events.0.timestamp"}},"right":{"$state":{"node":"node-2","path":"clock"}}}`. In the manual form, replace each `$state` reference with the actual current value from `node-1.events.0.timestamp`, `node-2.clock`. The runner resolves these references automatically; do not type `$state` into the application form.
7. Verify `{"node":"node-1","path":"lastCompare.agrees","op":"==","value":true}`.
8. On **node-1**, run `Application.CompareEvents` with `{"left":{"$state":{"node":"node-1","path":"events.0.timestamp"}},"right":{"$state":{"node":"node-3","path":"clock"}}}`. In the manual form, replace each `$state` reference with the actual current value from `node-1.events.0.timestamp`, `node-3.clock`. The runner resolves these references automatically; do not type `$state` into the application form.
9. Verify `{"node":"node-1","path":"lastCompare.agrees","op":"==","value":true}`.
10. On **node-1**, run `Application.CompareEvents` with `{"left":{"$state":{"node":"node-2","path":"events.0.timestamp"}},"right":{"$state":{"node":"node-3","path":"clock"}}}`. In the manual form, replace each `$state` reference with the actual current value from `node-2.events.0.timestamp`, `node-3.clock`. The runner resolves these references automatically; do not type `$state` into the application form.
11. Verify `{"node":"node-1","path":"lastCompare.agrees","op":"==","value":true}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
