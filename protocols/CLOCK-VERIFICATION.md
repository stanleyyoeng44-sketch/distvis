# Clock lessons: verification results

Verified on **2026-10-09**, using the scope of lec3.pdf (*Lecture 5 Notes: Time and Order*). All authored files are under `protocols`; existing protocol directories and platform implementation files were not modified.

## Result

- All four independent Go modules compile and run in real Docker nodes.
- `go test -race -count=1 ./...` and `go vet ./...` passed in every module.
- A further `go test -race -count=10 ./...` passed in every module.
- **14 end-to-end acceptance checks passed**, including deliberate failure cases.
- Every submitted application command was matched to its own `command_result`; expected failures were checked as failures, not treated as successful synchronization.
- All four acceptance runs ended with status **completed**. The final catalog showed no active run.

The [full JSON evidence](clock-verification.json) contains command IDs, event sequence numbers, returned results and measured timestamps. The [commented acceptance script](verify-clocks.mjs) explains and reproduces the API flow. The script uses Node.js built-ins only; it does not implement the algorithms or simulate their network.

## Open a lesson

These normal, visible experiments are ready for your own use. They are separate from the archived acceptance experiments so you can explore without confusing your history with the test run.

| Lesson | Local tutorial | Imported protocol | Your experiment |
|---|---|---|---|
| Cristian | [README](cristian/README.md) | [Code](http://127.0.0.1:3000/#protocol/774468f3-c992-40e2-81c0-af062e747dab/code) | [Three-node lesson](http://127.0.0.1:3000/#experiment/91bdcafb-0fbd-4585-b746-f090467d94a1/visual) |
| NTP | [README](ntp/README.md) | [Code](http://127.0.0.1:3000/#protocol/069c20f9-7cb2-47d4-bee3-37557bd306b9/code) | [Hierarchy lesson](http://127.0.0.1:3000/#experiment/cb9c34f3-b16d-44a1-ba7e-32bdc231f5e2/visual) |
| Berkeley | [README](berkeley/README.md) | [Code](http://127.0.0.1:3000/#protocol/df602570-ece1-420e-acbf-8a9eb6070af1/code) | [Group lesson](http://127.0.0.1:3000/#experiment/0b8566c7-46a5-4885-83cf-71cb1c6d96e9/visual) |
| Lamport + vector | [README](logical-clocks/README.md) | [Code](http://127.0.0.1:3000/#protocol/306689ba-e07c-422a-b997-7202f487341a/code) | [Six-event lesson](http://127.0.0.1:3000/#experiment/24082b78-6274-4a7f-98ac-fe5121114a45/visual) |

All imported protocol snapshots used **revision 1**. Local edits do not automatically update those snapshots: synchronize the same protocol with its current revision before running changed code. Do not create a duplicate protocol just to test an edit.

## Preserved run IDs and observed behavior

Acceptance runs used **3 Docker nodes**, **40 ms fixed link latency**, **1024 KiB/s bandwidth**, and seed 42, with deliberate link overrides during fault checks. Measured values below describe this one execution, not guaranteed synchronization accuracy.

### Cristian

Run ID: `8e42ba28-23ca-4f33-bcfb-4bceb9666c55` · [Acceptance history](http://127.0.0.1:3000/#experiment/32df081f-3ff0-423e-a388-67df97e34ba3/history)

1. Verified midpoint, correction and uncertainty formulas. A client configured +1200 ms ahead ended approximately **+1.26 ms** from the host reference after synchronization.
2. Blocked the reference link: Sync returned an error, released its busy guard and left the configured +600 ms offset unchanged apart from sub-millisecond clock reading differences.
3. Used a much slower return path: residual offset was approximately **−149.20 ms**, demonstrating that RTT/2 cannot discover one-way asymmetry.

### NTP

Run ID: `126f287d-43d1-48c6-a157-906807e9ea12` · [Acceptance history](http://127.0.0.1:3000/#experiment/324de1bc-b968-441c-badc-8a3a0cfd7e52/history)

1. Rejected an unsynchronized upstream without changing the client's clock or stratum.
2. Bracketed 150 ms of server work with T1/T2, checked both formulas for every sample and selected the valid minimum-delay sample. The first client ended approximately **−1.50 ms** from the host reference.
3. Propagated synchronization through strata **1 → 2 → 3**.
4. With one upstream blocked, excluded its failed samples and successfully selected the reachable reference instead.

### Berkeley

Run ID: `1d0f567c-32be-4593-b37b-c5f97902f12a` · [Acceptance history](http://127.0.0.1:3000/#experiment/151cfd67-803a-48a1-8301-16cbe8e787bc/history)

1. From offsets 0/120/240 ms, checked every relative delta and observed gradual completion. Final offsets were approximately **119.74, 120.79, 120.63 ms**: agreement near the group's original mean, not forced host zero. The negatively corrected clock advanced rather than going backwards.
2. With one participant unreachable, used the two responsive clocks and recorded the missing participant's failure instead of inventing a reading.
3. With a +10,000 ms outlier, excluded it from the mean but sent it a large negative **gradual** correction. Verified that the correction remained pending rather than stepping the clock. Full completion of that roughly 100-second outlier slew was not waited for; positive/negative completion is covered by exact unit tests and the smaller real-run slew.

### Lamport + vector clocks

Run ID: `daaa96e0-6c49-403a-a8f7-9f510373396d` · [Acceptance history](http://127.0.0.1:3000/#experiment/414e1627-4790-4921-ac49-3c00ffbeebdd/history)

1. Submitted a/e concurrently and reproduced **all six** lecture Lamport/vector/total-order timestamps exactly.
2. Verified happens-before, concurrency and equality; Read/Compare and transport acknowledgements did not add modeled events.
3. Crashed/recovered node-2, restored its clock, and produced the next event with Lamport 5, vector [2,3,0], and a new event ID.
4. Blocked only the acknowledgement path: the receiver durably recorded the event, while the sender returned `confirmed=false`. This verifies the distinction between an unknown delivery outcome and proven non-delivery.

## What this does not establish

- Real execution was verified with three nodes on Docker, not every 2–12-node topology or Kubernetes.
- No production NTP conformance, public UTC accuracy, authentication or clock-discipline implementation is claimed.
- Berkeley has a fixed coordinator; election and coordinator failover are deliberately outside scope.
- Physical clock state is volatile. Logical counter recovery is implemented and tested, but this is not an exactly-once delivery protocol.
- Extreme delays beyond the lessons' three-second peer wait are reported as unconfirmed/failed operations; there is no automatic retry.
- Exact unit-test formulas are deterministic; real scheduling, quantized network delivery and drift are not. A passing run is evidence of the tested behaviors, not a proof of all distributed-system executions.
