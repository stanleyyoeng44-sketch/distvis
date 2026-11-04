# 2PC partition: notification retries after heal

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

After the coordinator saved commit, its link to node-2 was blocked. Node-3 committed while node-2 remained uncertain, and the coordinator stayed decided rather than falsely declaring completion. Healing let repeated notifications and status checks complete the remaining participant. The final total was conserved, but the intermediate states show why two-phase commit can block during communication failure.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `ada80ffa-c96a-464f-94c6-a80a7bbc8c58`; recorded `2026-10-29T09:21:55.736Z`.
- [Platform replay](http://localhost:3000/#experiment/f4a70159-3348-444e-96a0-626b927d93cc/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 80; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | role TM; transaction T1; phase/state finished; decision commit; balance 0 |
| node-2 | role RM; transaction T1; phase/state committed; decision ; balance 70 |
| node-3 | role RM; transaction T1; phase/state committed; decision ; balance 130 |
| Participant balance sum | 200 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run two_phase_commit partition
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

1. On **node-1**, run `Application.Transfer` with `{"from":"node-2","to":"node-3","amount":30,"pauseAfterDecisionMs":2000}`.
2. Wait up to 20 seconds for `{"node":"node-1","path":"decision","op":"==","value":"commit"}`.
3. Apply network fault `{"kind":"link","from":"node-1","to":"node-2","blocked":true,"bidirectional":true}`. Omitted link delay/bandwidth use this experiment's defaults.
4. Wait up to 20 seconds for `{"node":"node-3","path":"state","op":"==","value":"committed"}`.
5. Wait up to 20 seconds for `{"node":"node-2","path":"state","op":"==","value":"uncertain"}`.
6. Verify `{"node":"node-1","path":"phase","op":"==","value":"decided"}`.
7. Restore all links. This does not recover crashed processes or replay dropped messages.
8. Wait up to 20 seconds for `{"node":"node-1","path":"phase","op":"==","value":"finished"}`.
9. Verify `{"node":"node-2","path":"balance","op":"==","value":70}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
