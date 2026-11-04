package main

import (
	"encoding/json"
	"errors"
	"testing"
)

func rmForTest() *ResourceManager {
	return &ResourceManager{id: "node-2", log: participantLog{Balance: 100}, save: func(any) error { return nil }, report: func(any) error { return nil }}
}
func TestPreparedStateSurvivesAndCommitIsIdempotent(t *testing.T) {
	rm := rmForTest()
	var disk []byte
	rm.save = func(v any) error { var err error; disk, err = json.Marshal(v); return err }
	// Install the prepared state directly: avoids starting a network monitor.
	rm.log.Txn = &rmTxn{ID: "T1", Delta: -30, Vote: voteYes, State: statePrepared}
	if err := rm.persistLocked(); err != nil {
		t.Fatal(err)
	}
	recovered := rmForTest()
	if err := json.Unmarshal(disk, &recovered.log); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		var reply DecideReply
		if err := recovered.Commit(DecideArgs{Txn: "T1", Decision: decisionCommit}, &reply); err != nil || !reply.OK {
			t.Fatal(reply, err)
		}
	}
	if recovered.log.Balance != 70 {
		t.Fatal("duplicate commit debited twice")
	}
}
func TestNoVoteAndStorageFailure(t *testing.T) {
	rm := rmForTest()
	var reply PrepareReply
	if err := rm.Prepare(PrepareArgs{Txn: "T1", Delta: -150}, &reply); err != nil || reply.Vote != voteNo {
		t.Fatal(reply, err)
	}
	if rm.log.Balance != 100 {
		t.Fatal("veto changed balance")
	}
	rm = rmForTest()
	rm.save = func(any) error { return errors.New("disk failure") }
	for i := 0; i < 2; i++ {
		reply = PrepareReply{}
		if err := rm.Prepare(PrepareArgs{Txn: "T2", Delta: -30}, &reply); err == nil || reply.Vote == voteYes {
			t.Fatal("YES escaped failed persistence")
		}
	}
}
func TestFailedDecisionSaveDoesNotReportCompletion(t *testing.T) {
	rm := rmForTest()
	rm.log.Txn = &rmTxn{ID: "T1", Delta: -30, Vote: voteYes, State: statePrepared}
	rm.save = func(any) error { return errors.New("disk failure") }
	var reply DecideReply
	if err := rm.Commit(DecideArgs{Txn: "T1", Decision: decisionCommit}, &reply); err == nil || reply.OK {
		t.Fatal("unsaved commit acknowledged")
	}
	if rm.log.Balance != 100 || rm.stateLocked().State != stateUncertain {
		t.Fatal("unsaved outcome reported as committed")
	}
	c := &Coordinator{log: coordinatorLog{Txn: "T1", Phase: phaseDecided}, save: func(any) error { return errors.New("disk failure") }}
	c.finishLocked("T1")
	if c.log.Phase == phaseFinished {
		t.Fatal("unsaved finish advertised")
	}
}

func TestConflictingAbortIsNotAcknowledged(t *testing.T) {
	rm := rmForTest()
	rm.log.Txn = &rmTxn{ID: "T1", State: stateCommitted}
	var reply DecideReply
	if err := rm.Abort(DecideArgs{Txn: "T1", Decision: decisionAbort}, &reply); err == nil || reply.OK {
		t.Fatal("conflicting abort acknowledged")
	}
}

func TestForgottenPrepareCannotCommit(t *testing.T) {
	rm := rmForTest()
	if err := rm.Commit(DecideArgs{Txn: "T1", Decision: decisionCommit}, &DecideReply{}); err == nil {
		t.Fatal("invented forgotten transaction")
	}
	if rm.log.Balance != 100 {
		t.Fatal("balance changed without prepared record")
	}
}
