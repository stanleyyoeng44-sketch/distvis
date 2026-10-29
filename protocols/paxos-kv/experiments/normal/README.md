# Paxos KV normal: reads also occupy slots

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Put and Get operations submitted through different nodes occupied consecutive log slots. The read after the second write returned the updated value, and all replicas applied the four slots in order. A Get is itself a chosen operation rather than an unchecked local read. This is the mechanism that orders reads with writes and supports linearizability [each completed operation appears to occur at one point between its invocation and response].

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `4923e01c-8c5f-4925-b636-8859bcd2736a`; recorded `2026-10-29T09:26:42.103Z`.
- [Platform replay](http://localhost:3000/#experiment/0d825339-1510-4ce9-b08f-38512a5bc717/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 504; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | applied 4; values {"x":"2"}; conflicts 0 |
| node-2 | applied 4; values {"x":"2"}; conflicts 1 |
| node-3 | applied 4; values {"x":"2"}; conflicts 1 |
| node-4 | applied 4; values {"x":"2"}; conflicts 0 |
| node-5 | applied 4; values {"x":"2"}; conflicts 0 |
| Chosen-slot check | conflicting slots: [] |
| Slot 1: observed majority acceptance | seq 63, 451.4 ms, ballot 17, acceptors ["node-1","node-3","node-5"] |
| Slot 2: observed majority acceptance | seq 245, 1239.8 ms, ballot 34, acceptors ["node-2","node-3","node-5"] |
| Slot 3: observed majority acceptance | seq 366, 2028.4 ms, ballot 35, acceptors ["node-1","node-2","node-3"] |
| Slot 4: observed majority acceptance | seq 459, 2507.4 ms, ballot 20, acceptors ["node-3","node-4","node-5"] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run paxos-kv normal
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

1. On **node-1**, run `Application.Put` with `{"key":"x","value":"1"}`.
2. On **node-2**, run `Application.Get` with `{"key":"x"}`.
3. On **node-3**, run `Application.Put` with `{"key":"x","value":"2"}`.
4. On **node-4**, run `Application.Get` with `{"key":"x"}`.
5. Wait up to 20 seconds for `{"node":"*","path":"applied","op":"==","value":4}`.
6. Verify `{"node":"node-4","path":"lastOperation.value","op":"==","value":"2"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
