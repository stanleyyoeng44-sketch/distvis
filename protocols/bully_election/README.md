# Bully election (Garcia-Molina)

**Lecture reference:** coordinator-based services — the bully election, where
the **highest-priority node alive** takes over whenever the coordinator dies.

You are the failure detector: when you believe the coordinator is down (or you
simply want to bully it), you click `StartElection` on a node. The
`ELECTION` / `OK` / `VICTORY` exchange then runs by itself and the cluster
converges on the strongest node still standing.

## The algorithm, in five steps

```
1. START      I become a candidate and send ELECTION to every HIGHER-priority node.
2. OK         Each higher node replies OK ("I am alive and I outrank you") and —
              the bullying part — immediately starts its OWN election.
3. BACK DOWN  If any OK arrived, I stand down and wait: the higher node will win.
4. WIN        If nobody answered (all higher nodes are down), I am the highest
              alive: I broadcast VICTORY and become coordinator.
5. AGREE      Any node that receives VICTORY records the new coordinator and
              abandons its own election.
```

Every `StartElection` therefore converges the cluster on the highest-priority
node that is still alive. Crash the best node and the next-highest takes over —
that is the "bully" in the name: stronger nodes simply override weaker ones.

## Priority: numbers, not strings

Priority is the numeric suffix of the node id: `node-3` outranks `node-2`.
**`node-10` outranks `node-9`** — comparing the id strings would get this
wrong (`"node-10" < "node-2"`), and the test suite checks it.

## What you do, what you watch

| Action (any node) | Effect |
|---|---|
| `Application.StartElection` | Run one full election round from this node. Returns as coordinator, or as a follower that backed down. |
| `Application.Read` | Snapshot: role, believed coordinator, round, win/back-down counters, `higherPeers`, event log. |

Roles: `follower` → `candidate` (campaigning) → `coordinator`, and back down to
`follower` when somebody outranks you or wins. `coordinator` is not special
code — it is just the node that won the last election.

### Try it

Three nodes, Docker, any latency.

1. **First election:** `StartElection` on **node-1** (the lowest priority).
   Watch its log: `ELECTION` leaves to node-2 and node-3, both reply `OK`, it
   backs down. Meanwhile node-3 bullies its way to the top and broadcasts
   `VICTORY`. All three report `coordinator: node-3`.
2. **Bully the leader:** `StartElection` on node-2 while node-3 is healthy.
   node-3 is bullied into a quick re-election (`BULLY node-2: start my own
   election`) and reasserts itself. Same coordinator, `electionsWon: 2`.
3. **Crash the best:** `crash` node-3 in the faults panel, then `StartElection`
   on node-1. node-1 backs down toward node-2; node-2's `ELECTION` to node-3
   fails, so node-2 wins. The live nodes agree on `coordinator: node-2`.
4. **Recovery takeover:** `recover` node-3, then `StartElection` on any node.
   node-3 bullies straight back to the top — the strongest node always wins.
5. **Split brain (the documented limit):** block BOTH links to node-3
   (`link` node-1↔node-3 and node-2↔node-3, `blocked: true`,
   `bidirectional: true`), then `StartElection` on node-1. node-2 wins among
   the reachable nodes while node-3 still believes IT is coordinator. Two
   leaders, no failure detector — this is exactly what the textbook algorithm
   does NOT solve. `heal`, run one more election, and everyone agrees again.

## Reading order

1. [`main.go`](main.go) — wiring; two services (`ElectionService` messages,
   `Application` actions), and where priority comes from.
2. [`protocol.go`](protocol.go) — messages, reported state, the priority rule.
3. [`bully.go`](bully.go) — the whole algorithm: `campaign`, the two handlers,
   the `StartElection` action.
4. [`protocol_test.go`](protocol_test.go) — executable versions of every claim.

## Guards, and what breaks without them

| Guard | Without it |
|---|---|
| one election per node at a time | two overlapping campaigns double-count OKs and split the node's own vote |
| ELECTION only accepted from members | an outsider could trigger elections or steal leadership |
| OK only to nodes I outrank | a lower node could "win" while a higher one lives |
| campaign results carry a round number | a campaign that already lost could resurrect itself over a fresh victory |
| victory from a member only | an outsider could declare itself coordinator on every node |

## Deliberate limits

- **You are the failure detector.** No heartbeats, no timeouts deciding
  anything: elections start only when you say so. That keeps every step
  visible — and it is the honest scope of the textbook algorithm.
- **The delayed-OK edge:** if a higher node is alive but answers slower than
  the 3 s RPC timeout while a lower node is campaigning, the lower node can
  win first. The higher node's own campaign then re-broadcasts its victory and
  the cluster converges upward; if in doubt, trigger one more `StartElection`.
- Split-brain during partitions (scenario 5) is inherent: without a failure
  detector or quorum there is no way to tell "dead" from "slow". Production
  systems add heartbeats, leases or consensus (see `../raft-election`).
- No authentication of messages, no persistent state across restarts, no
  leader-verified work handoff.

## Tests

```sh
go -C protocols/bully_election test -race -count=1 ./...
go -C protocols/bully_election vet ./...
```

Covered: numeric (not lexical) priority ordering, OK-refusal for senders I do
not outrank, victory standing down a live campaign (and invalidating its
round), the double-start guard, a full three-node election over real net/rpc
where the lowest-priority node starts and the highest wins, and the crash
scenario where the next-highest takes over and then reasserts itself when
bulled again. Run under `-count=50`, these are deterministic.
