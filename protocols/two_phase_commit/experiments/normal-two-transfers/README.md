# 2PC normal: two transfers conserve 200

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

Two transfers of 30 completed from node-2 to node-3. The balances progressed from 100/100 to 70/130 and then 40/160, conserving a total of 200. For each transaction the coordinator collected every YES vote, saved commit, and obtained acknowledgements. Transfer returning successfully means accepted; phase=finished is the evidence of transaction completion.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `4a412695-0ccf-4eb3-9ac1-19fc25a52215`; recorded `2026-10-29T09:21:25.890Z`.
- [Platform replay](http://localhost:3000/#experiment/8a5d4b5c-4e4e-48ef-aec6-11106292249f/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 107; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | role TM; transaction T2; phase/state finished; decision commit; balance 0 |
| node-2 | role RM; transaction T2; phase/state committed; decision ; balance 40 |
| node-3 | role RM; transaction T2; phase/state committed; decision ; balance 160 |
| Participant balance sum | 200 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run two_phase_commit normal-two-transfers
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

1. On **node-1**, run `Application.Transfer` with `{"from":"node-2","to":"node-3","amount":30}`.
2. Wait up to 20 seconds for `{"node":"node-1","path":"phase","op":"==","value":"finished"}`.
3. Verify `{"node":"node-2","path":"balance","op":"==","value":70}`.
4. Verify `{"node":"node-3","path":"balance","op":"==","value":130}`.
5. On **node-1**, run `Application.Transfer` with `{"from":"node-2","to":"node-3","amount":30}`.
6. Wait up to 20 seconds for `{"node":"node-1","path":"phase","op":"==","value":"finished"}`.
7. Verify `{"node":"node-2","path":"balance","op":"==","value":40}`.
8. Verify `{"node":"node-3","path":"balance","op":"==","value":160}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
