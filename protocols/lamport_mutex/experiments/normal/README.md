# Lamport A - concurrent requests

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Node-1 and node-3 requested entry concurrently and completed one protected operation each. The lower timestamp-and-node-ID pair entered first; the other requester waited for permission and the queue-head condition. The replay contained no overlapping critical-section reports [no recorded instant with two holders]. The two acquisitions generated twelve algorithm messages in total, excluding the RPC library's empty transport replies.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `f26fa1e8-9a25-42e3-99ac-496dcae25790`; recorded `2026-10-29T09:23:34.416Z`.
- [Platform replay](http://localhost:3000/#experiment/9cea43a5-3320-4126-b7e3-8085c3fcd105/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 123; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 1; completed 1; wait 168.8 ms; algorithm messages 5; role idle |
| node-2 | entries 0; completed 0; wait 0.0 ms; algorithm messages 2; role idle |
| node-3 | entries 1; completed 1; wait 2257.7 ms; algorithm messages 5; role idle |
| Full history | maximum simultaneous critical nodes = 1 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run lamport_mutex normal
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

1. Use simultaneous input for **node-1**: `Application.Acquire({"holdMs":2000})`; **node-3**: `Application.Acquire({"holdMs":2000})`.
2. Wait up to 20 seconds for `{"node":["node-1","node-3"],"path":"completed","value":1}`.
3. Wait up to 20 seconds for `{"node":"*","path":"queue","value":[]}`.
4. Verify `{"node":"*","path":"role","value":"idle"}`.
5. Verify `{"atMostOne":{"path":"role","value":"critical"}}` — Critical sections never overlap.
6. Verify `{"messages":{"value":12,"op":"=="}}` — Two acquisitions use 12 algorithm messages.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
