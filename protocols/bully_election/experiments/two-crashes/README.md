# Bully: two highest nodes crash

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The two highest nodes, node-7 and node-6, crashed in quick succession. The surviving nodes eventually elected node-5. The unreachable higher IDs caused timeout waits but did not prevent an election among the survivors. This is the requested two-failure case, not the older report's case with three total failed nodes.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `21f006b7-ced1-42e9-8302-6f9efef4ebc2`; recorded `2026-10-29T09:20:44.159Z`.
- [Platform replay](http://localhost:3000/#experiment/e1c0ea07-b08e-4deb-8cde-297d54168966/history).
- Node count: 7; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 628; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | last reported coordinator node-5; role follower; won 0; sent counter 12 |
| node-2 | last reported coordinator node-5; role follower; won 0; sent counter 12 |
| node-3 | last reported coordinator node-5; role follower; won 0; sent counter 12 |
| node-4 | last reported coordinator node-5; role follower; won 0; sent counter 12 |
| node-5 | last reported coordinator node-5; role coordinator; won 1; sent counter 18 |
| node-6 | last reported coordinator node-7; role follower; won 0; sent counter 6 |
| node-7 | last reported coordinator node-7; role coordinator; won 2; sent counter 42 |
| Whole-run algorithm counts | {"Election":41,"Coordinator":42,"OK":31} |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run bully_election two-crashes
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
3. Crash **node-6** using the node fault control.
4. Wait up to 25 seconds for `{"node":"*","path":"coordinator","op":"==","value":"node-5"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
