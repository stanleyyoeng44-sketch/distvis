# Lamport C - blocked link and healing

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

A blocked direct link prevented the requester from receiving one required permission even though every process remained online. Healing restored connectivity, but the dropped request was not replayed and the acquisition remained waiting. This matched the implementation's lack of retries rather than indicating a failure of the heal operation. No node entered without every required permission.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `120ed65d-5de4-4f6a-be5c-a04390743634`; recorded `2026-10-29T09:24:01.792Z`.
- [Platform replay](http://localhost:3000/#experiment/53982495-8e2a-4044-b3d4-b87449cb4aaa/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 38; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

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
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run lamport_mutex partition
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

1. Apply network fault `{"kind":"link","from":"node-2","to":"node-3","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
2. On **node-2**, run `Application.Acquire` with `{"holdMs":2000}`.
3. Wait up to 20 seconds for `{"node":"node-2","path":"lastError","op":"includes","value":"node-3"}`.
4. Verify `{"node":"node-2","path":"entries","value":0}`.
5. Restore all links. This does not recover crashed processes or replay dropped messages.
6. Wait 4 seconds while the real nodes continue running.
7. Verify `{"all":[{"node":"node-2","path":"operation","value":"waiting"},{"node":"node-2","path":"missingAcks","value":["node-3"]},{"node":"node-2","path":"entries","value":0}]}`.
8. Verify `{"atMostOne":{"path":"role","value":"critical"}}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
