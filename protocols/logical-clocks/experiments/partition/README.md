# Clocks partition: local events continue

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

A blocked link prevented delivery, but each side could still record local events. The sender retained its failed send and reported that delivery was unconfirmed. After healing, a newly submitted message delivered successfully and its receive event merged the sender's clock. No old dropped message was replayed automatically.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `2f8f58d3-a77b-446f-a198-e81b81613eb0`; recorded `2026-10-29T09:18:25.670Z`.
- [Platform replay](http://localhost:3000/#experiment/d1cf9b69-273f-40d8-9203-1f0cd3ebd3c4/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 35; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | {"lamport":2,"vector":[2,0,0],"total":6} |
| node-2 | {"lamport":0,"vector":[0,0,0],"total":0} |
| node-3 | {"lamport":3,"vector":[2,0,2],"total":11} |
| Independent graph check | 4 events; 4 vector-causal pairs; broken send/receive edges [] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run logical-clocks partition
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

1. Apply network fault `{"kind":"link","from":"node-1","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
2. On **node-1**, run `Application.Send` with `{"to":"node-3","label":"lost","receiveLabel":"absent"}`.
3. Verify `{"node":"node-1","path":"lastDelivery.confirmed","op":"==","value":false}`.
4. On **node-3**, run `Application.Local` with `{"label":"independent"}`.
5. Restore all links. This does not recover crashed processes or replay dropped messages.
6. On **node-1**, run `Application.Send` with `{"to":"node-3","label":"new-send","receiveLabel":"new-receive"}`.
7. Verify `{"node":"node-1","path":"lastDelivery.confirmed","op":"==","value":true}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
