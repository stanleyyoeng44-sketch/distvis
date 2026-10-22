# Berkeley clock synchronization — observation report

All four scenarios are automated: `node scripts/scenarios.mjs run berkeley_new`
(plus `node protocols/berkeley_new/monotonic.mjs` for the strict monotonicity
check). Each scenario is a real Docker run; export its JSON via
**Run history → Export record** for submission.

| Scenario | Run | Result |
|---|---|---|
| A convergence over rounds | `03b479c8-170b-4027-89e9-c7cbed333e7d` | ✓ pass |
| B outlier exclusion | `fd09fb4f-45f6-41e7-a1c9-186becfca643` | ✓ pass |
| C jump vs smooth | `921e533f-91ed-4f5d-ac6d-aa332968dc92` | ✓ pass |
| design variant: master crash | `6c1a0af4-c031-473a-9bf3-4e6df4a14c0e` | ✓ pass |
| monotonicity (companion) | `c225644b-8f9e-42d1-a6bc-0a9ee9348111` | ✓ pass |

## A. Normal scenario — convergence over repeated rounds

I configured the three software clocks at 0 / +100 / +200 ms and ran three
synchronization rounds (each round in this implementation is three steps I
trigger: `PollPeers` → `ComputeDeltas` → `SendAdjustments`). After the first
round the deltas were about +100 / 0 / −100 ms; after the third round every
node reported `hostOffsetMS` inside 88–112 ms with `pendingMS` = 0 — the
spread between the nodes was under a millisecond in the companion run.
Importantly, none of them moved toward real time: the group kept its own
~+100 ms offset from the host clock. This matches the expectation: Berkeley
makes the clocks **agree with each other**, and "small error" means a small
spread, not proximity to UTC.

## B. Fault scenario — an outlier loses its vote but keeps its correction

After converging, I set node-3's clock to +10 000 ms (`ConfigureClock`) and
ran a round with `outlierThresholdMS` = 500. The master's worksheet shows
`included: false` for node-3, while the mean stayed at ~0 ms — had the outlier's
vote counted, the mean would have been dragged to about +3 300 ms and the two
healthy clocks would have been "corrected" by over 3 seconds. The outlier still
received the largest correction (about −9 900 ms) and accepted it: exclusion
means losing your **vote**, not being abandoned. Node-3 then pays the huge
correction off gradually (`pendingMS` ≈ −9 900 and draining at the 10%/ms slew
rate, ~99 s in total).

## C. Jump vs. smooth adjustment (and why monotonic time matters)

With clocks at 0 / +300 / +600, the round assigns node-1 +300 ms and node-3
−300 ms. Tracking node-3 with `Read`: `pendingMS` went −238 → −117 → 0 over
about 3.4 s, and its `timeMS` **kept rising at every single sample** — a
slowing clock (rate ≥ 89% of real time) still moves forward. The companion
script `monotonic.mjs` asserts this strictly: 11 samples, every node's clock
strictly increasing, final spread 0.9 ms. Then, as the contrast, one
`ConfigureClock` call (the external disturbance, not the algorithm) moved
node-3 from ~+300 ms to +50 ms **in a single step** — its time went backwards
by ~250 ms instantly, with nothing left to "pay off".

**Discussion (what a jump breaks):** applications that assume time never runs
backwards break in concrete ways: log entries can appear out of order or get
negative durations ("event ended before it started"); cache entries with a
TTL can expire twice or live far too long; leases and lock timeouts can fire
prematurely and then not at all. The gradual slew avoids this by construction:
the clock's rate is bounded (worst case 1 − 0.01 − 0.10 = 89% of real time), so
timestamps keep increasing while the correction is applied.

## Design variant — a crashed master stops the rounds (no election)

I crashed the master (node-1) after the group had converged. A `PollPeers`
on node-2 is refused ("runs on the coordinator node-1") — no round can start,
and the participants simply keep their converged time (~+300 ms each). After
`recover`, node-1 **can** run rounds again (it is the master by membership
order, not by election), but both participants refuse its corrections with
"coordinator restarted; start a fresh experiment" — the group stays split
(participants ~+300 ms, restarted master ~+200 ms) until a fresh experiment.

**Discussion (contrast with Bully):** clock synchronization here has no
mechanism to choose a new master, so a dead coordinator stops synchronization
for the rest of the run — recovery does not help, because the restarted master
is a *different* coordinator identity. The Bully protocol solves exactly this
missing piece: it elects the highest-priority alive node automatically. A real
system combines the two (elect a master, then run Berkeley under it); that is
the boundary this lesson deliberately leaves out.

## Reproducing

```sh
node scripts/scenarios.mjs run berkeley_new        # scenarios A/B/C + design variant
node protocols/berkeley_new/monotonic.mjs        # strict time-never-backwards check
```

The steps of every scenario are in `scenarios.json`; the state fields are
documented in `README.md` (`clock.hostOffsetMS` ≈ the lab's `errorMs`,
`lastDeltaMS` ≈ `lastAdjMs`, `clock.pendingMS` ≈ `slewMsLeft`).
