# Cristian fault B · asymmetric link (2000 ms up, 200 ms down)

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

With a 2000 ms request path and a 200 ms reply path, the client moved approximately 900 ms ahead. Reversing the delays moved it approximately 900 ms behind, although the round-trip time was nearly unchanged. The result matches error = (request delay − reply delay)/2, with small additional error from scheduling and drift. Healing the links restored the symmetric-delay result.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `a43bfd73-1f63-4129-a4a2-3ba00d949681`; recorded `2026-10-29T09:34:34.268Z`.
- [Platform replay](http://localhost:3000/#experiment/e8c87d43-1caa-416b-b78f-695daca35056/history).
- Node count: 3; base delay: 200 ms; bandwidth: 128 KiB/s per directed link.
- Events: 57; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 27 node-2 mode=half | RTT 2210.4 ms; correction 148.1 ms; error after 900.8 ms |
| seq 42 node-2 mode=half | RTT 2209 ms; correction -1801.9 ms; error after -898.7 ms |
| seq 53 node-2 mode=half | RTT 407.8 ms; correction 898.9 ms; error after 0.7 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run christian c-asymmetric
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
2. Apply network fault `{"kind":"link","from":"node-2","to":"node-1","latency":2000}`. Omitted link delay/bandwidth use this experiment's defaults.
3. Apply network fault `{"kind":"link","from":"node-1","to":"node-2","latency":200}`. Omitted link delay/bandwidth use this experiment's defaults.
4. Request takes 2000 ms, reply 200 ms. Cristian assumes (2000+200)/2 = 1100 ms for the reply: expected error +900 ms.
5. On **node-2**, run `Application.Sync` with `{"server":"node-1"}`.
6. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":">=","value":800}` — node-2 ends up about 900 ms AHEAD.
7. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":"<=","value":1000}` — error stays inside the ±RTT/2 bound.
8. Apply network fault `{"kind":"link","from":"node-2","to":"node-1","latency":200}`. Omitted link delay/bandwidth use this experiment's defaults.
9. Apply network fault `{"kind":"link","from":"node-1","to":"node-2","latency":2000}`. Omitted link delay/bandwidth use this experiment's defaults.
10. Reversed: request 200 ms, reply 2000 ms. Expected error -900 ms.
11. On **node-2**, run `Application.Sync` with `{"server":"node-1"}`.
12. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":"<=","value":-800}` — node-2 ends up about 900 ms BEHIND.
13. Restore all links. This does not recover crashed processes or replay dropped messages.
14. On **node-2**, run `Application.Sync` with `{"server":"node-1"}`.
15. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":"abs<=","value":60}` — symmetric again: error back near 0.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
