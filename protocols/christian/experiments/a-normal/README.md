# Cristian A · normal sync, symmetric 80 ms links

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The clients began with visibly different offsets and synchronized through node-1 over symmetric links. Their measured error fell close to zero, then began growing again according to their configured drift [clock-rate error]. This matched the expected behavior: one synchronization corrects the offset but does not correct the clock's rate. The reference server deliberately starts near zero; the homework's statement that every initial error is nonzero is not appropriate for an accurate reference clock.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `72e42966-686f-4a53-a9a1-a847f622e100`; recorded `2026-10-29T09:34:05.958Z`.
- [Platform replay](http://localhost:3000/#experiment/dfbf3170-08b2-4269-b76d-d9bd6ebbcfa2/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 55; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 22 node-2 mode=half | RTT 166.8 ms; correction -749.8 ms; error after 0.7 ms |
| seq 32 node-3 mode=half | RTT 164.8 ms; correction 1202 ms; error after 0.1 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run christian a-normal
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

1. Every client starts with a visible clock error; node-1 is the accurate reference.
2. Wait up to 20 seconds for `{"node":"node-2","path":"errorMs","op":"abs>","value":500}`.
3. Verify `{"node":"node-3","path":"errorMs","op":"abs>","value":500}` — node-3 starts more than 500 ms off.
4. Verify `{"node":"node-1","path":"errorMs","op":"abs<=","value":5}` — node-1 (server) is the reference.
5. On **node-2**, run `Application.Sync` with `{"server":"node-1","mode":"half"}`.
6. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":"abs<=","value":60}` — after Sync node-2 is within 60 ms of real time.
7. Verify `{"node":"node-2","path":"lastSync.rttMs","op":">=","value":150}` — RTT is about 2 × 80 ms.
8. On **node-3**, run `Application.Sync` with `{}`.
9. Verify `{"node":"node-3","path":"lastSync.errorAfterMs","op":"abs<=","value":60}` — empty form defaults to node-1 / half and also works.
10. Drift: node-2 gains 1 ms/s, node-3 loses 2 ms/s, so the error slowly grows back.
11. Wait 10 seconds while the real nodes continue running.
12. On **node-3**, run `Application.Status` with `{}`.
13. On **node-2**, run `Application.Status` with `{}`.
14. Verify `{"node":"node-2","path":"syncs","value":1}` — one successful sync recorded.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
