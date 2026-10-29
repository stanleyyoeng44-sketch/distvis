# Paxos KV control: intersection remembers chosen value

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The same overlapping-majority experiment ran with ordinary persistence. After node-3 recovered, the new proposer learned the value already chosen for slot 1 and retained it. Its own different write completed in slot 2 instead. This is the safe control: final KV values can legitimately change in a later slot, but a previously chosen slot must not change.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `7b5de0e3-3e80-4992-9216-7483d829360a`; recorded `2026-10-29T09:27:25.948Z`.
- [Platform replay](http://localhost:3000/#experiment/1dabf1b0-a2c7-4f9e-9e8e-ef48ba4b46d0/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 243; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | applied 1; values {"x":"first"}; conflicts 0 |
| node-2 | applied 1; values {"x":"first"}; conflicts 0 |
| node-3 | applied 1; values {"x":"first"}; conflicts 0 |
| node-4 | applied 2; values {"x":"second"}; conflicts 1 |
| node-5 | applied 1; values {"x":"first"}; conflicts 0 |
| Chosen-slot check | conflicting slots: [] |
| Slot 1: observed majority acceptance | seq 58, 812.5 ms, ballot 17, acceptors ["node-1","node-2","node-3"] |
| Slot 2: observed majority acceptance | seq 209, 4893.9 ms, ballot 36, acceptors ["node-3","node-4","node-5"] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run paxos-kv persistence-control
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

1. Apply network fault `{"kind":"link","from":"node-1","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
2. Apply network fault `{"kind":"link","from":"node-1","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
3. Apply network fault `{"kind":"link","from":"node-2","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
4. Apply network fault `{"kind":"link","from":"node-2","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
5. Apply network fault `{"kind":"link","from":"node-3","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
6. Apply network fault `{"kind":"link","from":"node-3","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
7. On **node-1**, run `Application.Put` with `{"key":"x","value":"first"}`.
8. Wait up to 20 seconds for `{"node":"node-3","path":"applied","op":"==","value":1}`.
9. Crash **node-3** using the node fault control.
10. Restore all links. This does not recover crashed processes or replay dropped messages.
11. Apply network fault `{"kind":"link","from":"node-1","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
12. Apply network fault `{"kind":"link","from":"node-1","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
13. Apply network fault `{"kind":"link","from":"node-1","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
14. Apply network fault `{"kind":"link","from":"node-2","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
15. Apply network fault `{"kind":"link","from":"node-2","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
16. Apply network fault `{"kind":"link","from":"node-2","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
17. Recover **node-3** using the node fault control.
18. Wait 2 seconds while the real nodes continue running.
19. On **node-4**, run `Application.Put` with `{"key":"x","value":"second"}`.
20. Verify `{"node":"node-4","path":"log.1.value.value","op":"==","value":"first"}`.
21. Verify `{"node":"node-4","path":"log.2.value.value","op":"==","value":"second"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
