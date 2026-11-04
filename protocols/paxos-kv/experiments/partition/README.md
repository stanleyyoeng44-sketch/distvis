# Paxos KV partition: minority cannot commit

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The two-node component could not obtain three promises and its write returned an error after the bounded retry period. The majority component committed successfully. Once links healed, later traffic triggered recovery of the missed slots and all replicas stored the majority's value. A timed-out proposal is not guaranteed absent; the report distinguishes missing confirmation from rollback.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `755309e4-9c1b-47bd-93e5-276399fe2b76`; recorded `2026-10-29T09:27:05.398Z`.
- [Platform replay](http://localhost:3000/#experiment/bad279e6-d81a-4060-b8f2-81b43ebb24a5/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 456; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | applied 2; values {"x":"majority"}; conflicts 0 |
| node-2 | applied 2; values {"x":"majority"}; conflicts 0 |
| node-3 | applied 2; values {"x":"majority"}; conflicts 0 |
| node-4 | applied 2; values {"x":"majority"}; conflicts 0 |
| node-5 | applied 2; values {"x":"majority"}; conflicts 0 |
| Chosen-slot check | conflicting slots: [] |
| Slot 1: observed majority acceptance | seq 136, 7713.8 ms, ballot 17, acceptors ["node-1","node-2","node-3"] |
| Slot 2: observed majority acceptance | seq 257, 8115.3 ms, ballot 33, acceptors ["node-1","node-2","node-5"] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run paxos-kv partition
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
7. On **node-4**, run `Application.Put` with `{"key":"x","value":"minority"}`. Expect an error [the required failure observation], not successful completion.
8. On **node-1**, run `Application.Put` with `{"key":"x","value":"majority"}`.
9. Restore all links. This does not recover crashed processes or replay dropped messages.
10. On **node-1**, run `Application.Get` with `{"key":"x"}`.
11. Wait up to 30 seconds for `{"node":"*","path":"applied","op":"==","value":2}`.
12. Verify `{"node":"*","path":"values.x","op":"==","value":"majority"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
