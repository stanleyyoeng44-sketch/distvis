# Cristian clock synchronization

Lecture L03. Lab 1 §3.1.

## Algorithm

`node-1` is the time server. Its clock is accurate: offset 0, no drift. Every other node is a client.

When a client runs `Sync`:

1. It reads its own clock: **t0**. Then it sends `TimeService.Ask` to the server.
2. The server reads its clock and replies with **Ts**.
3. The client reads its own clock when the reply arrives: **t1**. The round-trip time is `RTT = t1 − t0`.
4. The server's time is now estimated as `Ts + RTT/2`. This assumes the reply took half of the round trip.
5. The client adds `correction = estimate − t1` to its clock at this instant (a *step*, i.e. a jump).

Under an accurate-server assumption and a client clock measuring elapsed time at the reference rate, the server samples during the round trip, so the time at receipt lies in `[Ts, Ts + RTT]`. The midpoint minimizes the worst-case network uncertainty to `RTT/2` (`errorBoundMs`). 

Code:

- the formula: [`estimate`](protocol.go)
- one sync: [`Application.Sync`](protocol.go)
- the server side: [`TimeService.Ask`](protocol.go)
- the simulated clock: [clock.go](clock.go)

### Simulated clocks

All containers share the host computer's clock. Each node therefore keeps its own **simulated** clock: `clock = real time + offset`, running at a speed of `1 + driftPPM/10⁶`. Initial values (from `initialClock` in [clock.go](clock.go)):

| node | offset | drift |
|---|---|---|
| node-1 (server) | 0 | 0 |
| node-2 | +750 ms | +1000 ppm (gains 1 ms/s) |
| node-3 | −1200 ms | −2000 ppm (loses 2 ms/s) |
| node-4 | +1650 ms | +3000 ppm |
| node-5 | −2100 ms | −1000 ppm |

Because the real time is known here, `errorMs = clock − real time` can be shown. The algorithm itself never uses `errorMs`; it exists only for observers.

## State fields (node panel)

Every node reports these after each action, and every 2 s so drift is visible.

| field | meaning |
|---|---|
| `role` | `server` (node-1) or `client` |
| `clock`, `clockMs` | this node's simulated clock (UTC text / Unix milliseconds) |
| `errorMs` | clock minus real time; positive = ahead |
| `driftPPM` | clock speed error |
| `serverDelayMs` | simulated queuing delay before this node answers `Ask` |
| `served` | `Ask` requests answered as a server |
| `syncs` / `failedSyncs` | successful / failed syncs as a client |
| `busy` | a sync is waiting for its reply |
| `lastSync` | the last **successful** sync, with every number in it (below) |
| `lastError` | why the last sync failed; `""` after a success |

`lastSync` contains:

- `t0Ms`, `tsMs`, `t1Ms`, `rttMs`
- `assumedOneWayMs`: what was added to Ts
- `estimateMs`, `correctionMs`
- `errorBoundMs`: network-uncertainty bound under an accurate server and a client measuring elapsed time at the reference rate; not an unconditional guarantee with drift or a non-reference server
- `errorBeforeMs` and `errorAfterMs`: the observer's view of the clock error before and after the sync

## Application inputs

| input | fields | effect |
|---|---|---|
| `Application.Sync` | `server` (node dropdown, empty = node-1), `mode`: `half` (default), `zero`, `full` | one Cristian round. `zero` uses Ts as-is; `full` uses Ts + RTT (design variant) |
| `Application.Status` | — | refresh and return the state |
| `Application.SetClock` | `offsetMs`, `driftPPM` | reset this clock to real time + offset with a new drift |
| `Application.SetServerDelay` | `delayMs` (0–8000) | this node, as a server, waits `delayMs` before reading Ts (scenario C) |

`Sync` is rejected when it is run on the chosen server itself, for an unknown server, or while another `Sync` is in progress on the same node. A failed `Sync` never changes the clock.

## Scenarios

Every scenario is automated in [scenarios.json](scenarios.json):

```bash
node scripts/scenarios.mjs run christian              # all five
node scripts/scenarios.mjs run christian c-asymmetric # just one
```

Current exports and measurements are in the **Verified experiment evidence** folders linked below. The following older `cristian_*.json` observations are historical examples from earlier Docker runs, not measurements of the newly rerun submission; use each current scenario README for its exact numbers.

To do a scenario by hand: create an experiment with the listed settings, run it, then click a node and use "Run on node-x".

### A. Normal case: symmetric links (`a-normal`, 80 ms) → `cristian_scenA_normal.json`

Steps: look at the initial `errorMs`, run `Sync` on node-2, then `Sync` on node-3 with an empty form, wait 10 s, then run `Status`.

Observed:

- **Before:** node-2 started at `errorMs = +750.8` and node-3 at `−1202.4`.
- **Sync results:** RTT was 169.8 ms (about 2 × 80 ms plus a few ms of processing). Corrections were −752.0 and +1201.4 ms. Afterwards the errors were −1.2 ms and −0.9 ms, well inside the `errorBoundMs` of 84.9.
- **Drift after 10 s:** the errors grew again to +9.8 ms (node-2) and −21.4 ms (node-3), matching their drift of +1 ms/s and −2 ms/s.

This matched expectations. One sync fixes the offset but not the drift, so in a real system clients must re-sync periodically.

### Fault A. The server crashes (`b-crash`) → `cristian_scenB_crash.json`

