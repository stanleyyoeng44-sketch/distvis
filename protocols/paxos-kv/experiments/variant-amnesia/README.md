# Paxos KV variant: forgetful intersection accepts conflicting slot

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Node-3 was configured not to save Paxos state, then formed the intersection of two different three-node majorities. The first majority chose one value for slot 1, after which node-3 crashed and forgot its promise and accepted value. A second majority then chose a different value for the same slot. The independent export checker confirmed conflicting slot-1 decisions, demonstrating why stable storage [state that survives process restart] is part of Paxos safety.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `58ff85df-be79-4220-8634-e250a66fbbb4`; recorded `2026-10-29T09:27:17.898Z`.
- [Platform replay](http://localhost:3000/#experiment/343f7b1b-1c3a-40d8-bbd2-112e00ea0f44/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 151; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | applied 1; values {"x":"first"}; conflicts 0 |
| node-2 | applied 1; values {"x":"first"}; conflicts 0 |
| node-3 | applied 0; values {}; conflicts 0 |
| node-4 | applied 1; values {"x":"second"}; conflicts 0 |
| node-5 | applied 0; values {}; conflicts 0 |
| Chosen-slot check | conflicting slots: ["1"] |
| Slot 1: observed majority acceptance | seq 61, 1045.6 ms, ballot 17, acceptors ["node-1","node-2","node-3"] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run paxos-kv variant-amnesia
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

1. On **node-3**, run `Application.Amnesia` with `{"enabled":true}`.
2. Apply network fault `{"kind":"link","from":"node-1","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
3. Apply network fault `{"kind":"link","from":"node-1","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
4. Apply network fault `{"kind":"link","from":"node-2","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
5. Apply network fault `{"kind":"link","from":"node-2","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
6. Apply network fault `{"kind":"link","from":"node-3","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
7. Apply network fault `{"kind":"link","from":"node-3","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
8. On **node-1**, run `Application.Put` with `{"key":"x","value":"first"}`.
9. Wait up to 20 seconds for `{"node":"node-3","path":"applied","op":"==","value":1}`.
10. Crash **node-3** using the node fault control.
11. Restore all links. This does not recover crashed processes or replay dropped messages.
12. Apply network fault `{"kind":"link","from":"node-1","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
13. Apply network fault `{"kind":"link","from":"node-1","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
14. Apply network fault `{"kind":"link","from":"node-1","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
15. Apply network fault `{"kind":"link","from":"node-2","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
16. Apply network fault `{"kind":"link","from":"node-2","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
17. Apply network fault `{"kind":"link","from":"node-2","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
18. Recover **node-3** using the node fault control.
19. Wait 2 seconds while the real nodes continue running.
20. On **node-4**, run `Application.Put` with `{"key":"x","value":"second"}`.
21. Verify `{"node":"node-1","path":"values.x","op":"==","value":"first"}`.
22. Verify `{"node":"node-4","path":"values.x","op":"==","value":"second"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
