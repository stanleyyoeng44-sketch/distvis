# Lab 1: Visualizing Distributed Protocols

Lab 1 for *Operating Systems and Distributed Systems*.

---

## Objectives

The lectures cover many distributed protocols, but slides alone make it hard to build intuition for how they behave on a real network. Messages arrive out of order, nodes crash, the network partitions. It is hard to get a feel from a written description for exactly *where* these failures make a protocol go wrong.

The goal of this lab is to **implement the distributed protocols from the lectures yourself, and use the visualization platform to show how they behave in different scenarios**: both when everything works and what happens after you inject faults.

When you are done, you should be able to answer:

- How do messages flow through this protocol on a healthy network?
- If a node crashes, or the network is cut into two parts, where does the protocol get stuck?
- What happens if you remove a key design decision mentioned in lecture (for example, "write to the log before voting")?

---

## Environment

Everything runs locally. **You do not need a cluster or a token.** You only need:

- **Node.js 22+** (runs the platform server)
- **Docker Desktop** (runs real Go nodes; not needed if you only use the built-in simulation mode)
- Go 1.24+ (optional, for compiling and checking your code locally)

```bash
# Clone the repository, then start the server
cd distvis
npm start
```

Open http://localhost:3000 in your browser to see the platform UI.

---

## 1. Platform Quick Start

This section gets your first protocol experiment running in about 10 minutes. Walk through the whole flow once before you start writing code.

### 1.1 Create a protocol

In the sidebar, click "My protocols (我的协议) → New protocol (新建协议)", choose "**Discover code from local protocol directory (从本地协议目录发现代码)**", and enter the path `protocols/` (relative to the project root). The platform lists the protocols already in that directory for reference. You can also start from scratch with an empty Go project.

