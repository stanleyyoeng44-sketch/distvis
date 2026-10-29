# Berkeley normal: periodic convergence

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Automatic rounds moved initial offsets of 0, 750, and 1500 ms toward a common offset near 750 ms. The nodes agreed with one another rather than moving toward UTC [the external reference time]. The first correction took several seconds because slewing [temporarily changing clock speed] applies at most 100 ms of correction per second. Later rounds maintained close agreement, matching the Berkeley algorithm.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `cc969303-3627-4672-b044-0981cd154246`; recorded `2026-10-29T09:16:18.427Z`.
- [Platform replay](http://localhost:3000/#experiment/78c91bef-d630-48b0-b005-506b6ed6e59c/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 228; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | round 5; error 750.01 ms; last adjustment 0.17 ms; remaining 0.00 ms |
| node-2 | round 5; error 749.44 ms; last adjustment -0.54 ms; remaining 0.00 ms |
| node-3 | round 5; error 750.26 ms; last adjustment 0.37 ms; remaining 0.00 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run berkeley normal-periodic
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

1. Wait up to 20 seconds for `{"node":"node-1","path":"lastAppliedRound","op":">=","value":2}`.
2. Wait 9 seconds while the real nodes continue running.
3. On **node-1**, run `Application.Read` with `{}`.
4. On **node-2**, run `Application.Read` with `{}`.
5. On **node-3**, run `Application.Read` with `{}`.
6. Verify `{"node":"node-1","path":"errorMs","op":"<=","value":850}`.
7. Verify `{"node":"node-2","path":"errorMs","op":"<=","value":850}`.
8. Verify `{"node":"node-3","path":"errorMs","op":"<=","value":850}`.
9. Verify `{"node":"node-1","path":"errorMs","op":">=","value":650}`.
10. Verify `{"node":"node-2","path":"errorMs","op":">=","value":650}`.
11. Verify `{"node":"node-3","path":"errorMs","op":">=","value":650}`.
12. Verify `{"spread":"errorMs","op":"<","value":40}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
