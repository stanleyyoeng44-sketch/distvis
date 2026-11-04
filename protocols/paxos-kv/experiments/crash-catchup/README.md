# Paxos KV crash: no-op hole recovery

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Node-5 crashed after slot 1 while the other nodes committed slots 2 through 4. After recovery, a Prepare for a later slot exposed those holes. Node-5 proposed no-ops [operations that do not change the KV data] for the holes, but Paxos recovered the already accepted real values instead. Its applied prefix reached slot 5 and its value of x matched the other replicas.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `87c72f4a-6b91-4a56-a5e0-5a10de9da513`; recorded `2026-10-29T09:26:55.819Z`.
- [Platform replay](http://localhost:3000/#experiment/1b37a1c7-5686-43ed-b7b5-e5fd4fd4d626/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 771; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | applied 5; values {"x":"4","y":"5"}; conflicts 0 |
| node-2 | applied 5; values {"x":"4","y":"5"}; conflicts 0 |
| node-3 | applied 5; values {"x":"4","y":"5"}; conflicts 0 |
| node-4 | applied 5; values {"x":"4","y":"5"}; conflicts 0 |
| node-5 | applied 5; values {"x":"4","y":"5"}; conflicts 0 |
| Chosen-slot check | conflicting slots: [] |
| Slot 1: observed majority acceptance | seq 64, 981.2 ms, ballot 17, acceptors ["node-1","node-3","node-5"] |
| Slot 2: observed majority acceptance | seq 152, 2062.0 ms, ballot 33, acceptors ["node-1","node-2","node-3"] |
| Slot 3: observed majority acceptance | seq 229, 2465.6 ms, ballot 49, acceptors ["node-1","node-2","node-3"] |
| Slot 4: observed majority acceptance | seq 306, 2878.1 ms, ballot 65, acceptors ["node-1","node-2","node-4"] |
| Slot 5: observed majority acceptance | seq 461, 5910.2 ms, ballot 81, acceptors ["node-1","node-2","node-5"] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run paxos-kv crash-catchup
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
2. Wait up to 20 seconds for `{"node":"*","path":"applied","op":"==","value":1}`.
3. Crash **node-5** using the node fault control.
4. On **node-1**, run `Application.Put` with `{"key":"x","value":"2"}`.
5. On **node-1**, run `Application.Put` with `{"key":"x","value":"3"}`.
6. On **node-1**, run `Application.Put` with `{"key":"x","value":"4"}`.
7. Recover **node-5** using the node fault control.
8. Wait 2 seconds while the real nodes continue running.
9. On **node-1**, run `Application.Put` with `{"key":"y","value":"5"}`.
10. Wait up to 30 seconds for `{"node":"node-5","path":"applied","op":"==","value":5}`.
11. Verify `{"node":"node-5","path":"values.x","op":"==","value":"4"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