Each protocol is a standalone Go module in the `protocols/你的协议名/` directory (`你的协议名` stands for your protocol's name). The platform automatically discovers files such as `main.go` and `protocol.go`, and you can edit, save, and run them right in the browser.

### 1.2 The three-level structure

The platform organizes work into three levels: **protocol → experiment → run**.

- **Protocol**: a body of Go code that defines what the nodes do. Every change you make to the protocol code applies to all later runs of every experiment under it. Existing run records are not changed.
- **Experiment**: a set of parameters (node count, latency, fault mode). One codebase can have several experiments. For example, "3 nodes, normal network" and "5 nodes, high latency" can be saved as two separate experiments.
- **Run**: clicking "Run experiment (运行实验)" produces one actual run. Run records are kept permanently. You can replay them or export them as JSON.

For each observation scenario you usually create a matching experiment, then "run the experiment → inject faults step by step → observe the space-time diagram".

### 1.3 Run your first experiment

1. Create an experiment under your protocol. Set the node count to 3 and the latency to 200ms, and set "Runtime environment (运行环境)" to the **built-in simulation** (the option labeled 内置参考模型 in the UI). It needs no Docker and works right away.
2. Click "Run experiment" to open the visualization view.
3. Click any node. The panel that opens has a "Run on node-x (在 node-x 运行)" section, which runs an application operation on that node.
4. In the "Space-time diagram (时空图)" view, each arrow is a message and each colored square is a state report. Click an arrow to see the full payload.
5. Click the play/pause button (播放 / 暂停), then drag the timeline at the bottom left and right to replay any past state step by step.

> 💡 Built-in simulation vs. real Docker Go nodes: the simulation runs the platform's built-in JavaScript reference implementation and **does not run the Go code you write**. Only Docker mode actually compiles and runs your Go program. While writing code, use the simulation to check your understanding of a scenario, then switch to Docker when debugging your code.

### 1.4 Fault injection

Click a node. The panel that opens offers three kinds of actions:

- **Run on node-x (在 node-x 运行)**: run an application operation on the node (Put/Get/Propose/Acquire, and so on)
- **Crash / recover**: simulate the node process crashing (`docker kill`) and restarting (the "Simulate crash (模拟崩溃)" / "Recover node (恢复节点)" button)
- **Links**: for "this node ↔ other nodes" (the "Links to other nodes (与其他节点的链路)" section), set one-way or two-way blocking, fixed delay, or bandwidth limits

The "Network (网络)" menu in the top control bar has a one-click "Restore all links (恢复全部链路)". It does not recover nodes that have crashed.

### 1.5 Space-time diagram and replay

The space-time diagram is the main observation tool. The horizontal axis is time, and each row is a node:

- **Solid arrow**: the message has been received by the target node
- **Dashed arrow**: the message has been sent but has not arrived yet (it is in the network)
- **Red ×**: the message was dropped (node crashed, link blocked, or random packet loss)
- **Colored square**: a state change reported by the node via `node.Report(...)`; click it to see the full JSON

The diagram is drawn using the actual timestamps of events. For a slanted arrow between two nodes, the slope is the message's actual propagation delay.

---

## 2. Your Tasks for This Lab

### 2.1 What you need to do

This lab covers the main protocols from three lectures: **L03** (time and clocks), **L04** (mutual exclusion and election), and **L05** (replication and consensus).

From the table below, **choose at least 6 protocols** (at least 1 per module), implement them on the platform, and demonstrate the required observation scenarios for each one:

| Module | Protocols | Required scenarios |
|------|------|---------|
| **L03 Clocks** | Cristian / NTP / Berkeley / Lamport + vector clocks (choose 3 of 4) | See §3 |
| **L04 Mutual exclusion** | Centralized / Lamport / Ricart-Agrawala / majority voting (choose 2 of 4) | See §4 |
| **L04 Election** | Bully election (required) | See §4 |
| **L05 Replication** | Primary-backup replication / Quorum KV (choose 1 of 2) | See §5 |
| **L05 Consensus** | Basic Paxos / Paxos KV / two-phase commit (choose 2 of 3) | See §5 |

For each protocol you choose, you need to:

1. **Normal scenario**: show the protocol executing correctly on an ideal network, using node state reports to explain each step of the algorithm.
2. **Fault scenario A**: interruption or recovery behavior caused by a node crash (`crash`).
3. **Fault scenario B**: abnormal behavior caused by a network partition (blocked links) or high latency.
4. **Design variant**: change one key implementation detail (see each protocol's description) and demonstrate why the protocol needs that design.

> The design variant is the heart of this lab. Getting a protocol to "just run" is easy. Demonstrating "what goes wrong without persistence" or "what happens if you don't wait for a majority" requires you to really understand why each step of the algorithm is designed the way it is.

### 2.2 Implementation requirements

**State reports**: every node calls `node.Report(...)` to report its key state, so that observers can see the protocol's current phase in the node panel of the space-time diagram. At a minimum, report the node's current role, the result of its most recent operation, and key counters (such as the term number, timestamps, and message counts).

**Application input**: use `node.DeclareInput(...)` to declare operations that can be sent from the UI, which makes manual demos easy. RPC protocols (recommended) generate the input forms automatically, so you don't have to write this part by hand.

**Reproducibility**: for each scenario, write a short description of the steps, so that someone else following them can reproduce what you observed.

**Scope**: you do not need to implement optimizations that the lecture notes don't cover (such as a Multi-Paxos leader, log snapshots, or membership changes), but you should know clearly what you did not implement and where the boundaries are.

### 2.3 Using automated scenario scripts (optional)

The platform provides a `scripts/scenarios.mjs` script that can automatically create experiments on the platform, replay scenarios, and check assertions:

```bash
node scripts/scenarios.mjs setup          # create experiments for all scenarios
node scripts/scenarios.mjs run paxos-basic case1-no-prior  # replay one scenario automatically
```

You can create a `scenarios.json` in your own protocol directory to define each scenario's steps and assertions; the script runs them and reports pass/fail results. The scenario file format is described in the appendix of this document. This is not required, but it helps a lot with debugging.

---

## 3. L03 Time and Clocks

### 3.1 Cristian clock synchronization

**Lecture**: L03

**Key ideas**: the client sends an RPC to the time server, records the send time $T_0$ and the return time $T_1$, and corrects its local clock using the server's returned time $T_s$ plus the estimated network delay $(T_1 - T_0) / 2$.

On the platform, each node has a simulated local clock (with an initial offset and an exaggerated drift rate) instead of using the system clock directly. This is what lets a single machine demonstrate nodes whose "clocks are out of sync". The `errorMs` field shows how far the local clock deviates from real time.

**Observation scenario A (normal case)**: one synchronization with symmetric delay

1. Look at each node's initial `errorMs`. None of them are zero.
2. Start a `Sync` from node-2 (a client) to node-1 (the time server).
3. After synchronizing, node-2's `errorMs` should be close to zero.
4. Open the space-time diagram: you can see the arrows for the RPC request and reply, and the timestamp node-2 carries in its reply.

**Observation scenario B (asymmetric link)**: error when the one-way delays differ

Set the link delay for node-1→node-2 to 200ms and for node-2→node-1 to 2000ms (or the other way around). Synchronize again and watch how `errorMs` changes. Cristian's algorithm assumes the round-trip delay is symmetric; how large is the error when it is not?

**Observation scenario C (server queuing delay)**: ...

**Design variant**: if you use $0$ instead of $(T_1 - T_0) / 2$ (taking the server time as is), what does the error become? What if you use $(T_1 - T_0)$ (adding the entire delay)?

---

### 3.2 NTP (Network Time Protocol)

**Lecture**: L03

**Key ideas**: NTP records four timestamps $t_0$–$t_3$ and computes `offset = ((t1-t0) + (t2-t3)) / 2` and `delay = (t3-t0) - (t2-t1)`. In real deployments, each NTP client takes 8 measurements and uses the `offset` from the one with the smallest `delay`: the smallest `delay` means the network was most symmetric, so that offset estimate is the most accurate.

`stratum` is the level in the clock hierarchy: a stratum-1 node is directly attached to an atomic clock, stratum-2 synchronizes from stratum-1, and error accumulates with each level.

**Observation scenario A (server processing time is subtracted)**: the server needs some time to process a request, and that time should not count as network delay. Open the space-time diagram, look at the actual send and receive times of the messages, and verify that $t_2 - t_1$ is exactly the server processing time and that it is correctly removed from the delay calculation.

**Observation scenario B (minimum-delay filtering)**: start 8 measurements (with larger link jitter), look at the `delay` value of each entry in the `lastSync.samples` list, and see which one is finally chosen. Why pick the smallest delay rather than the smallest offset?

**Observation scenario C (error accumulates with stratum)**: set up a synchronization chain node-1 → node-2 → node-3 (node-1 is stratum-1) and compare the final `errorMs` of the three nodes.

---

### 3.3 Berkeley algorithm

**Lecture**: L03

**Key ideas**: the master asks every node for its local clock, discards obvious outliers, averages the rest, and then tells each node how many milliseconds to adjust. The key point: nodes do not synchronize to the master's clock; instead, all nodes converge to the same (computed) average. Clocks are only ever moved forward (by smoothly increasing the clock rate) and never jump abruptly, because a jump could make time go backward and break applications that rely on time increasing monotonically.

**Observation scenario A (convergence over periodic rounds)**: after startup, wait a few synchronization rounds and check whether the nodes' `errorMs` values converge to a small range. Note that "small" does not mean "close to zero": the goal of the Berkeley algorithm is for nodes to **agree with each other**, not to synchronize with real time.

**Observation scenario B (outlier clocks are excluded from the average)**: use `SetClock` to manually set one node's clock offset to a very large value (for example, +10000ms), and check whether the master excludes it. Without exclusion, the average gets pulled far off; with exclusion, the other nodes converge normally.

**Observation scenario C (jump vs. smooth adjustment)**: find the `lastAdjMs` (adjustment amount) and `slewMsLeft` (smooth-adjustment remaining) fields in the node state and compare the nodes' clock trajectories under a one-time jump versus gradual smooth adjustment. Think about it: for applications that assume "time never goes backward" (such as logging or cache expiry), what problems would a jump cause?

**Design variant**: electing a new master after a crash is outside the scope of this protocol (it has no automatic master election), but you can demonstrate that once the master crashes, the remaining nodes cannot synchronize and the rounds stop. This contrasts with the Bully election scenario: clock synchronization needs an active coordinator.

---

### 3.4 Lamport clocks and vector clocks

**Lecture**: L03

This protocol has no application logic. It demonstrates the **properties** of two kinds of timestamps rather than providing a "service". Your task is to generate some events, then use `Compare` to verify the mathematical properties of the clocks.

**Key properties** (don't memorize them; be able to verify them on the space-time diagram):

- Lamport: `e → e'` ⟹ `L(e) < L(e')`, but not the converse: `L(e) < L(e')` does **not** imply `e → e'`.
- Vector: `V(e) < V(e')` **if and only if** `e → e'`; two events that do not satisfy this relation are concurrent.
- The total-order timestamp `L.node` defines a total order over all events, but for concurrent events this order is arbitrary (the node ID used to break ties has no physical meaning).

**Observation scenario A (the figure on slide 37 of the lecture notes)**: send messages in the event order shown in the figure, then use `Compare` to check: which of `b` and `e` has the larger Lamport value? What does the vector comparison say? (Expected: `L(b) > L(e)` but `vector = concurrent`.)

**Observation scenario B (concurrent events with equal Lamport values)**: have each of three nodes produce one local event without sending any messages. All three events should have `L` equal to 1. Use `Compare` to verify that they are pairwise concurrent. The total-order timestamp still puts them in some order (1.1 < 1.2 < 1.3). Does that order have any physical meaning?

**Observation scenario C (invariant after automatic events)**: turn on `Auto` mode, let the nodes produce random events, and stop after a while. Pick a few pairs of events by hand and use `Compare` to check: whenever the vector clocks report a causal relation (`vector = a→b` or `b→a`), the Lamport order always agrees with it (`agrees = true`). Conversely, events whose Lamport order agrees may still be concurrent according to the vector clocks.

---

## 4. L04 Mutual Exclusion and Election

The four mutual exclusion algorithms in this lecture (centralized, Lamport, Ricart-Agrawala, majority voting) share **the same application input interface** (`Acquire(holdMs)`, `Auto(...)`) and **the same state fields** (`role`, `entries`, `lastWaitMs`, `messagesSent`). This is intentional: it lets you follow the comparison table in the lecture notes and compare the message overhead and fault tolerance of the different algorithms within the same experimental framework.

### 4.1 Centralized mutual exclusion

**Lecture**: L04

**Key ideas**: the coordinator (`coordinator`, fixed as node-1) maintains a FIFO request queue. A node that wants to enter the critical section sends `Request` and may enter only after receiving `Grant`; it sends `Release` when it leaves. Each critical section entry takes 3 messages (Request → Grant → Release), at the cost of making the coordinator a single point of failure.

**Observation scenario A (normal case)**: have 3 nodes send `Acquire(holdMs=2000)` at the same time. Count the messages in the space-time diagram and verify that each entry into and exit from the critical section takes exactly 3 messages. Use the `entries` counter to confirm that only one node has `role=critical` at any moment.

**Observation scenario B (coordinator crash)**: crash the coordinator while some node is waiting for `Grant`, and observe that the waiting node blocks forever: its `Request` never gets a reply. This is the fatal weakness of centralized mutual exclusion: when the coordinator is unavailable, everyone is stuck.

**Design variant (coordinator forgets on restart)**: this is the core variant scenario. Steps:
1. A node already holds the lock (`role=critical`);
2. Crash and recover the coordinator. The coordinator does **not persist** its queue state (persistence mode turned off);
3. After the coordinator restarts, another node sends `Request`. The coordinator has no record of anything and sends `Grant` right away;
4. Now two nodes both believe they hold the lock, and mutual exclusion is violated.

Turn persistence on and repeat the same steps. Verify that after restarting, the coordinator correctly restores its queue and does not grant the lock twice.

---

### 4.2 Bully election (required)

**Lecture**: L04

**Key ideas**: the goal is to elect the **live node with the highest ID** as coordinator. Any node that detects that the coordinator has failed starts an election: it sends `Election` to the nodes with higher IDs. Receiving `OK` means a higher node is taking over; if no `OK` arrives, the node wins and broadcasts `Coordinator`.

**Observation scenario A (election at startup)**: after 5 nodes start, they hold an election automatically, and node-5 (the highest ID) should win. In the space-time diagram, observe how the three message types `Election` / `OK` / `Coordinator` flow, and compare with the figures in the lecture notes. The node state field `sent` records how many messages each node sent; when all $n$ nodes start an election at once, the total number of messages is $O(n^2)$.

**Observation scenario B (coordinator crash)**: crash node-5, wait for the timeout to fire, and watch the re-election. node-4 starts it, sends Election to node-5 with no response, and eventually wins. The space-time diagram clearly shows the sequence "send Election upward, no OK, broadcast Coordinator".

**Observation scenario C (the 7-node example from the lecture notes)**: use a 7-node experiment, crash node-7, manually have node-4 call `StartElection`, and reproduce the lecture figure step by step in the space-time diagram.

**Observation scenario D (up to two nodes crash at the same time)**: ...

**Design variant (the old coordinator "takes over" after recovering)**: recover the crashed node-7 and observe that after starting it holds an election and becomes coordinator again. This behavior is a defining feature of the Bully algorithm and also its main point of controversy: if a node was wrongly suspected of failure because it was temporarily overloaded (not actually crashed), taking over immediately on recovery can cause frequent leader changes. Think about it: in which situations is this reasonable behavior, and in which is it a problem?

---

### 4.3 Lamport mutual exclusion

**Lecture**: L04

**Key ideas**: fully decentralized, with no coordinator. Each node broadcasts a `Request`, and the other nodes acknowledge it with `Reply`; requests are queued in **timestamp total order**. A node may enter the critical section only when its own request is at the head of the queue and it has received a `Reply` from every other node. On exit it broadcasts `Release`.

Key constraint: a `Reply` must be held back until the other node's timestamp is ordered after the replier's own request, which ensures that a higher-priority request is never preempted. If any single node crashes, everyone waits forever for its `Reply` and blocks.

**Observation scenario A (two nodes request at the same time)**: node-1 and node-3 send `Acquire` at the same time. In the space-time diagram, look at the timestamps of their `Request` messages and how the `Reply` messages cross. The node with the smaller timestamp (higher priority) enters the critical section first.

**Observation scenario B (any single crash blocks everyone)**: crash a node while it is not requesting the critical section, and observe that the other nodes, once they make a request, wait forever for the crashed node's `Reply`. So even the crash of a node that has nothing to do with the current mutual exclusion blocks everyone. This is one of the main shortcomings of Lamport mutual exclusion.

**Design variant (message overhead comparison)**: Lamport mutual exclusion needs $3(n-1)$ messages per critical section entry and exit (with n nodes), while Ricart-Agrawala needs only $2(n-1)$. Why? Watch the `messagesSent` field and compare the message counts of the two protocols in the same scenario.

---

### 4.4 Ricart-Agrawala

**Lecture**: L04

**Key ideas**: an optimization of Lamport mutual exclusion. There is no separate `Release` message; the "release" is folded into the reply to the next request. Requests received while holding the critical section are deferred and all answered at once on exit, which saves one round of messages.

**Observation scenario A (two nodes request at the same time)**: reproduce the example from the lecture notes: node-1 and node-3 `Acquire` at the same time, and the one with the smaller timestamp enters first. The `lastWaitMs` field shows the waiting time.

**Observation scenario B (an uninvolved node's crash still blocks)**: as with Lamport, once a node that is not in the request queue crashes, anyone who makes a request blocks, because that node's `Reply` will never arrive. This weakness is shared by both algorithms.

**Observation scenario C (random load on an exponentially jittered network)**: change the experiment's latency to an exponential distribution and have 5 nodes `Acquire` randomly and frequently using `Auto` mode. Observe: does message reordering (a later message arriving first) break mutual exclusion? By default the platform keeps each link in FIFO order (like TCP), so it does not; but what happens if you switch to `ordering: unordered` (see the platform API docs)?

---

### 4.5 Majority-voting mutual exclusion

**Lecture**: L04

**Key ideas**: a node requests votes (`Vote`) from all other nodes. Once it receives a majority (more than $\lfloor n/2 \rfloor$ votes) it enters the critical section, and on exit it broadcasts `Release` to give the votes back. Each node casts only one vote at a time, which ensures that at most one node can win a majority.

Key weakness: a voter that crashes and restarts **forgets** whom it voted for and accepts vote requests again, which can let two nodes both obtain a "majority". Persisting the vote record avoids this problem.

**Observation scenario A (no contention, succeeds on the first try)**: have only one node make a request, and observe how the votes are collected and how it enters the critical section.

**Observation scenario B (everyone requests at once: split votes and backoff)**: have all nodes `Acquire` at the same time. Each gets only some of the votes, nobody reaches a majority, and everyone backs off and retries. Watch `lastWaitMs` and the number of retries.

**Design variant (voter forgets its vote after restart)**:
1. node-1 has received a majority of votes and is in the critical section;
2. One of its voters crashes and recovers;
3. After the voter recovers, node-2 makes a request. The voter has **no persisted record**, believes it has not voted yet, and votes for node-2;
4. node-2 also collects a majority, and both nodes enter the critical section at once: mutual exclusion is violated.

The persistence fix: the voter writes to disk before replying to `Vote`, so after a restart it knows whom it already voted for and does not vote twice.

---

## 5. L05 Replication and Consensus

### 5.1 Primary-backup replication

**Lecture**: L05

**Key ideas**: node-1 is the primary and the rest are backups. A write request can be sent to any node; backups forward it to the primary. The primary assigns increasing log sequence numbers (`index`, equivalent to a zxid) and then sends the updates to the backups in order.

Replication modes:
- `async`: the primary replies to the client right after applying the write, and the backups receive it later. Fast, but reads can return stale data.
- `sync`: the primary replies only after all backups have acknowledged. Slow, and a write times out with an error if any backup is unreachable.

Read modes:
- `local`: read locally; fast, but possibly stale;
- `primary`: read from the primary; linearizable;
- `session`: carry the `index` of your last write; the backup replies only after it has caught up locally, which guarantees "read your writes".

**Observation scenario A (stale reads with async replication)**:
1. `Mode(async, replicationDelayMs=4000)` makes the backups receive updates 4 seconds late;
2. Write `Put(x, 1)` on node-2; the write succeeds immediately;
3. Immediately read locally on node-2 with `Get(x, mode=local)`: the value is not there, and `stale=true`;
4. Switch to a `primary` read: it returns 1;
5. Do a `session` read with `minIndex=1`: it returns after about 4 seconds.

Watch the `waitedMs` field to understand how session reads wait.

**Observation scenario B (sync replication and a backup crash)**:
1. `Mode(sync)`; write x=1 successfully;
2. Crash node-3, then write x=2. It returns an error after a 4-second timeout, but x=2 has already taken effect on the primary and node-2;
3. The client sees a "failure" even though the data was partially written: a timeout does not mean the operation did not execute.

**Design variant (no automatic failover)**: the primary is fixed as node-1, so while it is down, writes and `primary` reads are all unavailable. This shows that primary-backup replication needs a higher-level mechanism (such as Raft consensus) to decide who the primary is; otherwise it cannot recover automatically.

---

### 5.2 Quorum replication

**Lecture**: L05

**Key ideas**: N peer replicas. Each write collects W acknowledgments, and each read collects R replies and takes the newest version. **W + R > N** guarantees that any write quorum and any read quorum share at least one replica, so a read always sees the most recent successful write.

**Observation scenario A (avoiding the slowest replica)**: set the node-1→node-5 link delay to 3000ms with W=R=3 (5 nodes). A write waits only for the fastest 3 replicas and returns in about 200ms, without waiting for node-5's slow acknowledgment. Then change to W=5 and see the cost of waiting for the slowest replica.

**Observation scenario B (W+R ≤ N leads to stale reads)**: W=1, R=1. node-1 writes x=1 and only acknowledges itself; node-3 reads x by asking only itself. node-3 has not received the write yet, so it reads the old value. This is not a bug; it is the inevitable cost of allowing W+R ≤ N, and `lastGet.stale` marks the read as stale.

**Observation scenario C (the minority partition is unavailable)**: W=R=3, partition {node-1, node-2} | {node-3, node-4, node-5}. The minority side (node-1, node-2) cannot gather 3 write acknowledgments, so its operations fail. This illustrates CP (choosing consistency at the expense of availability).

---

### 5.3 Basic Paxos

**Lecture**: L05

**Key ideas**: two-phase, single-value consensus. The proposer first uses `Prepare{n}` to collect promises from a majority (n must be greater than every proposal number any node has seen), then uses `Accept{n, v}` to get a majority to accept value v. v is the value with the highest proposal number among those already accepted in phase one, or the proposer's own value if there is none. Once a majority accepts, the value is chosen, and the proposer broadcasts `Decide` to announce it.

**The most important point to understand**: any two majorities must intersect (among 5 nodes, any two groups of 3 overlap in at least 1 node). This property guarantees that a second proposer, during its Prepare phase, will learn the old value from some acceptor that already accepted it, and will therefore reuse it. Paxos safety rests entirely on this.

**Observation scenarios A–D (the four cases from the lecture notes)**:

The lecture notes illustrate four cases (what happens when proposers contact different majorities). We suggest working through them in this order:

- **Case 1** (no prior proposal): block the links between N1 and N4/N5, have N1 `Propose(紫荆)`, and run both phases to completion; N1–N3 reach consensus.
- **Case 2** (a value has already been chosen): building on case 1, have N4 `Propose(澜园)` with a higher proposal number. During Prepare, N4 gets `<A 1.1 紫荆>` from N3, so it reuses `紫荆`, and all nodes eventually agree on `紫荆`.
- **Case 3** (sees a value that has not been chosen yet): N1 pauses between the two phases (`pauseMs`), and N4 seizes the moment to propose with a higher proposal number. Because N3 has already accepted N1's value, N4 must reuse it.
- **Case 4** (does not see the old value): adjust the links so that during Prepare N4 reaches no acceptor that has accepted the old value. N4 then proposes its own value, N4's value is chosen, and N1's proposal is rejected.

After each case, find the "chosen point" in the space-time diagram: the moment the Accept message is accepted by the 3rd acceptor is the moment consensus is reached.

**Observation scenario E (livelock)**: turn off backoff on two nodes and have them use `retry=true` so they keep rejecting each other's proposal numbers in an escalating loop. Once backoff is turned back on, one side quickly completes both phases first.

**Design variant (persistence guarantees safety)**: this is the most important variant.
1. Turn on `Amnesia` for one node (it does not persist its promises and votes);
2. Let that node take part in one round of consensus, then crash and restart it;
3. It has forgotten its promises, so a new proposer does not learn the old value during Prepare and chooses a different value;
4. As a result, two different values are both "chosen": consensus is broken.

Control experiment: with normal persistence, the promises survive the restart, the new proposer correctly reuses the old value, and safety is preserved.

---

### 5.4 Paxos replicated state machine KV

**Lecture**: L05

**Key ideas**: generalize Basic Paxos to multiple values. Each slot in the log is an independent Basic Paxos instance. A client sends to any server, and the server proposes the operation in **the lowest empty slot that it does not yet know to be decided**. If another operation wins that slot first, the server retries in the next slot. All replicas execute the log strictly in slot order, so everyone executes the same operations in the same order.

**Observation scenario A (sequential writes and reads)**:
Send Put / Get from different nodes in turn and watch slot allocation in the `log` field. Note that Get also takes a slot: it has to be ordered with the writes to guarantee that it reads the correct version (reading through the log is the key to linearizability).

**Observation scenario B (two writes compete for the same slot)**:
node-1 and node-2 `Put` at the same time, both proposing for slot 1. Only one value can be chosen. The loser sees the other's accepted value during Prepare, helps it complete slot 1, and then proposes its own operation in slot 2 (`conflicts` increases by one). All replica logs end up identical.

**Observation scenario C (crash and catch-up)**:
1. node-5 crashes, and the other 4 nodes keep committing slots 2–4;
2. After node-5 recovers, when the next Prepare message arrives it discovers holes in its log and concurrently proposes a no-op for each hole. Prepare is guaranteed to learn the chosen operation from a majority, so each no-op is replaced by the real operation;
3. node-5's log catches up, and its stored contents match the other nodes.

**Observation scenario D (a minority partition cannot commit)**:
Partition {node-4, node-5} away from the majority. A write sent to node-4 cannot get promises from a majority and reports an error after a 6-second timeout. After the partition heals, the two nodes catch up on the same log.

---

### 5.5 Two-phase commit (2PC)

**Lecture**: L05

**Key ideas**: the coordinator (TM, node-1) first sends `Prepare` to all participants (RMs). It commits only after collecting `YES` votes from everyone; any single `NO` aborts the transaction. **Any party can veto; a majority is not enough.** The key decision happens at the "commit point": the coordinator writes its decision to the log before notifying the participants. A crash before the log write → abort (presumed abort); a crash after the log write → after recovery, the coordinator resends Commit until every participant acknowledges.

The protocol in this lab (node-1 = TM, the rest = RMs, each holding an account balance of 100) simulates a bank transfer.

**Observation scenario A (normal commit)**:
Start a transfer and find the four message types Prepare / Vote(YES) / Commit / Ack in the space-time diagram. The reply `{"vote":"yes"}` to the diagram label `调用 RM.Prepare · {"txn":"T1","delta":-30}` is the vote (调用 means "call" in the UI). Verify that after two transfers the balance is conserved (the total stays at 200), and that all three parties agree on the outcome of every transaction.

**Observation scenario B (a participant vetoes)**: transfer out more than the balance (insufficient balance → vote NO). The other participant has already voted YES and taken a lock, but the transaction aborts and the lock is released. Question: compared with the majorities in Paxos/Raft, why must 2PC wait for **everyone** to vote instead of only a majority as in Paxos? (Hint: Paxos chooses a value, while 2PC commits an operation that has already locked resources.)

**Observation scenario C (coordinator crashes before deciding; participants block)**:
1. Start a transfer with `pauseBeforeDecisionMs=30000`. All participants vote YES, and the coordinator pauses before writing its decision;
2. Crash the coordinator at this point;
3. Every 2 seconds the participants ask the coordinator via `TM.Status`, but get no reply. They enter the `uncertain` state and the accounts stay locked, so no other transfers can proceed;
4. Recover the coordinator. It reads its log, finds no decision for T1, and presumes abort; the participants release their locks.

**Observation scenario D (coordinator crashes after the commit point)**:
1. Start a transfer with `pauseAfterDecisionMs=30000`. The coordinator has written `commit` to its log but crashes before notifying anyone;
2. The participants are still `uncertain`, and the balances are unchanged;
3. Recover the coordinator. It resends Commit, the participants eventually commit, and the balances change.

Compare scenarios C and D: a different crash point gives a completely different outcome. The logical "commit moment" of a transaction is the line where the coordinator writes its log, not the moment a participant sees Commit.

**Design variant (why participants write to the log before voting YES)**:
If a participant crashes after voting YES but before writing to stable storage, it has no `prepared` record after restarting. It will believe it never took part in the transaction, presume abort, and release its locks. But the coordinator may already have received all the YES votes and decided to commit, and another participant may already have committed: the data is now inconsistent.

To demonstrate this problem you need to modify the protocol code: remove the call to `sdk.Save`, then crash that node right after it votes, and recover it. Think about it: how is this similar to the Paxos Amnesia scenario?

---

## 6. Submission Requirements

### 6.1 Code submission

Each protocol you implement goes in its own `protocols/你的协议名/` directory (`你的协议名` stands for your protocol's name), containing:

- `main.go`, `protocol.go` (and any other Go files): the protocol implementation
- `go.mod`: the Go module file
- `README.md`: explains your implementation logic, the meaning of the state fields, the application inputs, and the steps for each observation scenario

If you defined automated scenarios in `scenarios.json`, submit that too.

### 6.2 Observation report

Submit a short report for each protocol (either in `README.md` or in a separate `report.md`), containing:

1. **Screenshots or exported JSON for at least four scenarios**: one each for the normal scenario, fault scenarios A and B (you may design your own), and the design variant. Either space-time diagram screenshots or JSON files exported via "Run history (运行历史) → Export record (导出记录)" are fine.
2. **A short explanation for each scenario** (3–5 sentences): what you observed, whether it matched your expectations, and the reason for any mismatch.
3. **Answers to the discussion questions**: each protocol's README has discussion questions; answer each one in 2–4 sentences.

This doesn't need to be a paper. Just explain things clearly.

### 6.3 Grading

| Item | Description |
|------|------|
| Code correctness (60%) | In the normal scenario the protocol behaves as the algorithm describes, and the meaning of the node state fields is clear |
| Fault scenarios (20%) | Both the crash and partition scenarios reproduce the expected behavior, and the design variant shows why things go wrong |
| Depth of understanding (10%) | Answers to the discussion questions show an understanding of the motivation behind the algorithm's design (not just a description of what happened) |
| Reproducibility (10%) | Someone following the steps in your README can reproduce the scenarios you present |
| Bonus (optional) | Performance optimizations, improvements to the platform's user experience, teaching aids (for example, automatically checking the consistency level of a distributed system, automatically testing distributed protocols, or automatically generating reports), or any other contribution you find valuable. Not required |

---

## 7. FAQ

**Q: I don't see any messages during a run, and the nodes stay in their initial state?**

Check that "Runtime environment" is set to the built-in simulation (if you want a quick check), or that Docker is running (if you use real Go nodes). The built-in simulation runs the platform's built-in JavaScript implementation, not your Go code.

**Q: The arrows in the space-time diagram are too dense to read?**

Use the node filter ("Messages (消息)" dropdown) to show only a few nodes, or zoom the timeline with "Time per screen (每屏时间)" (the default is 1 second per screen; you can change it to 2/5/10 seconds). Check "Hide heartbeats and heartbeat replies (隐藏心跳与心跳回复)" to filter out periodic heartbeat messages.

**Q: After "crash then recover" in a scenario, the node state panel still shows old data?**

After a crash, the node panel shows the last state the node reported, until the process calls `Report` again after recovering. If your protocol does not report right away on restart, the panel updates late. You can call `node.Report(...)` once at startup in your protocol code.

**Q: I want to demonstrate message reordering, but the platform is FIFO by default?**

By default every directed link is FIFO (messages on each link arrive in the order they were sent and are never reordered). To enable reordering, add `"network": {"ordering": "unordered", "reorderRate": 0.5, "reorderMin": 100, "reorderMax": 500}` to `settings` in `scenarios.json`, or set it dynamically with the platform API's `fault(kind: 'network', network: {...})`. See `docs/PLATFORM-API.md` for details.

**Q: Where is data from `sdk.Save` / `sdk.Load` stored? Is it lost on restart?**

It is stored in the container's `/state/` directory (Docker mode). When a container restarts after a crash, the contents of `/state/` are kept, and the Go program can read them back with `sdk.Load`. Ending the experiment cleans up the containers, and the data is lost.

**Q: If I change the protocol code, do old run records change?**

No. On every run the platform saves a snapshot of the code at that time in `data/<run-id>/source.json`. Replays always use the code snapshot from that run, so later changes do not affect them.

---

## Appendix: Quick Reference

### Platform API cheat sheet

```go
// Core SDK interface (distvis/sdk/rpc package, started with lab.Main)
node.RPCPeers(new(YourService))  // register the service and get RPC clients for the other nodes
node.Report(map[string]any{...}) // report state; it appears in the node panel immediately
sdk.Save(value)                  // persist to /state/state.json
sdk.Load(&value)                 // restore from /state/state.json
```

```go
// Low-level SDK (distvis/sdk package, started with sdk.Open; for direct control over messages)
node.Send(to, payload)           // send a JSON message
node.Receive(ctx)                // receive a message
node.DeclareInput([]sdk.InputAction{...}) // declare operations that can be sent from the UI
node.Commands                   // channel that receives input from the UI
```

### Scenario file cheat sheet

```json
{
  "scenarios": [{
    "id": "my-scenario",
    "name": "My scenario",
    "settings": {"nodeCount": 3, "latency": 200},
    "steps": [
      {"note": "Explanatory text; performs no action"},
      {"input": {"node": "node-1", "method": "Application.Put", "values": {"key": "x", "value": "1"}}},
      {"concurrent": [
        {"node": "node-1", "method": "Application.Acquire", "values": {"holdMs": 2000}},
        {"node": "node-3", "method": "Application.Acquire", "values": {"holdMs": 2000}}
      ]},
      {"fault": {"kind": "crash", "node": "node-2"}},
      {"fault": {"kind": "link", "from": "node-1", "to": "node-3", "blocked": true, "bidirectional": true}},
      {"wait": 3000},
      {"until": {"node": "node-1", "path": "role", "value": "leader"}, "timeout": 15000},
      {"check": {"atMostOne": {"path": "role", "value": "critical"}}, "label": "Mutual exclusion always holds"}
    ]
  }]
}
```

Field reference (as implemented in `scripts/scenarios.mjs`):

- `input` / `concurrent`: the action declared by the node is looked up by `method` (the RPC label, such as `Application.Acquire`); inputs in `concurrent` are delivered at the same coordinator instant. `"await": false` does not wait for the result; `expect` can be `ok`, `error`, or `timeout`; `timeout` defaults to 20000ms.
- `fault`: same as the platform fault API; `kind` is `crash`, `recover`, `link`, `heal`, `network`, or `channel`. Fields omitted from a link fault take the experiment defaults; `blocked` / `bidirectional` default to false.
- `note` only prints text; `wait` sleeps a fixed number of milliseconds; `until` polls until the condition holds or times out (default 20000ms); `check` asserts immediately. `label` is the text shown in the output.
- Conditions:
  - `{node, path, op, value}`: `node` can be a node name, `"*"` (every online node matches), `"any"` (some online node matches), or an array; `op` defaults to `==` and supports `== != < <= > >= abs<= abs> includes exists missing in`; `path` is a dotted path (such as `lastSync.rttMs`).
  - `{same: path}` / `{differ: path}`: the field is equal on all nodes / not equal on all nodes (all online nodes by default; use `nodes` to choose).
  - `{messages: {method, op, value}}`: RPC request count; `op` defaults to `>=`.
  - History invariants `{atMostOne: {path, op, value}}` / `{moreThanOne: {...}}`: over the whole run history, at most one node / more than one node matched the condition at the same time.
  - Combinators: `{all: [...]}`, `{any: [...]}`, `{not: {...}}`.
