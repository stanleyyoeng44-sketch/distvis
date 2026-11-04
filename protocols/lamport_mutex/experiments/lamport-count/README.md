# Lamport D - six messages per acquisition

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

One completed acquisition used two Request, two Ack, and two Release calls with three nodes. The sum is six algorithm messages, matching 3(n−1). The requester left its critical section and all queues eventually emptied. Empty RPC responses are transport acknowledgements and are not counted as additional algorithm messages.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `d7be61a4-6a7f-4ff3-8777-1a8108bc1729`; recorded `2026-10-29T09:24:12.793Z`.
- [Platform replay](http://localhost:3000/#experiment/eed790f5-9748-4777-a539-9f6a808d093d/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 68; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 1; completed 1; wait 172.2 ms; algorithm messages 4; role idle |
| node-2 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| node-3 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| Full history | maximum simultaneous critical nodes = 1 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run lamport_mutex lamport-count
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

1. On **node-1**, run `Application.Acquire` with `{"holdMs":2000}`.
2. Wait up to 20 seconds for `{"node":"node-1","path":"completed","value":1}`.
3. Wait up to 20 seconds for `{"node":"*","path":"queue","value":[]}`.
4. Verify `{"messages":{"method":"MutexService.Request","op":"==","value":2}}`.
5. Verify `{"messages":{"method":"MutexService.Ack","op":"==","value":2}}`.
6. Verify `{"messages":{"method":"MutexService.Release","op":"==","value":2}}`.
7. Verify `{"messages":{"op":"==","value":6}}`.
8. Verify `{"atMostOne":{"path":"role","value":"critical"}}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
