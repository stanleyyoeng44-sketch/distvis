# DistVis

User and developer documentation: [Go API](docs/GO-API.md) · [Platform HTTP API](docs/PLATFORM-API.md) · [Agent / MCP / skill](docs/AGENT.md) · [Lab 1 Guide](docs/LAB1.md) ([English](docs/LAB1.en.md)). After starting the platform, open **API Documentation** from the sidebar (on visualization pages, the sidebar collapses into a navigation drawer; click ☰ in the upper-left corner to open it). You can also visit [/docs/go](http://localhost:3000/docs/go) and [/docs/platform](http://localhost:3000/docs/platform) directly. The documentation supports search, table-of-contents navigation, code copying, and Markdown downloads.

Documentation example checks: `node tests/docs-container-smoke.mjs` runs the downloaded Ping program as-is on an idle platform and automatically archives the recording; `node tests/browser-docs-smoke.mjs` checks documentation navigation, search, copying, and mobile layout (requires Playwright). `demo.mjs` in the Platform API documentation can execute the complete create, input, fault, and export workflow.

A local lab workbench for teaching distributed systems: implement protocols, run nodes, inject faults, and replay events step by step.

Implemented features include built-in teaching simulations, a Go SDK, Docker / Docker Desktop Kubernetes runtime adapters, topology message animations, node/link operation panels, code drafts, event search, historical replay, and JSON export. The first examples are **Raft leader election, token-ring mutual exclusion, and LWW replica storage**.

**Ordinary `net/rpc` and gRPC unary services are supported.** Application input forms are generated automatically from Go parameter types / protobuf definitions, and standard client calls automatically record requests, responses, and errors. Users can specify a local protocol root directory; each subdirectory is a Go module that the platform discovers, mounts, and runs automatically. See the [Go API](docs/GO-API.md) for integration instructions and runnable examples in [protocols/](protocols/).

Protocol entry points use `lab.Main(configureProtocol)` (`lab` is `distvis/sdk/rpc`). The shared library handles node initialization, message dispatch, connection reuse, and cleanup on exit. A protocol only needs to assemble services and state. Use `node.RPCPeers()` to obtain the clients for all other nodes in a batch, or `node.RPC(node.Neighbor(1))` to obtain the next node in a ring. For gRPC, use `lab.GRPCPeers(node, pb.NewReplicationClient)` to obtain standard clients. Put startup logic such as initial-token setup and election timers in `node.OnStart`.

The protocol name and description can be written in the file header of `main.go`: `// distvis:name Name` and `// distvis:description Description`. For protocols that do not send messages automatically after nodes start, add `// distvis:start node-2 Application.Sync {"minMs":0} Description` to tell the interface where to begin. Entry points are detected automatically; projects with multiple entry points can add `// distvis:entry ./cmd/peer`. The four examples no longer use separate JSON files; legacy `distvis.json` files remain supported.

## Local startup

Node.js 22 or later is required. The frontend and backend do not depend on third-party runtime packages, so `npm install` is not needed.

If the API does not specify `runtime`, it defaults to Docker/Go. Unit tests and offline demonstrations can explicitly specify `runtime: "simulation"`.

```bash
cd distvis
npm start
```

Open **http://localhost:3000** in your browser, then open or create a Go protocol under **My Protocols**. The protocol workspace provides **Protocol Code / Experiment List**. Create experiments with different parameters under it, then open an experiment’s **Visualization / Run History**. Clicking **Run Experiment** starts real Docker Go nodes by default. **Built-in Reference Model** runs a JavaScript model and does not execute the protocol’s Go code.

```bash
# Optional: change the port and history storage location
PORT=3100 DISTVIS_DATA=/absolute/path/to/history npm start
```

The service listens only on `127.0.0.1`. It is intended for personal, local classroom labs and does not support multi-user authentication or public deployment.

## Real Go nodes

### Docker

1. Start Docker Desktop and confirm that `docker version` can connect to the daemon.
2. Select the runtime **Local Docker · Go Nodes**.
3. After an experiment is created, the coordinator builds an image using `registry-1.docker.io/library/golang:1.24-alpine` → `registry-1.docker.io/library/alpine:3.21`, then starts the specified number of containers. Explicit registry addresses avoid invalid image mirrors configured for Docker Hub.
4. Each container runs an independent Go program as a non-root user, limited to 128 MiB / 0.5 CPU / 64 PIDs, with direct networking disabled. All protocol messages are relayed through the SDK and coordinator.
5. Node crashes invoke `docker kill` in reality. Recovery restarts the same container and runs the Go program again. Persistent data in `/state` is retained.
6. When the experiment ends, its containers and images are cleaned up automatically; logs, configuration, and source code are retained.

The first run must download the base images. Go does not need to be installed on the host. Build or container-start errors appear in the event stream.

If a build fails, the workbench displays the specific error and expandable build output. If the error contains `yourcode.mirror.aliyuncs.com` and `403 Forbidden`, the cause is a placeholder image-mirror address in Docker configuration; the failure occurs while downloading the base image. In Docker Desktop → Settings → Docker Engine, remove that address from the `registry-mirrors` array, keep any other configuration, then click **Apply & restart**. The project now explicitly uses official registry endpoints. After changing project code, restart `npm start` and create a new experiment to retry; historical failure records are not rewritten.

If the host cannot access Docker Hub directly, specify an available registry of your own:

```bash
DISTVIS_GO_IMAGE=registry.example.edu/course/golang:1.24-alpine \
DISTVIS_RUNTIME_IMAGE=registry.example.edu/course/alpine:3.21 \
npm start
```

`registry.example.edu` above is only an example. Replace it with a real registry that contains the required images and that Docker on the host can pull from. The build image must include Go and a shell. The runtime image must provide Alpine-compatible `adduser`, `sleep`, and `killall`. These two variables are recorded in the build event and Dockerfile for the run; they must not contain newlines or additional build instructions.

### Docker Desktop Kubernetes

First enable Kubernetes in Docker Desktop and verify:

```bash
kubectl --context docker-desktop get nodes
```

Select **Docker Desktop K8s · Go Nodes**. The adapter always uses the `docker-desktop` context; it does not use the current default context or operate on other clusters.

The coordinator creates a dedicated namespace for each experiment and one Pod per node, using locally built images (`imagePullPolicy: Never`). Docker Desktop Kubernetes must be able to see local Docker images. With Kubernetes installations that do not share image storage, import the images into the cluster first; this version does not automatically push images to a registry.

The Go program is started using `kubectl exec -i`. A crash terminates the Go process inside that Pod, and recovery starts the process again. The Pod and `emptyDir` remain in place, simulating a node-program crash rather than destroying the Kubernetes worker node.

## Writing protocols

Create or open a protocol under **My Protocols**, then edit `main.go`, `protocol.go`, `go.mod`, and protobuf files on the **Protocol Code** page. Files can be created, renamed, and deleted. Initial code is selected only when the protocol is created. One protocol manages one Go project; under **Experiment List**, you can create experiments with normal networks, slow networks, different node counts, and other settings.

Each experiment separately saves the node count, latency, bandwidth, runtime, and reference-model seed. Creating an experiment saves only its configuration; it does not start nodes. After opening an experiment, choose **Run Experiment** or **Save Configuration Only**. Running automatically opens the visualization. Application inputs, fault injection, and message records are associated with that run. **Run History** contains only runs for the current experiment. The breadcrumb at the top returns to the parent protocol, and **View Protocol Code** inside an experiment opens the same shared code.

Protocol code must be saved before running an experiment. Changes apply to subsequent runs of all child experiments. Past code snapshots, parameters, and execution records remain unchanged and can be replayed, exported, or viewed read-only. **Continue Editing This Version** copies historical code into a draft for the parent protocol; after saving, it is used by all experiments under that protocol. Code is independent between protocols.

Unsaved drafts are cached in the browser by protocol ID. Legacy workspaces retain their experiment IDs, parameters, and history. Workspaces with the same source and code are grouped under the same protocol; different code remains in separate protocols. Drafts from older browsers are restored as separate protocols to prevent conflicting drafts from overwriting shared code; the original cache is retained.

Under **Create Protocol → Discover Code from Local Protocol Directory**, specify the project root directory. The default is `protocols/`; you can also set `DISTVIS_PROTOCOL_DIR`. Each subdirectory is a Go module, including support for third-party dependencies and generated protobuf code. Selecting one creates an editable version of the protocol; saving does not overwrite the original directory. See the [Go API](docs/GO-API.md) for details.

All nodes use the protocol code by default. Advanced settings can still configure node replicas and the build entry point, and all files are archived for every run. Example entry points use `lab.Main`; RPC automatically generates application input forms. Reference-model runs include the Go reference project but do not execute it.

The following low-level SDK approach is still available for directly studying message passing:

```go
package main

import (
    "context"
    "distvis/sdk"
)

func main() {
    node, err := sdk.Open()
    if err != nil { panic(err) }
    must := func(err error) { if err != nil { panic(err) } }

    must(node.Report(map[string]any{"role": "ready"}))
    if node.ID == "node-1" {
        must(node.Send("node-2", map[string]any{
            "type": "Hello", "text": "你好",
        }))
    }
    for {
        msg, err := node.Receive(context.Background())
        if err != nil { return }
        must(node.Report(map[string]any{
            "lastFrom": msg.From, "payload": string(msg.Payload),
        }))
    }
}
```

- `node.ID` / `node.Nodes`: logical addresses for this node and all nodes, such as `node-1`. You do not need to manage IP addresses, ports, or DNS.
- `Send(to, payload)`: submits a JSON message. Success means it was submitted to the coordinator, not that delivery is guaranteed.
- `Receive(ctx)`: reads a message and automatically records a receive acknowledgement.
- `Report(state)`: reports any JSON-serializable state, which immediately appears in the node operation panel.
- `DeclareInput([]sdk.InputAction)`: lets a node program declare application-level inputs; the browser automatically generates a form. Inputs arrive through `node.Commands` as `{id, action, values}`. `cmd.Values` is a `json.RawMessage` that preserves types such as numbers, booleans, and objects.
- `sdk.Save(value)` / `sdk.Load(&value)`: saves protocol state to `state.json` in the working directory. The examples persist the Raft term and `votedFor`.
- stdout is the SDK’s JSON protocol channel; write ordinary debug logs to stderr.

### Declaring application inputs for a protocol

Click a node (or its blue **Run** badge in the upper-left corner to jump directly to the first input field). In the popup panel, fill in the form under **Run on node-x**, then click **Run on node-x**. Target-node fields (Go field tags `distvis:"node"` / `distvis:"nodes"`) appear as node dropdowns; the latter allows multiple selections. If a node declares multiple actions, select an action before filling out its fields. Inputs apply to the currently running live node; pausing or viewing the past does not send inputs into the past. Offline nodes and ended experiments cannot accept submissions. Input declarations, all fields, input failures, and subsequent protocol messages are included in experiment history. Application inputs are marked with blue diamonds in the space-time diagram.

RPC protocols only need to annotate application services: fields are extracted automatically from method signatures. There is no need to call `DeclareInput` or manually receive `Commands`. The explicit declaration API below remains available for low-level message SDKs and special form requirements.

Forms are not hard-coded by protocol name. Low-level Go protocols can call `DeclareInput`; built-in models declare `inputs` in actors in `server/protocols.js` and implement `command`. Different nodes can declare different actions, and declarations can be repeated to update the interface. Supported field types are `text`, `number`, `boolean`, `select`, and `json`, with required-field, numeric range/integer, and text-length constraints. The platform validates fields against their declarations and does not implicitly convert strings to numbers. Limits: 64 actions, 16 fields per action, and 8 KiB per input.

```go
must(node.DeclareInput([]sdk.InputAction{{
    Action: "propose", Label: "Submit a number",
    Fields: []sdk.InputField{{
        Name: "number", Label: "Proposed number",
        Type: "number", Required: true, Integer: true,
    }},
}}))
// Put the Commands branch in the protocol's own select event loop.
for cmd := range node.Commands {
    if cmd.Action != "propose" { continue }
    var input struct { Number float64 `json:"number"` }
    if err := json.Unmarshal(cmd.Values, &input); err != nil { continue }
    // Execute protocol logic here; use Send for network messages and Report for results.
    must(node.Report(map[string]any{"inputId": cmd.ID, "proposed": input.Number}))
}
```

Generic HTTP endpoint: `POST /api/runs/:id/commands`. For example, request `{"node":"node-2","action":"propose","values":{"number":42}}`; the response includes `commandSeq` and `id`. HTTP success means the input has been handed to the execution channel. The result of application processing / consensus should be determined from node state. The legacy LWW request format `{node,key,value}` remains compatible.

Built-in examples:

- **Token ring:** each node can submit a payload and process it when it holds the token. Submitting again replaces any unprocessed payload. State shows pending and most recently processed inputs, and the next Token message carries the payload. Submitting input does not create a new token.
- **Raft leader election:** any node can submit a number and wait for a Leader to forward it; during a partition, it waits and retries. The Leader replies when it receives the number. This demonstrates only the proposal entry point and routing. **“Leader accepted” does not mean the proposal was committed by a majority**; Raft log replication is not included.
- **LWW:** declares key/value input and continues to support writing to any replica.
- **Custom Go:** the Hello example includes custom message input. You can modify the declaration and handling logic without changing the frontend or coordinator.

Existing history remains unchanged; new declarations and updated example code take effect when new experiments are created.

See [examples/node/main.go](examples/node/main.go) and [examples/custom.go.txt](examples/custom.go.txt).

## Faults and replay

- Crash / recover nodes.
- Break directed links between any two nodes, or set fixed latency and bandwidth limits. Select A→B, B→A, or both directions. Changing only one direction leaves the other direction’s rules unchanged. If the two directions have different rules, selecting **Both directions** first prompts you to confirm overwriting them.
- All operations for a node or link are handled in the same popup panel. Click a node to see application inputs, crash / recover controls, links to other nodes, and current state. Click a connection (or a link row in the panel) to edit that link directly, without selecting the target again. Esc closes one level at a time: link → node → panel. On mobile, the panel becomes a bottom drawer.
- **Restore All Links** and recent fault records are in the **Network** menu on the top control bar. This resets link latency, bandwidth, and disconnections; it does not recover crashed nodes.
- Operation buttons, link rules, and recent faults in the panel always read live state and are marked **Live**. Node status rows follow the replay position and are marked **Replay @ time**.
- Bandwidth is measured in **KiB/s**, calculated from the UTF-8 byte count of message JSON. Messages queue in each direction, and the two directions are independent.
- Faults take effect during execution. In-flight messages check the link and node generation at receive time. Messages from before a crash do not suddenly reappear after restart.
- Pausing, slow playback, stepping one event at a time, and dragging the timeline only change the **observation position**; the protocol continues running in the background. Faults and writes always affect the current live experiment.
- Slow playback supports **0.01×, 0.025×, 0.05×, 0.1×**, as well as the existing 0.25×–4× range. At 0.01×, one second of recorded activity takes 100 seconds to replay. Selecting a speed while watching live switches to replay; **Return to Live** resumes following the latest records.
- **Topology Animation / Space-Time Diagram** can be switched at any time and share the same playback position and speed. Time runs from left to right in the space-time diagram, with one row per node. Pan by dragging horizontally while holding the mouse button, using the horizontal scroll wheel, or using Shift + scroll. You can also use the bottom slider, arrow buttons, or left/right arrow keys after focusing the diagram. **Return to Playback Position** resumes following the playhead. Panning changes only the viewport, not replay progress, and does not reveal events beyond the current replay position. Larger topologies can be scrolled vertically.
- By default, the space-time diagram shows one second per screen and can be zoomed to 100/250/500 milliseconds or 2/5/10/30 seconds. It supports filtering by node and hiding heartbeats and replies. Overlapping labels are hidden automatically; hover or focus to show them, and click an arrow to view its full content. Repeated identical state reports are omitted from the diagram only; original logs are retained.
- The space-time diagram displays sends, receives, drops, state changes, and faults up to the replay position. Solid lines indicate received messages, dashed lines indicate messages not yet received, and a red × indicates a dropped message. Each window displays at most the latest 250 messages and 150 state-change/fault markers, along with actual counts. Zoom the timeline or use the event stream to inspect other records.
- Topology messages use directional arrows, with labels showing key payload fields. Click an arrow to pause at the current observation position and inspect sender/receiver nodes, message ID, time, and payload. The node operation panel displays main state fields, with expandable full JSON. Nodes, messages, and state markers in the space-time diagram are also clickable and support Tab / Enter.
- Events are sorted by coordinator time and monotonically increasing sequence number. Sends and receives are linked by message ID. Short messages in the animation are displayed for at least 550 ms for easier observation; the event log is the source of exact timestamps.
- Historical replay reconstructs the state recorded in the log; it does not re-execute user code. Built-in simulations can be reproduced with a fixed seed; scheduling of real Go programs is not guaranteed to be reproducible.

Protocols are saved to `data/protocols/<protocol-id>.json`, including the Go project, source, and version number. Experiments are saved to `data/experiments/<experiment-id>.json`, including `protocolId`, parameters, and version number. Each run is saved independently under `data/<run-id>/`, linked through `config.experimentId` and `config.protocolId`, and records the `protocolRevision` used during execution:

```text
meta.json       Parent experiment ID, run name, protocol, node count, network defaults, seed, and run status
events.jsonl    Message, state, fault, user-write, and lifecycle events
source.json     Immutable multi-file code snapshot and node replicas for this run
build/          Actual build context for container experiments
```

After the coordinator restarts, it reloads history. Runs interrupted unexpectedly are marked `interrupted`; their existing traces can be replayed, but execution is not resumed automatically. After a hard exit, containers may still exist. Inspect Docker resources using the `distvis.run=<run-id>` label, or inspect and clean up the corresponding `distvis-<first 8 characters of run ID>` namespace.

## Example scope

| Example | Demonstrates | Explicitly not included |
| --- | --- | --- |
| Raft leader election | Random timeouts, terms, one vote per term, majority elections, heartbeats, re-election after faults | Log replication, commits, snapshots; it cannot demonstrate the safety of a complete Raft storage system |
| Token-ring mutual exclusion | Token handoff, critical sections, loss of progress after a crash | Token-loss detection and reconstruction |
| LWW replica storage | A register resolved by `(logical version, writer)`, periodic anti-entropy, convergence after partition, browser-client writes | Linearizability, transactions, deletion semantics |

The teaching simulation and Go example implement the same teaching rules but do not use the same execution engine. SDK network faults apply only to SDK messages and are not equivalent to kernel-level TCP/UDP faults. Custom networking in Kubernetes that bypasses the SDK is not controlled.

Limits: 2–12 nodes; by default, at most 10 minutes or 60,000 events. Each node’s output is limited to 64 KiB, and total stdout per process session is limited to 4 MiB. These limits control resource consumption for local experiments.
