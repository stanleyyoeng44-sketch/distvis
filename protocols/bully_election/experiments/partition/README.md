# Bully partition: two component coordinators

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Partitioning {1,2} from {3,4,5} produced separate coordinators node-2 and node-5. Bully cannot distinguish a partition from failure and therefore does not guarantee one globally unique coordinator under partitions. After healing, a new election caused the highest coordinator to reannounce to every member and restored agreement on node-5. An earlier audit run exposed a missing reannouncement to the other component; this retained passing rerun verifies the correction.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `28dfcc7f-45ae-41d9-9b4b-1e3b9367e402`; recorded `2026-10-29T09:20:57.105Z`.
- [Platform replay](http://localhost:3000/#experiment/18192f2b-26f3-4888-88a1-3047c934e7b5/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 618; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | last reported coordinator node-5; role follower; won 0; sent counter 12 |
| node-2 | last reported coordinator node-5; role follower; won 1; sent counter 17 |
| node-3 | last reported coordinator node-5; role follower; won 0; sent counter 9 |
| node-4 | last reported coordinator node-5; role follower; won 0; sent counter 9 |
| node-5 | last reported coordinator node-5; role coordinator; won 2; sent counter 50 |
| Whole-run algorithm counts | {"Election":27,"Coordinator":49,"OK":21} |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run bully_election partition
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
2. Apply network fault `{"kind":"link","from":"node-1","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
3. Apply network fault `{"kind":"link","from":"node-1","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
4. Apply network fault `{"kind":"link","from":"node-1","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
5. Apply network fault `{"kind":"link","from":"node-2","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
6. Apply network fault `{"kind":"link","from":"node-2","to":"node-4","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
7. Apply network fault `{"kind":"link","from":"node-2","to":"node-5","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
8. Wait up to 25 seconds for `{"node":"node-2","path":"coordinator","op":"==","value":"node-2"}`.
9. Verify `{"node":"node-5","path":"coordinator","op":"==","value":"node-5"}`.
10. Restore all links. This does not recover crashed processes or replay dropped messages.
11. On **node-1**, run `Application.StartElection` with `{}`.
12. Wait up to 20 seconds for `{"node":"*","path":"coordinator","op":"==","value":"node-5"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
