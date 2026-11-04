# Bully crash: automatic failure detection

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

After node-5 was established as coordinator, it crashed. Heartbeat timeouts [missed responses to periodic health checks] triggered elections without an application command. Node-4, the highest surviving ID, eventually announced itself and all online nodes agreed. Failure detection adds delay because silence alone is not immediate proof of a crash.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `32bf8da4-c2a4-4e67-95ef-494d7ce6c478`; recorded `2026-10-29T09:20:22.043Z`.
- [Platform replay](http://localhost:3000/#experiment/d6614aad-cd28-4f77-8e99-6f42dd8354c6/history).
- Node count: 5; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 399; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | last reported coordinator node-4; role follower; won 0; sent counter 8 |
| node-2 | last reported coordinator node-4; role follower; won 0; sent counter 8 |
| node-3 | last reported coordinator node-4; role follower; won 0; sent counter 8 |
| node-4 | last reported coordinator node-4; role coordinator; won 1; sent counter 13 |
| node-5 | last reported coordinator node-5; role coordinator; won 2; sent counter 29 |
| Whole-run algorithm counts | {"Election":21,"Coordinator":28,"OK":17} |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run bully_election crash-auto
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
2. Crash **node-5** using the node fault control.
3. Wait up to 25 seconds for `{"node":"*","path":"coordinator","op":"==","value":"node-4"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
