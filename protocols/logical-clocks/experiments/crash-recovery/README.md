# Clocks crash: durable counters

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Node-2 recorded an event, crashed, and could not receive the next message. After recovery it loaded its saved counter and produced event number 2 rather than reusing event number 1. A newly submitted send was then delivered successfully. This matches the persistence rule [save the counter before reporting or sending an event], while also showing that failed delivery does not erase the original send event.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `16db3b16-c633-4696-a4bb-fa902d563ba0`; recorded `2026-10-29T09:18:14.910Z`.
- [Platform replay](http://localhost:3000/#experiment/dadc4d21-6108-4877-a0a8-532b26ab8913/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 42; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | {"lamport":2,"vector":[2,0,0],"total":6} |
| node-2 | {"lamport":3,"vector":[2,3,0],"total":10} |
| node-3 | {"lamport":0,"vector":[0,0,0],"total":0} |
| Independent graph check | 5 events; 6 vector-causal pairs; broken send/receive edges [] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run logical-clocks crash-recovery
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

1. On **node-2**, run `Application.Local` with `{"label":"before"}`.
2. Crash **node-2** using the node fault control.
3. On **node-1**, run `Application.Send` with `{"to":"node-2","label":"lost","receiveLabel":"not-received"}`.
4. Verify `{"node":"node-1","path":"lastDelivery.confirmed","op":"==","value":false}`.
5. Recover **node-2** using the node fault control.
6. Wait 2 seconds while the real nodes continue running.
7. On **node-2**, run `Application.Local` with `{"label":"after"}`.
8. Verify `{"node":"node-2","path":"clock.lamport","op":"==","value":2}`.
9. On **node-1**, run `Application.Send` with `{"to":"node-2","label":"retry-new-event","receiveLabel":"received"}`.
10. Verify `{"node":"node-1","path":"lastDelivery.confirmed","op":"==","value":true}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
