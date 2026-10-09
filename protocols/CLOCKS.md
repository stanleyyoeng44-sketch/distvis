# Time and order: four Go lessons

Written for someone learning **Go and distributed clocks together**. Read this guide first, then open a lesson's `README.md` and follow its suggested file order. The source comments deliberately explain syntax, standard-library APIs, DistVis APIs, and design choices rather than merely restating the code.

The scope follows **lec3.pdf**, whose internal title is *Lecture 5 Notes: Time and Order*: Cristian (§2.1), Berkeley (§2.2), NTP (§2.3), and Lamport/vector clocks (§3). The implementations use real Go processes and ordinary `net/rpc`, not the platform's built-in simulation models.

## Choose a lesson

- [Cristian](cristian/README.md): one reference server, one measured RTT, a midpoint estimate and uncertainty interval.
- [NTP](ntp/README.md): four timestamps, subtraction of server processing time, lowest-delay filtering and a simple hierarchy.
- [Berkeley](berkeley/README.md): a coordinator, median-based rejection of outliers, averaging, relative corrections and gradual clock adjustment.
- [Lamport + vector clocks](logical-clocks/README.md): timestamp the **same events** with both clocks; reproduce the lecture example and compare causality with concurrency.

The three physical-clock examples **never change your operating system clock**. They change a software clock stored inside each process. Host-clock offset in reported state is a diagnostic for this laboratory, not information used by the algorithms. No public time server or GPS/UTC receiver is contacted.

## Read a Go module without getting lost

Each of the four directories is a separate **module**, Go's unit of dependency management. All executable source files in one directory declare `package main`; Go compiles them together. A function in `protocol.go` can call a function in `clock.go` directly because both are in the same package. There is no need to import sibling files.

Each module contains:

1. **`README.md`** — algorithm, assumptions, inputs, experiment walkthrough and boundaries.
2. **`main.go`** — DistVis metadata and the small executable entry point.
3. **`clock.go`** (physical lessons) or **`clocks.go`** (logical lesson) — the mathematical clock model, with no network calls.
4. **`protocol.go`** — RPC request/reply types, node state, lifecycle registration and application actions.
5. **`rpc.go`** — a small, explained timeout helper around the standard asynchronous `net/rpc.Client.Go` API.
6. **`protocol_test.go`** — worked numerical examples and service tests. Tests are executable explanations.
7. **`go.mod`** — module name, Go version, dependencies and a local `replace` directive.
8. **`go.sum`** — machine-maintained dependency integrity checksums. This format does **not** support tutorial comments; this paragraph explains every kind of line instead. A line contains a dependency module path, its version (possibly with `/go.mod`), and an `h1:` checksum. The `/go.mod` form checks that dependency's module manifest; the other form checks its source archive. Do not edit these hashes by hand.

`go mod tidy` adds `// indirect` entries for packages pulled in by the SDK. In these modules:

- `google.golang.org/grpc` implements gRPC support inside the shared SDK, even though our examples choose `net/rpc`.
- `google.golang.org/protobuf` provides protobuf encoding and descriptors used by that support.
- `google.golang.org/genproto/googleapis/rpc` provides generated standard RPC types.
- `golang.org/x/net`, `x/sys`, and `x/text` supply networking, OS interfaces and text support required by dependencies.

You do not call these modules in the lesson code. Learn the APIs you actually use in the annotated source, rather than trying to read all transitive dependencies first. Extra entries in `go.sum` may come from dependency tests, not from code we wrote.

The small `rpc.go` helper and the Cristian/NTP `clock.go` helper are intentionally repeated. An imported DistVis protocol is a snapshot of **one module**, so it must not require another lesson directory at runtime. This modest duplication keeps each example independently readable and runnable; there is no hidden shared teaching framework.

## A short Go reading guide

### Values, pointers and errors

```go
var reply TimeReply                      // A zero-initialized value.
result, err := callRPC[TimeReply](client, "TimeService.Time", Empty{})
if err != nil {
    return err                           // Explicit error propagation.
}
reply = result                           // Assignment, not declaration.
```

- `:=` declares and initializes local variables, inferring their types. `=` assigns to variables already declared.
- `*TimeReply` is a pointer type; `&reply` takes an address; `*reply = value` writes through a pointer. A receiver such as `(s *TimeService)` lets a method operate on the original service rather than a copied value.
- `nil` means no value for pointers/maps/slices/functions/interfaces. An `error` result equal to `nil` conventionally means success.
- `fmt.Errorf` constructs an error. `%s` formats a string, `%v` a value, and `%w` wraps another error while preserving its identity.
- Upper-case names are exported. RPC request/reply types, methods and transmitted struct fields must be exported so reflection/encoding can access them.
- Backtick **struct tags**, such as `json:"offsetMS"`, are metadata, not code executed by Go. DistVis reads them to derive application field names. `distvis:"node"` and `distvis:"nodes"` produce node selectors.

### Slices, maps and copies

- `[]string` is a slice: a length/capacity/pointer view of an underlying array. Assigning a slice does **not** copy its elements.
- `append([]uint64(nil), old...)` constructs an independent copy. The `...` expands a slice into individual arguments. This is essential when old vector timestamps must remain immutable.
- `map[string]*rpc.Client` looks up clients by member ID. A missing entry returns the element's zero value (`nil` here).
- `range` iterates a collection. Use `for i, value := range items` for index and value; use `for i := range items` when you need to change `items[i]` rather than a copied loop value.

### Goroutines and synchronization

