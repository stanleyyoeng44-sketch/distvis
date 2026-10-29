package main

import "fmt"

type AutoArgs struct {
	Enabled bool `json:"enabled"`
}
type ModeArgs struct {
	Step bool `json:"step"`
}

// Auto changes periodic rounds, not the reporting ticker. A round already in
// progress finishes; disabling the timer does not cancel a remote correction.
func (a *Application) Auto(args AutoArgs, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.id != s.coordinator {
		return fmt.Errorf("only the fixed coordinator runs rounds")
	}
	s.auto = args.Enabled
	*reply = s.stateLocked()
	return s.report(*reply)
}

func (a *Application) SetMode(args ModeArgs, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.busy {
		return fmt.Errorf("wait for the current round")
	}
	s.stepMode = args.Step
	*reply = s.stateLocked()
	return s.report(*reply)
}

func (a *Application) SetClock(args ClockArgs, reply *State) error {
	return a.ConfigureClock(args, reply)
}
