# Lamport mutual exclusion

**Lecture reference:** L04 — Lamport's fully distributed mutual exclusion: a
`Request`/`Reply`/`Release` broadcast, requests queued in **timestamp total
order**, replies deferred while a strictly earlier request exists. (The
message count below proves the lineage: 3(n−1) per entry — Ricart-Agrawala is
the §4.4 optimization without the Release, 2(n−1).)

Distributed mutual exclusion with **no coordinator, no token, no shared
memory**: every process runs the same code and decides for itself who enters
next, using only Lamport timestamps and the messages it receives. You trigger
each process's **request** and **release**; the `req`/`ack`/`release` exchange
runs the algorithm by itself.

Compare [`../netrpc-token`](../netrpc-token) (token ring) and
[`../logical-clocks`](../logical-clocks) (the clock rules themselves).

## The algorithm, in five lines

```
REQUEST CS : tick; broadcast req(myTS, me); wait for an ack from EVERY other process
ON req     : L = max(L, ts)+1; queue it;  ack now UNLESS I am in CS or my own
             pending request sorts earlier — otherwise DEFER the ack
ON ack     : tick; remember who acked; ENTER when all others acked me and my
             request is at the queue head
ON release : L = max(L, ts)+1; the request leaves the queue; send every deferred
             ack that is no longer blocked
RELEASE CS : remove my request; broadcast release; send my deferred acks
```

The **total order** of requests is `(timestamp, nodeID)`: timestamps may tie,
so the node id breaks them (the lecture's `N*L+i` idea, reduced to "id
decides"). Every comparison in the code goes through one function, `before()`.

The safety argument fits in one sentence: *a request is only ever blocked by a
strictly earlier request, so requests enter in total order, one at a time.*
The entire "ack now or defer?" decision lives in ONE function,
`blockedByMeLocked`, reused unchanged when a request arrives and when a release
frees the queue.

## What you do, what you watch

| Action (any node) | Effect |
|---|---|
| `Application.RequestCS` | Broadcast `req(myTS, me)`. Entry into CS happens **automatically** once every process has acked and the request is at the queue head. |
| `Application.Release` | Leave CS: remove the request, broadcast `release`, hand out the deferred acks. Only legal for the node inside CS. |
| `Application.Read` | Snapshot: clock, status, queue, acks, deferred list, event log. |

Every node reports `status` (`idle → requesting → in-cs`), `myTS`, `acksHave`
/ `acksNeed`, the sorted `queue`, the `deferred` list (who is waiting for an
ack from *me*), `entryOrder` (every request that entered CS, in order) and a
`recentEvents` log with the Lamport clock value at each event — the same
numbers the lecture picture asks you to fill in.

### Try it

Three nodes, Docker, ~80 ms latency.

1. **Quiet request:** `RequestCS` on node-1. Watch its state: `req` messages
   leave, acks come back (`acksHave 2/2`), then `status: in-cs`. Nobody had to
   defer anything.
2. **Contention:** while node-1 is in CS, `RequestCS` on node-2 and node-3.
   node-1's `deferred` list grows to `[node-2 node-3]` — it withholds its ack.
   node-2 and node-3 ack each other or defer, strictly by `(TS, id)`.
3. **Release:** `Release` on node-1. The deferred acks go out and exactly one
   of the waiting requests enters; the other still waits. Repeat until all
   three are through. `entryOrder` shows the total order they agreed on.
4. **Stale acks:** request again quickly after releasing. The event log shows
   `ignore stale ack ... for req(...)` — an old ack never opens the door for a
   new request (that is what would break mutual exclusion).
5. **Failure boundary (deliberate):** block a link or crash a node while
   someone is `requesting`. The requester waits forever — Lamport's algorithm
   assumes no failures, and this protocol implements exactly that. Production
   systems add failure detectors and reconfiguration; this lesson does not.

Use the platform's **concurrent inputs** ("同时在其他节点运行") to make several
nodes request in the same instant — that is where the interesting interleavings
come from.

## Reading order

1. [`main.go`](main.go) — wiring; two services (`MutexService` messages,
   `Application` actions).
2. [`clock.go`](clock.go) — the Lamport clock (two rules!) and the `(TS, node)`
   total order.
3. [`protocol.go`](protocol.go) — messages, reported state, queue helpers.
4. [`mutex.go`](mutex.go) — the algorithm: handlers, `blockedByMeLocked`,
   `tryEnterLocked`, the two actions.
5. [`protocol_test.go`](protocol_test.go) — executable versions of every claim.

## Message fields worth understanding

- `req(ts, from)`: `ts` is the timestamp of the **request event**. One
  broadcast, one timestamp, every copy identical — the request is one logical
  event even though it travels in N−1 messages.
- `ack(from, ts, forTS)`: `forTS` names the request being approved (stale
  matching), `ts` is the sender's clock at the ack event (clock propagation).
- `release(from, ts)`: removes that process's request from every queue.

Each receive merges clocks (`L = max(L, ts)+1`), so after the exchange every
clock has moved past every timestamp it has seen — exactly the numbers you
fill into the lecture picture.

## Guards, and what breaks without them

| Guard | Without it |
|---|---|
| only idle nodes may request | two requests from one process corrupt its queue identity |
| only the CS holder may release | anyone could open the door early |
| ack matched by `ForTS == myTS` | a late ack for an old request grants entry to a new one — mutual exclusion broken |
| queue dedupes by `(TS, node)` | a retransmitted request queues twice and skews the order |
| malformed/unknown senders rejected | garbage timestamps poison the clock comparison |

## Deliberate limits

- **No failure handling** (your choice, and the textbook's): a crashed or
  unreachable peer simply never acks, so pending requests block. Safety is
  preserved; liveness is not.
- No clock drift (Lamport clocks are counters, not real clocks), no Byzantine
  behavior, no request cancellation, no queue persistence across restarts.
- Entering CS is automatic; the protocol does not *do* anything inside the
  critical section — that is your application's business.

## Tests

```sh
go -C protocols/lamport_mutex test -race -count=1 ./...
go -C protocols/lamport_mutex vet ./...
```

Covered: both clock rules, the `(TS, node)` total order with tie-breaks and
dedupe, stale-ack rejection (with clock merging), the action guards, a
deferred-ack round over real net/rpc (defer → release → enter, in total order),
and a three-node contention storm asserting **mutual exclusion is never
violated** and entries follow the total order.
