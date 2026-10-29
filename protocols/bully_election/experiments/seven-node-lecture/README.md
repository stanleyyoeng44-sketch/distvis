# Bully: seven-node lecture trace

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

This run used exactly seven nodes, correcting the older report's eight-node substitution. After node-7 crashed, node-4 explicitly started an election and contacted higher-priority members. Node-6 became the highest surviving coordinator. The manual trigger reproduces the lecture's initiator while automatic failure detection remains part of the implementation.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `3ccf62be-75fb-4d1d-803b-7a7778762de5`; recorded `2026-10-29T09:20:34.585Z`.
- [Platform replay](http://localhost:3000/#experiment/a50475cf-3c70-45a8-922d-0227f3d6dd7c/history).
- Node count: 7; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 593; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | last reported coordinator node-6; role follower; won 0; sent counter 6 |
| node-2 | last reported coordinator node-6; role follower; won 0; sent counter 6 |
| node-3 | last reported coordinator node-6; role follower; won 0; sent counter 6 |
| node-4 | last reported coordinator node-6; role follower; won 0; sent counter 9 |
| node-5 | last reported coordinator node-6; role follower; won 0; sent counter 9 |
| node-6 | last reported coordinator node-6; role coordinator; won 1; sent counter 15 |
| node-7 | last reported coordinator node-7; role coordinator; won 2; sent counter 48 |
| Whole-run algorithm counts | {"Election":27,"Coordinator":48,"OK":24} |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run bully_election seven-node-lecture
```

For a manual run, select this protocol, create a fresh Docker experiment with the settings below, wait for every input form, then follow the numbered steps. Pause/replay in the browser only changes what you see; the real processes continue running.

```json
{
  "runtime": "docker",
  "nodeCount": 7,
  "latency": 80,
  "bandwidth": 128,
  "seed": 42
}
```

1. Wait up to 20 seconds for `{"node":"*","path":"coordinator","op":"==","value":"node-7"}`.
2. Crash **node-7** using the node fault control.
3. On **node-4**, run `Application.StartElection` with `{}`.
4. Wait up to 25 seconds for `{"node":"*","path":"coordinator","op":"==","value":"node-6"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
