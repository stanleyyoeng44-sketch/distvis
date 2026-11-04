# Lamport B - idle peer crash

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

An uninvolved idle node crashed before another node requested entry. The requester collected the other live peer's permission but remained waiting for the crashed member, with entries=0. It continued waiting beyond the RPC timeout [the limit on waiting for one remote call]. The finite recording demonstrates blocking over the observation interval; indefinite blocking follows from the code's all-peer requirement and lack of retransmission.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `17e07f10-0dbf-45cf-bc02-0aee96a101ec`; recorded `2026-10-29T09:23:42.934Z`.
- [Platform replay](http://localhost:3000/#experiment/589942b6-9522-4c71-ad6d-7bf40f41ef77/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 40; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | entries 0; completed 0; wait 0.0 ms; algorithm messages 1; role idle |
| node-2 | entries 0; completed 0; wait 0.0 ms; algorithm messages 2; role waiting |
| node-3 | entries 0; completed 0; wait 0.0 ms; algorithm messages 0; role idle |
| Full history | maximum simultaneous critical nodes = 0 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run lamport_mutex crash
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

1. Crash **node-3** using the node fault control.
2. On **node-2**, run `Application.Acquire` with `{"holdMs":2000}`.
3. Wait up to 20 seconds for `{"node":"node-2","path":"lastError","op":"includes","value":"node-3"}`.
4. Wait 11 seconds while the real nodes continue running.
5. Verify `{"all":[{"node":"node-2","path":"operation","value":"waiting"},{"node":"node-2","path":"entries","value":0},{"node":"node-2","path":"missingAcks","value":["node-3"]}]}`.
6. Verify `{"atMostOne":{"path":"role","value":"critical"}}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
