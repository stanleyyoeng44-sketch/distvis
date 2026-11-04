# Cristian C · server queuing delay (1000 ms)

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The server waited 1000 ms before sampling its clock, so the round-trip time included the queueing delay. Adding half of that entire interval overestimated the short return path and produced approximately +500 ms of error. Removing the server delay restored a result close to zero. This demonstrates why a four-timestamp protocol such as NTP separately measures server processing time; it does not imply NTP can eliminate arbitrary path asymmetry.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `d05330b4-91bc-440a-8add-6930c6dca9ae`; recorded `2026-10-29T09:34:42.021Z`.
- [Platform replay](http://localhost:3000/#experiment/ee2926ba-5ba8-43b0-b4a0-1cd91dfaa087/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 42; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 25 node-2 mode=half | RTT 1168.8 ms; correction -251.4 ms; error after 500.5 ms |
| seq 41 node-2 mode=half | RTT 169.4 ms; correction -500.8 ms; error after 0.4 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run christian d-server-delay
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

1. Wait up to 20 seconds for `{"node":"node-2","path":"errorMs","op":"abs>","value":500}`.
2. On **node-1**, run `Application.SetServerDelay` with `{"delayMs":1000}`.
3. Ts is read after 80 ms + 1000 ms queueing; only 80 ms of the 1160 ms RTT is really the reply. Expected error about +500 ms.
4. On **node-2**, run `Application.Sync` with `{"server":"node-1"}`.
5. Verify `{"node":"node-2","path":"lastSync.rttMs","op":">=","value":1100}` — RTT includes the 1000 ms queue.
6. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":">=","value":400}` — node-2 ends up about 500 ms ahead.
7. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":"<=","value":600}` — error is about delay/2.
8. On **node-1**, run `Application.SetServerDelay` with `{"delayMs":0}`.
9. On **node-2**, run `Application.Sync` with `{"server":"node-1"}`.
10. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":"abs<=","value":60}` — without queueing the error is back near 0.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
