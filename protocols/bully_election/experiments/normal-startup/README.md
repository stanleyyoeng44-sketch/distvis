# Bully normal: automatic five-node election

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Five nodes started their elections automatically, and node-5 became the coordinator. Lower-priority nodes received OK responses and accepted the highest node's Coordinator announcement. The trace therefore includes the Election, OK, and Coordinator steps without a manually triggered startup. Concurrent elections can produce quadratic message work [the number of messages can grow proportional to n²], although this particular run is not an asymptotic benchmark.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `1449649c-ab65-438d-8907-752b9219fb58`; recorded `2026-10-29T09:20:16.745Z`.
- [Platform replay](http://localhost:3000/#experiment/4e0e9826-6e1a-4c1a-b985-b3fb4839d8ce/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 239; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | last reported coordinator node-5; role follower; won 0; sent counter 4 |
| node-2 | last reported coordinator node-5; role follower; won 0; sent counter 4 |
| node-3 | last reported coordinator node-5; role follower; won 0; sent counter 4 |
| node-4 | last reported coordinator node-5; role follower; won 0; sent counter 4 |
| node-5 | last reported coordinator node-5; role coordinator; won 1; sent counter 24 |
| Whole-run algorithm counts | {"Election":10,"Coordinator":20,"OK":10} |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run bully_election normal-startup
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

1. Wait up to 20 seconds for `{"node":"*","path":"coordinator","op":"==","value":"node-5"}`.
2. Verify `{"node":"node-5","path":"role","op":"==","value":"coordinator"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
