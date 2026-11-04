# Cristian: synchronizing to one time server

**Lecture reference:** lec3.pdf, §2.1. Start with [the shared Go tutorial](../CLOCKS.md) for explanations of every file type, dependency and core Go construct.

## Read the code in this order

1. [main.go](main.go): metadata, `package main`, imports and the Runtime callback.
2. [clock.go](clock.go): software wall time, skew versus drift, validation and stepping.
3. [protocol.go](protocol.go): exported RPC types, the pure `estimate` function, service state and actions.
4. [rpc.go](rpc.go): generics, asynchronous net/rpc, channels, timers and timeout ownership.
5. [protocol_test.go](protocol_test.go): exact numerical examples, asymmetric delay and concurrency tests.
6. [go.mod](go.mod), [go.sum](go.sum): dependency declaration/checksums; see the shared guide before reading these generated hashes.

## Algorithm

The first member (`node-1`) is the laboratory reference. Each other node is a client:

1. Client records a monotonic start instant and requests the server's time.
2. Server records its **software** time when its handler receives the request and replies immediately.
3. Client measures elapsed round-trip time `RTT` and reads its software clock at response receipt.
4. Estimated server time at receipt is `serverMS + RTT/2`.
5. Correction is `estimatedMS - beforeMS`, added to the client's current software clock.

Adding the correction now, rather than setting an old absolute timestamp, preserves time elapsed between response receipt and adjustment.

If a true lower bound `minDelayMS` on each one-way delay is known, the lecture's idealized uncertainty interval is:

```text
[serverMS + minDelayMS, serverMS + RTT - minDelayMS]
midpoint error bound = RTT/2 - minDelayMS
```

The code rejects `2*minDelayMS > RTT`. Use **0** if you do not know a justified lower bound. This is not an unconditional precision certificate: it assumes a correct reference, approximately unit-rate clocks over the exchange, valid delay bounds, and negligible server work. Artificial drift, reference error and real scheduling/serialization can add error. A symmetric path makes the midpoint a good estimate; an asymmetric path can bias it.

## Try it in DistVis

Create a **three-node Docker** experiment, initially around **80 ms latency**, **128 KiB/s** bandwidth. The nodes start with offsets `0`, `+750`, `+1500` ms and zero drift. Wait for input forms to appear.

Actions use the exact JSON field names below; DistVis derives forms from the request structs:

- **Application.Read**, any node: no fields. Refresh clock state.
- **Application.ConfigureClock**, any node: `offsetMS` and `driftPPM`. This resets the software clock to the host clock plus that offset; it is not a relative correction. Example: `{"offsetMS":1500,"driftPPM":1000}`. Offset is limited to ±60,000 ms; drift to ±10,000 ppm.
- **Application.Sync**, a client: `{"minDelayMS":0}`. It always uses node-1, showing the single-server dependency. Running it on node-1 is rejected.

Suggested sequence:

1. Read node-2 and notice its roughly +750 ms diagnostic host offset.
2. Run Sync on node-2.
3. Inspect the request/reply arrows and `last`: raw server timestamp, RTT, before/estimated time, correction and uncertainty interval.
4. Read node-2 again. Its offset should be much closer to the reference, not necessarily exactly zero.
5. Configure node-2 with `offsetMS: 1000, driftPPM: 1000`, synchronize, wait a few seconds, then Read. Positive drift gradually recreates skew.
6. Block the link to node-1 and run Sync. The command should fail after the bounded wait without adjusting the clock. Heal the link and retry explicitly.
7. Give the forward/reverse links different latencies and compare estimated correction with the diagnostic host offset. This demonstrates why `RTT/2` is an assumption about one-way delay.

## State and concurrency

`clock` is a point-in-time view. `last` is the most recent **successful** sample. On a failed round, `last` remains the previous successful sample while `lastError` describes the new failure. The clock continues advancing; failure does not freeze time.

A `busy` guard prevents overlapping Sync and ConfigureClock on the client. Its state mutex is not held over peer RPC, so Read remains available. Network failure does not apply zero-valued reply fields as timestamps. The RPC helper returns an independent zero reply on timeout, avoiding a race with a late decoder.

## Boundaries

- One fixed server; no failover, redundancy, trust verification or external UTC source.
- The textbook adjustment is a **step** and may move software time backwards. Compare Berkeley's gradual correction for monotonic behavior.
- A server clock changed by external configuration during a measurement is outside the stable-reference assumption.
- Server processing time is not separately measured/subtracted. That is the motivation for NTP's extra timestamps.
- No periodic synchronization or persistence. Restarting a physical-clock node resets its software clock; start a fresh experiment for a clean comparison.
- A reported successful input submission is not proof of completion. Check the corresponding `command_result` and its error/result.

## Local tests

```sh
go -C protocols/cristian test -race -count=1 ./...
go -C protocols/cristian vet ./...
```

Tests verify the midpoint, interval endpoints, invalid inputs, asymmetric-delay error, drift/step behavior, a real net/rpc error path and concurrent reads/configurations. The separate acceptance script checks real Docker traffic and faults.
