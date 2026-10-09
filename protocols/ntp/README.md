# NTP: four timestamps, filtering and hierarchy

**Lecture reference:** lec3.pdf, §2.3. This implements the lecture's synchronization algorithm over DistVis RPC, **not a wire-compatible or RFC-complete NTP daemon**.

Read [the shared Go tutorial](../CLOCKS.md), then [main.go](main.go), [clock.go](clock.go), [protocol.go](protocol.go), [rpc.go](rpc.go), and [protocol_test.go](protocol_test.go). `go.mod` declares dependencies; `go.sum` contains generated integrity hashes explained in the shared guide. The helper files are kept local so this module can be imported independently.

## What four timestamps tell us

Using the lecture's zero-based naming:

- `T0`: client sends request.
- `T1`: server receives request.
- `T2`: server sends response after its work.
- `T3`: client receives response.

```text
delay  = (T3 - T0) - (T2 - T1)
offset = ((T1 - T0) + (T2 - T3)) / 2
```

**Positive offset means the server is ahead; add it to the client.**

Example: `T0=1000, T1=1350, T2=1550, T3=1300`. The server is +300 ms ahead, both paths take 50 ms, and server processing takes 200 ms. The formulas yield delay=100 and offset=300. Simply halving total elapsed time would mistakenly include the processing interval.

Multiple exchanges produce `(delay, offset)` pairs. The selected pair has the **smallest valid delay**, not the average offset. All samples in a round use the same unadjusted client clock; one correction is applied only after filtering. Failed samples remain visible but are excluded. A clock generation counter rejects server samples spanning a configuration/adjustment.

## A deliberately simple, loop-free hierarchy

- Node-1 is a chosen laboratory reference with stratum 1. It has no actual GPS or atomic-clock connection.
- Other nodes start at stratum 16 (unsynchronized) and must synchronize before serving time.
- A node may select only **earlier-ranked members** as upstreams. By default it selects its immediate predecessor.
- After success, its stratum becomes the selected server's stratum plus one.

Thus node-2 can become stratum 2 by using node-1, then node-3 can become stratum 3 by using node-2. Node-3 can instead use node-1 directly and become stratum 2. Trying node-3 → unsynchronized node-2 fails rather than quietly treating an arbitrary clock as a reference.

The lecture uses a simplified class hierarchy; real NTP uses strata and additional selection/loop-avoidance rules. Our fixed membership ranking is an understandable classroom substitute, **not** a production topology algorithm.

## Try it in DistVis

Use three nodes, Docker, around 80 ms latency and 128 KiB/s bandwidth. Initial offsets are `0`, `+750`, `+1500` ms, with zero drift.

### Actions

- **Application.Read**: refresh state; no fields.
- **Application.ConfigureClock**: `{"offsetMS":1500,"driftPPM":0}`. Offset is an absolute initial host offset (±60,000 ms), drift is frequency error (±10,000 ppm). Reconfiguration invalidates synchronization on non-reference nodes.
- **Application.Sync**: `{"servers":["node-1"],"samples":3,"processingMS":200}`.
  - `servers`: distinct earlier-ranked members. Empty means immediate predecessor.
  - `samples`: 1–8 per server; zero means 3. At most 16 total in a round.
  - `processingMS`: 0–500 ms of deliberate server work between T1 and T2.

### Walkthrough

1. On node-3, try Sync with `servers:["node-2"]`. It should fail because node-2 is not synchronized.
2. On node-2, Sync against node-1 with 3 samples and 200 ms processing.
3. Inspect every sample's four timestamps. `(T2-T1)` should reflect the work. Check that `delayMS` subtracts it.
4. Inspect `last.selected`, a **zero-based array index** into `last.samples`; its delay must be the smallest among valid samples.
5. Sync node-3 against node-2. Inspect strata 1 → 2 → 3 and the selected upstream.
6. On node-3, try `servers:["node-1","node-2"]` with 3 samples each. The minimum-delay valid pair wins; it need not have the smallest stratum.
7. Add network jitter and repeat. The individual samples vary; filtering chooses one rather than falsely claiming a perfectly stable network.
8. Block one candidate upstream while leaving another reachable. Its samples should report errors; the other server can still supply a usable estimate.

Samples are issued in a bounded concurrent burst so a failed server does not cost one timeout per sample. That creates observable queuing and keeps the command within DistVis's application deadline. Real NTP normally polls across time and maintains richer filters; this lesson deliberately does not implement that scheduler.

## State

`stratum`, `upstream`, and `eligibleServers` show the hierarchy. `clock` is an observation snapshot. `last.samples` retains timestamps, errors and formulas. `selected=-1` means no valid sample. `lastError` describes a failed round. A failed round leaves clock parameters unchanged; ordinary passage of time/drift continues.

A prior successful stratum is retained if a later round fails; it represents previous synchronization, **not a fresh health certificate**. This simplified lesson has no aging/dispersion/holdover-expiry algorithm. Reconfigure or start a new run when you need a fresh unsynchronized node.

## Boundaries and lessons

- No NTP packet format, UDP port 123, authentication, leap-second handling, falseticker consensus, production reference selection, root dispersion or oscillator discipline loop.
- No external UTC is queried. The host-offset field is a laboratory diagnostic and never enters a formula.
- Four timestamps remove measured server work, not arbitrary asymmetric network delay. Bias under asymmetry remains `(forwardDelay - reverseDelay)/2`.
- Frequency differences and old samples add error; this is a short-round approximation, not an accuracy guarantee.
- Adjustments step the software clock and may move it backwards. Berkeley demonstrates gradual adjustment instead.
- Physical-clock state resets on process restart; synchronization is manual, not periodic.

## Tests

```sh
go -C protocols/ntp test -race -count=1 ./...
go -C protocols/ntp vet ./...
```

Tests cover exact four-timestamp calculations, subtraction of server work, asymmetric bias, malformed/negative-delay rejection, filtering, a real net/rpc three-level hierarchy, invalidated synchronization and concurrent server clock changes. The acceptance script adds real Docker evidence and a partial-upstream failure.
