# Berkeley: agree with the group, not with UTC

**Lecture reference:** lec3.pdf, §2.2. This lesson includes the lecture's important requirement that clock correction be **gradual**, not a backwards jump.

Read [the shared Go tutorial](../CLOCKS.md), then [main.go](main.go), [clock.go](clock.go), [protocol.go](protocol.go), [rpc.go](rpc.go) and [protocol_test.go](protocol_test.go). The shared guide also explains `go.mod`, generated `go.sum` and the indirect SDK dependencies. All code is ordinary Go; there is no shared custom algorithm framework to navigate.

## One synchronization round

1. The first member, node-1, is the fixed coordinator. It is **not** an external time authority.
2. It polls other clocks concurrently. Each poll estimates `peerTime + RTT/2` at response receipt.
3. Subtracting the coordinator's clock at that same instant gives an estimated **offset**. The coordinator adds its own offset, zero, as one participant.
4. Find the median offset; discard values farther than the configured threshold from it; average the survivors.
5. For every successful poll, send `delta = meanOffset - peerOffset`. The coordinator receives `meanOffset - 0` itself. Even an excluded outlier can receive a correction; it is excluded from voting on the mean, not abandoned.
6. Each clock schedules the delta by changing its rate temporarily. Acknowledgement means **scheduled**, not finished.

Why offsets instead of timestamps? Poll replies can arrive at different times. Averaging raw wall timestamps from different instants confuses elapsed time with clock skew. Offsets compensate for the measurement instant, approximately, assuming small drift and roughly symmetric paths.

### Worked arithmetic

Offsets `0, 100, 200, 10000`, threshold 300 ms:

- Median: 150 ms.
- Retained offsets: 0, 100, 200.
- Mean: 100 ms.
- Corrections: +100, 0, -100, -9900 ms.

The median-distance rule is an explicit choice because the lecture does not prescribe the exact outlier detector. It assumes a useful cluster of healthy clocks; it is not Byzantine agreement. If fewer than two stable, successful clocks remain, the algorithm refuses to adjust any clock.

## How gradual adjustment works

At a scheduled correction's start, preserve the current software clock value. As real time elapses, apply at most **0.10 ms of correction per elapsed millisecond**. Stop changing the rate once the full requested correction has been paid off; do not overshoot.

- A +200 ms correction takes about two seconds.
- A -200 ms correction also takes about two seconds, by slowing the clock.
- Drift is limited to ±1%; the slowest adjusted rate is `1 - 0.01 - 0.10 = 0.89 > 0`. Therefore the algorithm cannot make time run backwards.

The 10% rate is deliberately exaggerated for a short classroom demonstration. Production systems usually slew much more slowly. There is no timer that repeatedly mutates a counter: `clock.read(now)` calculates accumulated slew from elapsed monotonic time. **Read** refreshes the UI snapshot while the mathematical clock advances continuously.

## Try it in DistVis

Three nodes, Docker, approximately 80 ms latency and 128 KiB/s bandwidth work well. Default offsets are `0`, `+750`, `+1500` ms, so the first round may need several seconds to finish slewing.

### Actions

- **Application.Read**, any node: no fields.
- **Application.ConfigureClock**, any node: `{"offsetMS":200,"driftPPM":0}`. This external disturbance resets its software clock relative to the host and can intentionally jump time/cancel a slew. That is **not** what the synchronization algorithm does. Limits: ±60,000 ms offset, ±10,000 ppm drift.
- **Application.Sync**, **node-1 only**: `{"outlierThresholdMS":2000}`. Zero uses the 2000 ms default; explicit positive thresholds up to 120,000 ms are supported.

### Short experiment

1. Configure nodes 1/2/3 with offsets 0/100/200 ms and zero drift.
2. Run Sync on node-1 with threshold 1000 ms.
3. Inspect `lastRound.samples`: measured offset/RTT, `included`, `deltaMS`, `scheduled` and any `error`.
4. Read each node immediately: inspect `clock.pendingMS`. It should approach zero on later reads while `clock.timeMS` increases.
5. After a few seconds, read them again. All should be near the group's original +100 ms offset, not forced to UTC/host zero. Network error prevents exact agreement.
6. Configure offsets 0/100/10000 ms and repeat with threshold 500 ms. Node-3 should not contribute to the mean but should receive a large negative correction. Its large correction takes correspondingly longer to finish.
7. Block a participant link and run a new round once existing slews finish. Responsive clocks can still form a mean if at least two remain; the unavailable clock is listed with an error and is not silently treated as offset zero.

Use a fresh run or ConfigureClock to prepare a new exercise while a very large outlier correction is still slewing. Ordinary Sync/Poll rejects clocks with unfinished corrections to avoid overlapping or replacing adjustments unnoticed.

## Concurrency and failure semantics

- The coordinator has a busy guard; polling and adjustment phases run without holding its state mutex across RPC.
- Every poll contains a **boot instance** and a **generation**. ConfigureClock or an accepted adjustment changes the generation. An adjustment based on an old generation or pre-restart instance is rejected.
- Adjustments carry an increasing round number. Duplicate/older rounds cannot add the same delta twice.
- A node remembers the coordinator boot identity it accepted. A later coordinator boot is refused rather than silently resetting round ordering. `crypto/rand.Text()` supplies an identity, not time or authentication.
- RPC timeout means **outcome unknown**, not guaranteed non-execution. No automatic correction retry is attempted. `scheduled=false` with an error means no confirmation, not proof that nothing happened.
- Some nodes can accept while others fail. The returned round is a per-node report; a successful command is not an atomic promise that every clock converged. Inspect the individual fields.

## Deliberate limits

The coordinator is selected by fixed membership order. There is **no coordinator election/failover protocol**. If it crashes/restarts, use a **fresh experiment run** to reset round state consistently; do not interpret node recovery as election. Participant physical-clock state is volatile and resets on restart.

No external UTC source, periodic scheduler, precise one-way-delay measurement, durable transaction, Byzantine fault tolerance or oscillator-frequency estimation is claimed. Clock drift can recreate skew after the scheduled adjustment completes. Repeat rounds manually after prior slews finish.

## Tests

```sh
go -C protocols/berkeley test -race -count=1 ./...
go -C protocols/berkeley vet ./...
```

Tests verify the median/mean/delta signs, outlier correction, insufficient-sample refusal, positive/negative slew continuity and completion, bounds, stale/duplicate adjustment rejection, coordinator restart refusal, and actual net/rpc polling/scheduling. The acceptance script verifies gradual convergence and outlier handling in Docker.