`go f()` starts a goroutine. RPC handlers also run concurrently, even if you never explicitly write `go` in a handler. A `sync.Mutex` guards related shared fields. `Lock` and `Unlock` must surround **both reads and writes** when another goroutine can write.

`defer s.mu.Unlock()` schedules unlocking when the current function returns. Deferred calls run in last-in, first-out order. Do not copy a mutex after using it. Do not hold the state mutex while making a peer RPC: the peer may need to call you, and neither side should deadlock waiting for the other's lock.

A `sync.WaitGroup` waits for workers: `Add(1)` **before** launch, `defer Done()` inside each worker, then `Wait()`. NTP/Berkeley preallocate result slices and assign each worker a distinct element; they never append to one shared slice concurrently.

A channel transports values and synchronizes goroutines. `select` waits for whichever communication is ready. The RPC helper selects between `pending.Done` and a timer. It returns an independent zero reply on timeout because the late RPC decoder may still be writing the original reply.

### Time and floating point

`time.Time` is an instant; `time.Duration` is an elapsed interval. `time.Now()` includes a monotonic reading, and `end.Sub(start)` uses monotonic readings when both operands have them. That is safer for RTTs than subtracting adjustable wall timestamps. `Duration.Seconds()*1000` retains fractional milliseconds, while integer conversions can truncate them.

Software wall timestamps use `float64` **milliseconds**, not nanoseconds. Current Unix millisecond integers fit safely in JavaScript's exact integer range; Unix nanoseconds do not. Calculations retain useful fractional milliseconds but do not claim arbitrary precision. `math.IsNaN` and `math.IsInf` reject invalid numeric inputs.

The logical-clock lesson instead uses bounded `uint64` integer counters. It checks limits before incrementing and keeps the total-order value exact in the UI.

## DistVis APIs you are using

- `lab.Main(configure)` owns initialization, signals, the receive loop and shared connections. Pass the function, not `configure()`.
- `node.ID` identifies this node. `node.Nodes` is the fixed, ordered membership list, including itself.
- `node.RPCPeers()` returns ordinary clients for all **other** members. A client existing does not mean its peer is healthy. Never close these Runtime-owned clients in protocol code.
- `node.RegisterName("TimeService", service)` registers internal methods. Add `lab.Application()` only to the application service to generate user-facing forms.
- A standard RPC method looks like `func (s *Service) Method(args Args, reply *Reply) error`. Requests/replies travel through DistVis's network and appear in the run history.
- `node.Report(state)` publishes a complete state snapshot for inspection. It does not synchronize clocks, send peer messages, or persist recovery state.
- `sdk.Save(value)` / `sdk.Load(&value)` persist algorithm state across recovery **within the same run**. Only the logical lesson needs durable counters. Save/Load are serialized under its mutex. A new run starts a new state volume.

All examples are manually driven. Setup does not send messages, and no timer silently generates application events. An idle initial run is expected: use the metadata's suggested starting action. Physical clock values still advance mathematically; use **Read** to refresh the visible snapshot.

## Build and test locally

From the repository root:

```sh
for lesson in cristian ntp berkeley logical-clocks; do
  go -C "protocols/$lesson" mod tidy
  go -C "protocols/$lesson" test -race -count=1 ./...
  go -C "protocols/$lesson" vet ./...
done
```

`go test` compiles the application code plus `_test.go` files. `-race` instruments shared-memory accesses. `go vet` checks likely mistakes that are legal syntax. `./...` means this package and any subpackages **inside the current module**; running it at the repository root does not recursively test independent nested modules.

No binary needs to be checked in. `go run .` without a DistVis coordinator is not the interactive UI: the executable expects an initialization record on stdin. Use the platform's Docker runtime for the complete demonstration. The local `replace distvis => ../..` resolves the SDK; container builds inject their own SDK path automatically.

## End-to-end acceptance script

See [the readable verification results](CLOCK-VERIFICATION.md) for the actual checks, measured outcomes, ready-to-use experiment links and preserved run IDs.

[verify-clocks.mjs](verify-clocks.mjs) uses Node.js's built-in HTTP and assertion APIs. It uses already imported protocol IDs, creates/reuses archived acceptance experiments, runs one Docker experiment at a time, discovers live action names, checks each matching `command_result`, and stops only its own run. It exercises link faults and logical-node recovery too.

It writes [clock-verification.json](clock-verification.json): structured evidence, not source code. Its keys identify protocol/experiment/run IDs and record assertions/observations. Generated timestamps and measurements change on every real execution. Read the script's comments for the request flow and the report's results for what actually passed. DistVis itself retains immutable full histories in its ordinary platform storage; all authored lesson/test/report files are here under `protocols`.

## Scope cautions

- These are classroom algorithms, not secure/public NTP servers. Internal RPC inputs assume trusted lesson participants; declared node IDs are not authentication.
- The server-processing and network-symmetry assumptions remain visible. Filtering cannot infer an unknown one-way delay.
- Berkeley's fixed coordinator has no election or failover implementation. Its corrections are not an atomic group commit.
- Physical clocks reset on process restart and can drift again after synchronization. Start another round explicitly; none claims permanent perfect agreement.
- Lamport's `L(a) < L(b)` does **not** prove `a → b`. Ordering concurrent events by `N*L+i` gives an artificial total order, not a causal edge. This corrects misleading wording in the lecture's narrative around the example.
- Vector-clock causality applies to the **modeled data events** with fixed membership, not to observation commands, transport acknowledgements, or every action in the operating system.