Steps: crash node-1, run `Sync` on node-2, recover node-1, run `Sync` again.

Observed:

- **During the crash:** the request was dropped ("Destination node offline"), and `Sync` failed after the 10 s RPC timeout with `context deadline exceeded`. `failedSyncs` became 1 and `lastError` says "clock unchanged". `errorMs` stayed at about +760: a failed sync applies no correction.
- **After recovery:** a manual retry worked (error +1.6 ms).

This matched expectations. There is only one server and no failover, so nobody can synchronize while it is down. The code does not retry by itself.

### Fault B. Asymmetric link (`c-asymmetric`) → `cristian_scenC_asymmetric.json`

Steps:

1. Set the link node-2→node-1 to 2000 ms and node-1→node-2 to 200 ms. Run `Sync`.
2. Reverse the two delays. Run `Sync`.
3. Heal the links. Run `Sync`.

Observed:

| links | RTT | assumed reply delay | real reply delay | errorAfterMs |
|---|---|---|---|---|
| request 2000, reply 200 | 2213.7 | 1106.9 | ≈200 | **+901.2** |
| request 200, reply 2000 | 2214.6 | 1107.3 | ≈2000 | **−899.5** |
| healed (200 / 200) | 411.5 | 205.8 | ≈200 | +1.1 |

The error equals `(request delay − reply delay) / 2` = ±900 ms, and it stays inside `errorBoundMs` (≈1107). The client cannot detect this: both asymmetric cases show the same RTT, but their errors have opposite signs. Measuring only the RTT gives no way to tell how it was split.

### Scenario C. Server queuing delay (`d-server-delay`) → `cristian_scenD_serverDelay.json`

Steps: run `SetServerDelay(1000)` on node-1, run `Sync` on node-2, set the delay back to 0, run `Sync` again.

Observed:

- **With the delay:** RTT was 1170.4 ms. Cristian assumed a reply delay of 585.2 ms, but the reply really took about 80 ms, because Ts is read *after* the queue. The result was an error of **+500.5 ms**, which is about `delay / 2`.
- **Without the delay:** the error went back to +0.2 ms.

The client cannot separate the server's waiting time from network time. NTP fixes exactly this: the server also reports when it received the request (t1) and when it replied (t2), so the processing time `t2 − t1` can be subtracted.

### Design variant: +0 vs +RTT/2 vs +RTT (`e-variant`, 200 ms) → `cristian_designVariant.json`

Steps: run `Sync` three times on node-2 with `mode` set to `zero`, then `full`, then `half`.

| mode | estimate | RTT | errorAfterMs |
|---|---|---|---|
| `zero` | Ts | 406.7 | **−202.4** (behind: the reply's travel time is lost) |
| `full` | Ts + RTT | 410.2 | **+206.0** (ahead: the request's travel time is counted too) |
| `half` | Ts + RTT/2 | 410.7 | **−0.9** |

`zero` and `full` are each off by one whole one-way delay. Their worst-case bound is the full RTT (≈410 ms), while `half` halves that bound to RTT/2. `RTT/2` is the only choice that is correct for symmetric links, and it minimizes the worst case when nothing is known about the asymmetry.

## Discussion questions

**How large is the error when the delays are not symmetric?** The error is `(d_request − d_reply) / 2`, where `d_` is each one-way delay. It is bounded by `±RTT/2`. With 2000/200 ms we measured +901 ms, against a prediction of +900. The client measures only the sum of the two delays, never how it is split, so it cannot correct for this.

**Why add RTT/2 rather than 0 or RTT?** Ts was read somewhere between t0 and t1, so the true time is in `[Ts, Ts + RTT]`. The midpoint has the smallest worst-case error (RTT/2). The ends (`zero`, `full`) can be off by a full RTT, and on symmetric links they are always off by one one-way delay (±202 ms in our run).

**What does a crash of the server do?** All clients lose the ability to synchronize, and their clocks drift freely. The server is a single point of failure. Real systems use several servers (NTP pools), or elect a new master as in Berkeley plus an election algorithm.

**Why is the step a problem?** In scenario A, node-2's correction was −752 ms, which moved its clock backwards. Software that assumes time only increases (log ordering, timeouts, cache expiry) can break. Berkeley avoids this by slewing: running the clock slightly faster or slower until the correction is absorbed.

## Boundaries (not implemented)

- No periodic synchronization; each `Sync` is started by hand. Drift therefore grows back between syncs.
- No server redundancy or failover, and no automatic retry.
- No filtering over multiple samples (for example, keeping the sample with the smallest RTT).
- No slewing: corrections are steps, and they can move the clock backwards.
- Clocks are not persisted. A crashed client restarts with its initial offset.
- The minimum one-way delay is assumed unknown (0), so the bound is the plain `RTT/2`.

## Verified experiment evidence

One folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.

- [Cristian A · normal sync, symmetric 80 ms links](experiments/a-normal/README.md)
- [Cristian fault A · time server crashes](experiments/b-crash/README.md)
- [Cristian fault B · asymmetric link (2000 ms up, 200 ms down)](experiments/c-asymmetric/README.md)
- [Cristian C · server queuing delay (1000 ms)](experiments/d-server-delay/README.md)
- [Cristian design variant · +0 vs +RTT/2 vs +RTT (200 ms links)](experiments/e-variant/README.md)
