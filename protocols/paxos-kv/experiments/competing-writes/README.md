# Paxos KV: simultaneous slot-one proposals

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Two nodes captured slot 1 and proposed different writes concurrently. One write won slot 1; the other proposer learned that value and retried its own operation in slot 2. Both keys were present afterward and at least one conflicts counter increased. The independent checker found no slot with conflicting decisions in the safe run.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `57857500-d1d0-4c66-911d-cd42c2aacfb4`; recorded `2026-10-29T09:26:49.408Z`.
- [Platform replay](http://localhost:3000/#experiment/88d0b236-2ce6-4401-8845-ff06ef617a94/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 320; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | applied 2; values {"a":"1","b":"2"}; conflicts 1 |
| node-2 | applied 2; values {"a":"1","b":"2"}; conflicts 0 |
| node-3 | applied 2; values {"a":"1","b":"2"}; conflicts 0 |
| node-4 | applied 2; values {"a":"1","b":"2"}; conflicts 0 |
| node-5 | applied 2; values {"a":"1","b":"2"}; conflicts 0 |
| Chosen-slot check | conflicting slots: [] |
| Slot 1: observed majority acceptance | seq 95, 1631.2 ms, ballot 18, acceptors ["node-2","node-3","node-5"] |
| Slot 2: observed majority acceptance | seq 275, 2301.2 ms, ballot 49, acceptors ["node-1","node-4","node-5"] |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run paxos-kv competing-writes
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

1. Use simultaneous input for **node-1**: `Application.Put({"key":"a","value":"1","pauseMs":500})`; **node-2**: `Application.Put({"key":"b","value":"2","pauseMs":500})`.
2. Wait up to 20 seconds for `{"node":"*","path":"applied","op":"==","value":2}`.
3. Verify `{"node":"*","path":"values.a","op":"==","value":"1"}`.
4. Verify `{"node":"*","path":"values.b","op":"==","value":"2"}`.
5. Verify `{"node":"any","path":"conflicts","op":">=","value":1}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
