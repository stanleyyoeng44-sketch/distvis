package main

import "time"

type HeartbeatReply struct {
	Coordinator string `json:"coordinator"`
}

func (s *ElectionService) Heartbeat(_ Empty, reply *HeartbeatReply) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	reply.Coordinator = s.coordinator
	return nil
}

// A timeout is suspicion, not proof of a crash. A partition can therefore
// produce one coordinator in each component. Bully does not provide consensus.
func (s *ElectionService) detectFailure() {
	s.mu.Lock()
	if !s.automatic || s.role == roleCandidate {
		s.mu.Unlock()
		return
	}
	coordinator, round := s.coordinator, s.electionRound
	if coordinator == "" {
		ready := time.Now().After(s.waitingUntil)
		s.mu.Unlock()
		if ready {
			go s.startElection()
		}
		return
	}
	if coordinator == s.id {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	var reply HeartbeatReply
	err := call(s.peers[coordinator], "ElectionService.Heartbeat", Empty{}, &reply)
	s.mu.Lock()
	// Ignore a timeout from an obsolete coordinator or an already replaced round.
	changed := s.coordinator != coordinator || s.electionRound != round
	s.mu.Unlock()
	if !changed && (err != nil || reply.Coordinator != coordinator) {
		go s.startElection()
	}
}

type AutoArgs struct {
	Enabled bool `json:"enabled"`
}

func (a *Application) Auto(args AutoArgs, reply *State) error {
	s := a.service
	s.mu.Lock()
	defer s.mu.Unlock()
	s.automatic = args.Enabled
	*reply = s.stateLocked()
	return s.report(*reply)
}
