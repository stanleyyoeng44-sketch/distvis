package main

import "fmt"

type VariantArgs struct {
	SkipPrepareSave bool `json:"skipPrepareSave"`
}

// Select on the participant before the transfer. This executes exactly the
// missing stable-storage write discussed in LAB1, without editing between runs.
func (a *Application) Variant(args VariantArgs, reply *State) error {
	rm := a.participant
	if rm == nil {
		return fmt.Errorf("select this variant on an RM")
	}
	rm.mu.Lock()
	defer rm.mu.Unlock()
	if rm.log.Txn != nil {
		return fmt.Errorf("select before any transaction")
	}
	rm.unsafeSkipPrepareSave = args.SkipPrepareSave
	*reply = rm.stateLocked()
	return rm.report(*reply)
}
