# Berkeley crash: drift while down, converge after recovery

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Automatic rounds first brought clocks with different drift rates into close agreement. Crashing node-1 stopped new rounds, and the two live participants separated by more than 150 ms during twelve seconds without synchronization. Recovering the same coordinator restored its saved identity and round sequence; automatic rounds resumed and the group spread fell below 120 ms. This satisfies the additional recovery requirement in the Chinese assignment without introducing an election [choosing a different coordinator].

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `cf505e6d-b045-4137-b0d3-9eb920fa8e54`; recorded `2026-10-29T09:16:49.410Z`.
- [Platform replay](http://localhost:3000/#experiment/cb8607b9-45c3-49a8-a4f3-d075a5e33002/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 229; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | round 4; error 486.97 ms; last adjustment -0.75 ms; remaining 0.00 ms |
| node-2 | round 4; error 501.02 ms; last adjustment -59.66 ms; remaining -5.81 ms |
| node-3 | round 4; error 478.34 ms; last adjustment 60.41 ms; remaining 2.06 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run berkeley crash-master
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
2. Wait 1 seconds while the real nodes continue running.
3. On **node-1**, run `Application.SetClock` with `{"offsetMS":750,"driftPPM":0}`.
4. On **node-2**, run `Application.SetClock` with `{"offsetMS":750,"driftPPM":10000}`.
5. On **node-3**, run `Application.SetClock` with `{"offsetMS":750,"driftPPM":-10000}`.
6. On **node-1**, run `Application.Auto` with `{"enabled":true}`.
7. Wait up to 30 seconds for `{"node":"*","path":"lastAppliedRound","op":">=","value":2}`.
8. Wait up to 30 seconds for `{"spread":"errorMs","op":"<","value":100}`.
9. Crash **node-1** using the node fault control.
10. Wait 12 seconds while the real nodes continue running.
11. Verify `{"spread":"errorMs","nodes":["node-2","node-3"],"op":">","value":150}`.
12. Recover **node-1** using the node fault control.
13. Wait up to 45 seconds for `{"node":"*","path":"lastAppliedRound","op":">=","value":4}`.
14. Wait up to 45 seconds for `{"spread":"errorMs","op":"<","value":120}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
