# RA random load: exponential FIFO

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Five nodes generated repeated requests while every message received exponential delay jitter [a random additional delay with the configured mean]. With FIFO delivery [each directed link preserves send order], all requested operations completed. The full history check found no simultaneous holders. Random delay changes waiting times but does not authorize a node to ignore missing permissions.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `91cb04d2-b120-4a87-831a-483216079c73`; recorded `2026-10-29T09:25:25.346Z`.
- [Platform replay](http://localhost:3000/#experiment/7020254c-c5b7-4ced-bcc6-f3aa2129d869/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 722; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 2; completed 2; wait 4555.2 ms; algorithm messages 16; role idle |
| node-2 | entries 2; completed 2; wait 4252.6 ms; algorithm messages 16; role idle |
| node-3 | entries 2; completed 2; wait 4467.6 ms; algorithm messages 16; role idle |
| node-4 | entries 2; completed 2; wait 4486.4 ms; algorithm messages 16; role idle |
| node-5 | entries 2; completed 2; wait 5060.5 ms; algorithm messages 16; role idle |
| Full history | maximum simultaneous critical nodes = 1 |
| Actual delivery-order reversals | 0 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run ricart-agrawala jitter-fifo
```

For a manual run, select this protocol, create a fresh Docker experiment with the settings below, wait for every input form, then follow the numbered steps. Pause/replay in the browser only changes what you see; the real processes continue running.

```json
{
  "runtime": "docker",
  "nodeCount": 5,
  "latency": 80,
  "bandwidth": 128,
  "seed": 42,
  "delayModel": "exponential",
  "jitter": 80
}
```

1. Use simultaneous input for **node-1**: `Application.Auto({"count":2})`; **node-2**: `Application.Auto({"count":2})`; **node-3**: `Application.Auto({"count":2})`; **node-4**: `Application.Auto({"count":2})`; **node-5**: `Application.Auto({"count":2})`.
2. Wait up to 60 seconds for `{"node":"*","path":"completed","op":"==","value":2}`.
3. Verify `{"atMostOne":{"path":"role","value":"critical"}}` — No overlapping critical sections in reported history.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
