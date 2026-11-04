# Cristian fault A · time server crashes

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Crashing node-1 made the client's time request fail, and the client recorded a failed synchronization without applying a correction. After node-1 recovered, a new manually submitted synchronization succeeded. This matched the expected single-server limitation [synchronization depends on that one server being available]. Recovery did not replay the old request automatically.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `ffd3c8ce-e252-49da-9e34-7b99afec69f1`; recorded `2026-10-29T09:34:19.708Z`.
- [Platform replay](http://localhost:3000/#experiment/1c420899-2f60-4c0e-afe0-82e7ecced46b/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 49; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| seq 47 node-2 mode=half | RTT 165.6 ms; correction -761.6 ms; error after 1 ms |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run christian b-crash
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
2. Crash **node-1** using the node fault control.
3. The only time server is gone; the request is dropped and Sync waits for the 10 s RPC timeout.
4. On **node-2**, run `Application.Sync` with `{"server":"node-1"}`. Expect an error [the required failure observation], not successful completion.
5. Wait up to 15 seconds for `{"node":"node-2","path":"failedSyncs","value":1}`.
6. Verify `{"node":"node-2","path":"errorMs","op":"abs>","value":500}` — clock was NOT changed by the failed sync.
7. Verify `{"node":"node-2","path":"lastError","op":"includes","value":"clock unchanged"}` — lastError explains the failure.
8. Recover **node-1** using the node fault control.
9. Wait up to 30 seconds for `{"node":"node-1","path":"role","value":"server"}`.
10. Wait 1.5 seconds while the real nodes continue running.
11. On **node-2**, run `Application.Sync` with `{"server":"node-1"}`.
12. Verify `{"node":"node-2","path":"lastSync.errorAfterMs","op":"abs<=","value":60}` — after recovery a retried sync works.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
