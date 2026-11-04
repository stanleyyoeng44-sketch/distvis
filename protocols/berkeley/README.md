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
- **Application.Auto**, node-1: `{"enabled":false}` disables automatic rounds; true enables them. A round already running finishes.
- **Application.SetMode**, any node: `{"step":true}` selects the deliberately unsafe immediate-jump comparison; false restores smooth correction. Configure every node consistently before a comparison round.
- **Application.SetClock** is an alias for ConfigureClock with the same fields.

The state includes `role`, `lastRound`, per-sample `included`/`scheduled`/`error`, `lastAppliedRound`, `lastError`, and the clock readings. `errorMs` aliases the observer's host-relative offset, `lastAdjMs` is the last scheduled correction, and `slewMsLeft` is the remaining smooth adjustment. `stepMode` and `auto` expose the selected experiment controls.
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
- A node remembers the coordinator identity it accepted. The coordinator saves this identity and reserves every round number on disk before sending adjustments. Restarting the same coordinator restores both; a delayed instruction still carries an older round or an outdated clock generation and is rejected. `crypto/rand.Text()` supplies an identity, not time or authentication; loss of the coordinator's durable file is not ordinary recovery.
- RPC timeout means **outcome unknown**, not guaranteed non-execution. No automatic correction retry is attempted. `scheduled=false` with an error means no confirmation, not proof that nothing happened.
- Some nodes can accept while others fail. The returned round is a per-node report; a successful command is not an atomic promise that every clock converged. Inspect the individual fields.

## Deliberate limits

The coordinator is selected by fixed membership order. There is **no coordinator election/failover protocol** [no other node replaces node-1]. Recovering node-1 in the same run resumes periodic rounds using its durable identity and increasing round number, as required by the [Chinese assignment](../../docs/LAB1.md#33-berkeley-算法). Software-clock offsets remain volatile [reset on process restart]; the returning coordinator is an ordinary clock sample, not an external reference. Losing the coordinator's saved identity requires a fresh experiment rather than silently accepting a different coordinator.

No external UTC source, precise one-way-delay measurement, durable transaction, Byzantine fault tolerance [agreement despite malicious participants] or oscillator-frequency estimation is claimed. Clock drift can recreate skew after the scheduled adjustment completes. The coordinator now attempts periodic rounds approximately every three seconds when no round or local slew is active. All nodes publish clock state once per second; `Application.Auto(enabled=false)` disables rounds for controlled manual experiments without disabling state reports.

## Discussion

**Why average offsets rather than synchronize to the coordinator?** The coordinator is an ordinary member with no guaranteed external time accuracy. Averaging successful offsets gives the participating group a common target. The normal experiment converges near +750 ms, not zero.

**What does outlier rejection protect?** An outlier [a reading far from the central cluster] must not drag the target away from the healthy clocks. The median-distance rule excludes its vote but can still correct that node. With offsets 0/750/10000 ms, the retained mean is approximately 375 ms, and the outlier's −9625 ms slew needs about 96 seconds at 100 ms per second.

**Why not jump immediately?** A negative jump can produce a later clock reading smaller than an earlier one, confusing log order and expiry calculations. Smooth correction slows a fast clock while keeping its rate positive. The jump variant is explicitly unsafe, even though it reaches a similar final target faster.

**What happens when the coordinator crashes?** No remaining node is authorized to compute and send a new round, so clocks with different drift rates separate again. Recovering the same node restores its durable round sequence and resumes synchronization without electing a replacement. The crash experiment leaves automatic rounds enabled, measures drift while node-1 is down, then verifies renewed convergence after recovery. Bully instead demonstrates choosing a different coordinator while the old one is absent.

## Tests

```sh
go -C protocols/berkeley test -race -count=1 ./...
go -C protocols/berkeley vet ./...
```

Tests verify the median/mean/delta signs, outlier correction, insufficient-sample refusal, positive/negative slew continuity and completion, bounds, stale/duplicate adjustment rejection, unrelated-coordinator refusal, durable round reservation and recovery, storage-failure refusal, and actual net/rpc polling/scheduling. `clockBeforeAdjustmentMS` and `clockAfterAdjustmentMS` measure the same instant before and after correction: they are equal for a slew and can decrease for the deliberate jump variant. The acceptance script verifies gradual convergence and outlier handling in Docker.

## Verified experiment evidence

One folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.

- [Berkeley normal: periodic convergence](experiments/normal-periodic/README.md)
- [Berkeley: discard outlier vote](experiments/outlier/README.md)
- [Berkeley crash: drift while down, converge after recovery](experiments/crash-master/README.md)
- [Berkeley partition: partial round](experiments/partition/README.md)
- [Berkeley variant: jump versus slew](experiments/variant-jump/README.md)
