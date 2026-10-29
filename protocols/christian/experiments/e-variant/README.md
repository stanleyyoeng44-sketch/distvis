# Cristian design variant · +0 vs +RTT/2 vs +RTT (200 ms links)

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

The zero mode used the returned server time without accounting for its journey back and left the client behind by approximately one link delay. The full mode added both directions and left the client ahead by approximately one link delay. The half mode was close to zero with symmetric links, as expected. Its midpoint minimizes worst-case uncertainty only under the stated accurate-server and bounded-clock-rate assumptions.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `2189eaee-537c-4f0f-b033-cafed3b72af1`; recorded `2026-10-29T09:34:46.571Z`.
- [Platform replay](http://localhost:3000/#experiment/e0d1cfd7-98ab-4af4-b7d8-bb4a15b6dbb5/history).
- Node count: 3; base delay: 200 ms; bandwidth: 128 KiB/s per directed link.
- Events: 46; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 22 node-2 mode=zero | RTT 408.1 ms; correction -953.6 ms; error after -202.8 ms |
| seq 32 node-2 mode=full | RTT 405.8 ms; correction 404.5 ms; error after 202.5 ms |
| seq 45 node-2 mode=half | RTT 408.1 ms; correction -203.6 ms; error after -0.3 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run christian e-variant
```

For a manual run, select this protocol, create a fresh Docker experiment with the settings below, wait for every input form, then follow the numbered steps. Pause/replay in the browser only changes what you see; the real processes continue running.

```json
{
  "runtime": "docker",
  "nodeCount": 3,
  "latency": 200,
  "bandwidth": 128,
  "seed": 42
}
```

1. Wait up to 20 seconds for `{"node":"node-2","path":"errorMs","op":"abs>","value":500}`.
2. RTT is about 400 ms. zero ignores the reply delay, full counts the whole round trip, half is Cristian.
3. On **node-2**, run `Application.Sync` with `{"mode":"zero"}`.
4. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":"<=","value":-150}` — zero: about 200 ms BEHIND (missed the reply delay).
5. On **node-2**, run `Application.Sync` with `{"mode":"full"}`.
6. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":">=","value":150}` — full: about 200 ms AHEAD (also counted the request delay).
7. On **node-2**, run `Application.Sync` with `{"mode":"half"}`.
8. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":"abs<=","value":60}` — half (Cristian): about 0.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
