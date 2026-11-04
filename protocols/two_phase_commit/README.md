# Two-phase commit bank transfer

Implements [LAB1 §5.5](../../docs/LAB1.en.md#55-two-phase-commit-2pc). Node-1 is the **TM** [transaction manager, the coordinator]; all other nodes are **RMs** [resource managers, each owning one account initially containing 100]. The submission scenarios use three nodes, so the total balance is 200.

## Implementation

- [main.go](main.go) registers TM or RM services, loads durable state, and starts recovery.
- [protocol.go](protocol.go) implements Prepare/votes, the durable decision, repeated notifications, participant status polling, and application methods.
- [variant.go](variant.go) exposes the deliberate missing-prepare-log experiment without needing a second protocol copy.
- [protocol_test.go](protocol_test.go) checks durable prepared state, repeated commit behavior, vetoes, storage errors, and refusal to invent a forgotten prepare.

1. Transfer reserves a unique transaction ID and records the preparing transaction before returning an accepted state. A background worker sends Prepare concurrently to **every** RM, including zero-delta participants if the experiment has additional accounts.
2. Each RM checks its account and current lock. A YES voter saves the prepared record [the pending delta and promise to await the decision] before sending its vote; an insufficient balance votes NO.
3. The TM commits only if every RM votes YES. Any NO or missing vote selects abort. It saves the decision before any Commit/Abort notification: this durable write is the **commit point** [the point after which recovery must preserve that decision].
4. The TM retries its saved decision until all RMs acknowledge. An RM applies a committed delta only once and persists the resulting balance and outcome before acknowledging.
5. Prepared RMs poll TM.Status. If the TM is unreachable, they become uncertain and remain locked. On TM recovery, an unfinished transaction without a saved decision is durably aborted; a saved commit is resent.

An Abort arriving before a delayed Prepare leaves a durable tombstone [a record that the transaction is already aborted], so that late Prepare cannot relock the account after the TM finishes. Previous participant outcomes are retained to reject stale conflicting operations and recognize repeated commits. A storage error stops successful votes/acknowledgements until restart rather than allowing an unsaved state to be acknowledged on retry.

## Application inputs

| Method | Fields | Meaning |
|---|---|---|
| `Application.Transfer` on node-1 | `from`, `to`, `amount`; optional `pauseBeforeDecisionMs`, `pauseAfterDecisionMs` | Accept one transfer. Amount must be 1..1000000; pauses 0..60000 ms. |
| `Application.Read` | none | Return and publish the current role-specific state. |
| `Application.Variant` on an RM | `skipPrepareSave` | Before any transaction, deliberately skip saving that RM's prepared vote. Unsafe experiment only. |

A successful Transfer response means **accepted**, not committed. Follow `decision` and `phase`; `phase=finished` means every RM has acknowledged the outcome. Long pauses and blocked transactions do not hold the application RPC open until its deadline.

## State fields

| Field | Meaning |
|---|---|
| `role` | TM or RM. |
| `txn` | Current transaction ID. |
| TM `from`, `to`, `amount` | Requested bank transfer. |
| TM `phase` | preparing, decided, or finished; initially no transaction. |
| TM `decision`, `reason` | Empty, commit, or abort and the reason. |
| TM `votes`, `acks` | Per-participant vote and decision acknowledgement. |
| RM `balance`, `delta` | Current durable balance and pending/recent change. |
| RM `vote`, `state` | yes/no and idle, prepared, uncertain, committed, or aborted. |
| `lastError` | Most recent storage or protocol diagnostic. |

`txn` is the key transaction counter; votes and acknowledgements explain phase completion. In the diagram, the RPC response to RM.Prepare containing `vote:"yes"` is the Vote message, and the response to RM.Commit/Abort is the Ack. There need not be separate methods named Vote and Ack.

## Reproduction

```sh
go -C protocols/two_phase_commit test -race ./...
go -C protocols/two_phase_commit vet ./...
DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run two_phase_commit
```

[scenarios.json](scenarios.json) defines normal two-transfer conservation, insufficient-funds veto, coordinator crash before/after the decision, notification partition, missing participant prepare persistence, and its safe participant-recovery control. Every experiment has its own steps, observation paragraph, and complete exported JSON in the evidence index below. Faults are injected only after the required phase or vote has been observed.

## Discussion

**Why is a majority insufficient?** The debit and credit affect different resources, and one participant may be unable to perform its part. Committing despite its NO would allow only part of the transfer to happen. Paxos chooses one value by majority; 2PC commits a transaction only when all required resource owners have promised to carry it out.

**Why do participants block while the TM is down?** After voting YES, an RM cannot safely assume abort because the TM may already have saved commit. It must keep the lock until it learns a durable decision. The two coordinator-crash experiments differ only in whether that decision was saved, yet correctly end in abort versus commit.

**Why persist before voting YES?** A sent YES is a promise that must remain meaningful after a crash. Without its prepared record, a recovered RM cannot identify or apply the transaction even if another RM has committed. The unsafe variant ends with balances 100/130 and a stuck coordinator, while the safe control preserves 70/130.

**How is the missing prepared log similar to Paxos Amnesia?** Both failures erase a promise after another node has already relied on it. A 2PC participant forgets the pending account change, while a Paxos acceptor forgets the proposal it promised or accepted; recovery can then contradict a decision made before the crash. Saving the promise before replying is therefore part of correctness, not merely an optimization.

**Why is repeated Commit safe?** The RM remembers the committed transaction and resulting balance before acknowledging. A duplicate sees the completed outcome and returns acknowledgement without applying the delta again. This is idempotence [repeating the same operation has no additional effect], not proof that an arbitrary repeated client Transfer is the same transaction.

**Does a failed or timed-out call mean nothing happened?** No. A request or reply can be lost after the remote side acted. The coordinator therefore retries its saved decision, and the participants retain outcomes rather than treating every message as a new transfer.

## Limits

One active transfer at a time, one fixed TM, trusted peers, no coordinator election, no three-phase commit, and no nonblocking recovery while the TM remains inaccessible. Read is a diagnostic local view, not an atomic snapshot of every account at the same instant. A decided transaction can temporarily have one participant committed and another uncertain; the final all-or-nothing outcome requires eventual communication and durable state. The deliberate missing-log variant breaks those assumptions and is not a valid operating mode.

## Verified experiment evidence

One folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.

- [2PC normal: two transfers conserve 200](experiments/normal-two-transfers/README.md)
- [2PC: insufficient balance veto](experiments/veto/README.md)
- [2PC crash: before durable decision aborts](experiments/crash-before-decision/README.md)
- [2PC crash: durable commit completes](experiments/crash-after-decision/README.md)
- [2PC partition: notification retries after heal](experiments/partition/README.md)
- [2PC variant: YES without durable prepared record](experiments/variant-no-prepare-log/README.md)
- [2PC control: prepared log survives participant crash](experiments/participant-recovery-control/README.md)
