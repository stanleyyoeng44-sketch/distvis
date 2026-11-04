# Berkeley partition: partial round

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Blocking the coordinator's link to node-3 made its poll time out, while the coordinator and node-2 still formed a two-clock average. The missing sample was marked unsuccessful rather than treated as a zero offset. After healing and allowing the first slew to finish, a new round successfully scheduled node-3's adjustment. This shows partial progress, not an atomic guarantee [an all-or-nothing update] across all clocks.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `d204d997-f7f9-4b39-a9a6-35d034f76373`; recorded `2026-10-29T09:17:24.938Z`.
- [Platform replay](http://localhost:3000/#experiment/59546d31-a9ee-4408-a87c-a3b62a533cdd/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 94; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | round 2; error 392.57 ms; last adjustment 374.73 ms; remaining 357.18 ms |
| node-2 | round 2; error 375.04 ms; last adjustment 375.38 ms; remaining 375.38 ms |
| node-3 | round 2; error 1499.88 ms; last adjustment -750.11 ms; remaining -750.11 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run berkeley partition
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

1. On **node-1**, run `Application.Auto` with `{"enabled":false}`.
2. Apply network fault `{"kind":"link","from":"node-1","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
3. On **node-1**, run `Application.Sync` with `{}`.
4. Verify `{"node":"node-1","path":"lastRound.samples.2.scheduled","op":"==","value":false}`.
5. Verify `{"node":"node-1","path":"lastRound.samples.1.scheduled","op":"==","value":true}`.
6. Restore all links. This does not recover crashed processes or replay dropped messages.
7. Wait 5 seconds while the real nodes continue running.
8. On **node-1**, run `Application.Sync` with `{}`.
9. Verify `{"node":"node-1","path":"lastRound.samples.2.scheduled","op":"==","value":true}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
