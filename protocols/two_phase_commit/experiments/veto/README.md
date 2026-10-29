# 2PC: insufficient balance veto

[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)

## Observation

A transfer of 150 exceeded node-2's balance, so that participant voted NO. The coordinator chose abort even though the recipient could accept the credit. Any prepared recipient released its lock, and both balances remained 100. Unlike consensus over one proposed value, this transaction needs every resource manager's agreement to preserve its all-or-nothing effects.

## Recorded execution

- Runtime: **Docker** [real Go processes, not the built-in simulation].
- Run: `e2cc197c-d835-47ca-9517-b56706dbaaf4`; recorded `2026-10-29T09:21:30.287Z`.
- [Platform replay](http://localhost:3000/#experiment/bf09d417-ab4a-4393-8f94-43749233f775/history).
- Node count: 3; base delay: 80 ms; bandwidth: 128 KiB/s per directed link.
- Events: 54; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.

### Measured values

| Record | Measurement |
|---|---|
| node-1 | role TM; transaction T1; phase/state finished; decision abort; balance 0 |
| node-2 | role RM; transaction T1; phase/state aborted; decision ; balance 100 |
| node-3 | role RM; transaction T1; phase/state aborted; decision ; balance 100 |
| Participant balance sum | 200 |

These are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.

## Reproduce

From the repository root (start `npm start` in another terminal and ensure Docker is available):

```sh
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run two_phase_commit veto
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

1. On **node-1**, run `Application.Transfer` with `{"from":"node-2","to":"node-3","amount":150}`.
2. Wait up to 20 seconds for `{"node":"node-1","path":"phase","op":"==","value":"finished"}`.
3. Verify `{"node":"node-1","path":"decision","op":"==","value":"abort"}`.
4. Verify `{"node":"node-2","path":"balance","op":"==","value":100}`.
5. Verify `{"node":"node-3","path":"balance","op":"==","value":100}`.
6. Verify `{"node":"node-3","path":"state","op":"==","value":"aborted"}`.

## Interpretation and limits

The observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.
