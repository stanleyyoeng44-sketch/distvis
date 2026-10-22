# Berkeley clock synchronization, step by step

**Lecture reference:** lec3.pdf, §2.2 — including the requirement that clock
correction be **gradual**, not a backwards jump.

This version of the Berkeley algorithm is meant to be *operated*, not just
read. A round is split into the three steps the algorithm actually performs,
and **you trigger each one yourself** as a separate action in the DistVis UI.
Nothing happens behind your back: you see the raw measurements before any
number is computed, and you see the computed corrections before any clock is
touched.

Compare it with [`../berkeley`](../berkeley), which runs the same algorithm as
one automatic `Sync` action.

## The idea in one paragraph

There is no UTC. The group of clocks **agrees with each other**: the
coordinator collects everyone's readings, the group decides on a common
average, and every clock moves gradually toward it. The coordinator is just
node-1 — a normal participant whose vote happens to be "offset zero" because
the others are measured against it. It is **not** a time authority.

## The three steps of one round

| Step | Action (on node-1) | Changes clocks? | What to look at |
|---|---|---|---|
| 1 | `Application.PollPeers` | no | `samples[]`: each node's `timeMS`, `rttMS`, `offsetMS` |
| 2 | `Application.ComputeDeltas` | no | `medianMS`, `meanMS`, `included`, `deltaMS` |
| 3 | `Application.SendAdjustments` | **yes, gradually** | `scheduled` per node, then `clock.pendingMS` |

Two more actions work on **any** node at any time:

- `Application.Read` — report the node's state. Watch `clock.pendingMS` drain
  toward zero while `clock.timeMS` keeps rising: that is a correction being
  paid off.
- `Application.ConfigureClock` — an **external disturbance** for experiments
  (`{"offsetMS":200,"driftPPM":0}`). It rebuilds the clock, which may jump time
  and cancel a pending slew. That is *not* what the algorithm does; the
  algorithm only ever calls the gradual `clock.slew`. Limits: ±60,000 ms
  offset, ±10,000 ppm drift.

The coordinator's reported state contains `phase`
(`idle → polled → computed → sent`) and `nextAction`, so the UI tells you which
step to run next. Steps 1 and 2 are read-only and can be repeated freely; only
step 3 commits. `PollPeers` always starts a fresh worksheet — a round that
never reached step 3 simply disappears, and there is nothing to undo.

### Worked arithmetic

Offsets `0, 100, 200, 10000` ms (node-1 is the 0), threshold 300 ms:

- Median: 150 ms.
- Retained offsets: 0, 100, 200 → mean: 100 ms.
- Corrections: `+100, 0, −100, −9900` ms (`delta = mean − own offset`).

Note what happened to the broken clock: it is **excluded from voting** on the
mean but still **receives the biggest correction**. Being an outlier means
losing your vote, not being abandoned. If fewer than two responsive, stable
clocks remain, step 2 refuses and no clock is touched.

### Why offsets, not timestamps?

Poll replies arrive at different times, so averaging raw wall timestamps would
confuse elapsed time with clock skew. Each reading is converted to an **offset**
against the coordinator's clock at the estimated instant the peer read its
clock (`peerTime + RTT/2 − coordinatorTimeAtReceipt`). Same trick as Cristian,
same assumption: roughly symmetric paths and quick handlers.

### How gradual adjustment works

At the moment a correction is scheduled, the clock value is preserved. As real
time passes, at most **0.10 ms of correction is paid off per elapsed
millisecond** (rate 0.10), never overshooting. A +200 ms correction takes about
two seconds; a −200 ms one takes about two seconds of *slowing down* — the
clock never runs backwards (slowest possible rate: `1 − 0.01 − 0.10 = 0.89 > 0`).
The rate is deliberately exaggerated for a classroom demo. There is no ticking
timer: `clock.read(now)` calculates the accumulated slew from monotonic elapsed
time, so the clock advances continuously between reads.

## Reading order

1. [`main.go`](main.go) — wiring: two services (`TimeService` = node-to-node
   RPCs, `Application` = the actions you trigger).
2. [`clock.go`](clock.go) — the software clock and its gradual slew.
3. [`protocol.go`](protocol.go) — messages, node state, and the node-side
   `Poll`/`Adjust` RPCs with all guards explained.
4. [`round.go`](round.go) — the three steps you trigger, plus the
   median/mean/delta math.
5. [`protocol_test.go`](protocol_test.go) — executable versions of every claim
   above.

## The guards, and what breaks without them

All state changes on a node go through `TimeService.Adjust`, whose guards are
numbered in the source:

| Guard | Without it |
|---|---|
| 1: known coordinator, sane round/delta | anyone could set any clock to anything |
| 2: coordinator boot identity | a restarted coordinator would silently replay old rounds |
| 3: round number moves forward | one duplicate message applies the same correction twice |
| 4: instance + generation match step 1 | a correction computed from an old measurement is applied to a new clock |
| 5: no overlapping slews | scheduling a new correction silently discards the unpaid part of the old one |

Step 4 is easy to *see*: run step 1, then `ConfigureClock` on node-3, then run
step 3. node-3's correction is rejected ("clock changed since poll"), the other
nodes go through. This is also why a round is honest about partial success —
step 3 is per-node, not atomic broadcast; inspect each sample's `scheduled`
and `error` fields instead of trusting the command alone.

A timeout does **not** cancel a net/rpc call: "no reply within 3 s" means
*outcome unknown*, not "nothing happened". Failed adjustments are never
retried automatically, because a relative correction is not idempotent.

## Try it in DistVis

Three nodes, Docker, ~80 ms latency, 128 KiB/s bandwidth. The nodes start at
offsets `0`, `+750`, `+1500` ms so the first round has something to fix.

### Short experiment

1. `ConfigureClock` on node-1/2/3 with `offsetMS` 0/100/200 and `driftPPM` 0.
2. **Step 1** — `PollPeers` on node-1. Check `samples[]`: offsets ≈ 0/100/200,
   `rttMS` ≈ twice the link latency.
3. **Step 2** — `ComputeDeltas` with `outlierThresholdMS` 1000. `medianMS` and
   `meanMS` are both 100; verify `deltaMS` = `meanMS − offsetMS` by hand.
4. **Step 3** — `SendAdjustments`. Every sample gets `scheduled: true`.
5. `Read` each node immediately: `clock.pendingMS` ≈ 100 and falling. Read them
   again after ~2 s: all three clocks now agree near **+100 ms** — the group's
   average, not UTC/host zero.

### Outlier experiment

6. `ConfigureClock` on node-3 with `offsetMS` 10000. Run all three steps with
   `outlierThresholdMS` 500. Node-3 has `included: false` but gets the largest
   negative `deltaMS` (≈ −9900 ms). Its slew takes correspondingly longer —
   or reset it with `ConfigureClock`, which also demonstrates that an external
   disturbance can jump time and cancel a slew (the algorithm never does).

### Fault experiment

7. Block the node-1 ↔ node-2 link in the faults panel
   (`{"kind":"link","from":"node-1","to":"node-2","latency":80,"bandwidth":128,"blocked":true,"bidirectional":true}`),
   then run step 1. Node-2's sample carries an `error` and is never treated as
   offset zero. Steps 2–3 proceed with the remaining two clocks. `heal`
   afterwards.

### What to watch while a round is in flight

- Immediately after step 3, another `PollPeers` is refused ("still slewing"):
  a mid-slew clock is a moving target. Wait, or prepare a fresh experiment
  with `ConfigureClock`.
- Step 2/3 out of order are refused with a message naming the missing step.
- `nextAction` in the reported state always shows what to do next.

## Limits

- The coordinator is **node-1 by membership order**. There is **no election or
  failover protocol**: if node-1 crashes, start a fresh run. Participant clock
  state is volatile and resets on restart (the boot identity exists so peers
  *notice* a restart, not to survive one).
- No external UTC source, no periodic scheduler, no precise one-way-delay
  measurement, no durable state, no Byzantine agreement. The outlier rule
  (median distance) assumes a healthy majority; it is not a fault-tolerant
  consensus.
- Drift recreates skew after a correction finishes; repeat rounds manually
  once the slews are done.

## Tests

```sh
go -C protocols/berkeley_new test -race -count=1 ./...
go -C protocols/berkeley_new vet ./...
```

Covered: the median/mean/delta arithmetic (including the outlier's big
correction and the refusal cases), step ordering, slew continuity in both
directions and its completion, all five `Adjust` guards, and a full three-step
round over real net/rpc with per-node recording and the "wait for slewing"
refusal.
