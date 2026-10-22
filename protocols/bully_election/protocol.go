package main

import (
	"fmt"
	"net/rpc"
	"strconv"
	"strings"
	"sync"
)

// ─── Messages ────────────────────────────────────────────────────────────────
// The classic bully exchange has exactly three messages:
//
//	ELECTION  "I am holding an election"      -> to every HIGHER-priority node
//	OK        "I am alive and I outrank you"  -> the RPC reply to ELECTION
//	VICTORY   "I am the coordinator now"      -> broadcast by the winner
//
// ELECTION is only ever sent to higher-priority nodes, so the receiver always
// outranks the sender. The OK is the RPC REPLY rather than a separate message:
// DistVis draws request and reply arrows anyway, and one round trip is all the
// sender needs to learn "somebody outranks me and is alive".

// ElectionMsg is the classic ELECTION message.
type ElectionMsg struct {
	From string `json:"from"`
	ID   int    `json:"id"` // Sender's priority, for logs and defensive checks.
}

// ElectionReply is the classic OK.
type ElectionReply struct {
	OK bool `json:"ok"`
}

// VictoryMsg is the classic COORDINATOR/VICTORY announcement.
type VictoryMsg struct {
	From string `json:"from"`
	ID   int    `json:"id"`
}
type VictoryReply struct{}

// State is what a node reports after every event: its role, who it believes
// the coordinator is, and a short log of the latest algorithm events.
type State struct {
	Node          string   `json:"node"`
	ID            int      `json:"id"`          // Bully priority (numeric suffix of the node id).
	Role          string   `json:"role"`        // follower | candidate | coordinator
	Coordinator   string   `json:"coordinator"` // Believed leader; "" until the first election.
	ElectionRound uint64   `json:"electionRound"`
	ElectionsWon  int      `json:"electionsWon"`
	BackedDown    int      `json:"electionsBackedDown"` // Elections abandoned: an OK showed I am outranked, or a victory arrived.
	HigherPeers   []string `json:"higherPeers"`         // Who this node would contact in an election.
	Recent        []string `json:"recentEvents"`
	LastEvent     string   `json:"lastEvent"`
	LastError     string   `json:"lastError"`
}

// ─── Node state ──────────────────────────────────────────────────────────────

// ElectionService is the complete state of one process. mu guards every field.
// There is no special "coordinator code": every node runs the same code, and
// the coordinator is just the node that won the last election.
type ElectionService struct {
	mu            sync.Mutex
	id            string
	priority      int
	members       []string
	peers         map[string]*rpc.Client // Owned by the runtime; never Close them here.
	report        func(any) error
	status        string // follower | candidate | coordinator
	coordinator   string
	electionRound uint64
	won           int
	backedDown    int
	events        []string // Ring buffer of recent events for teaching.
	lastErr       string
}

// Application holds the actions you trigger from the platform UI.
type Application struct{ service *ElectionService }

const (
	statusFollower    = "follower"
	statusCandidate   = "candidate"
	statusCoordinator = "coordinator"
)

// priority turns "node-12" into 12. Comparing the ID STRINGS would be wrong:
// "node-10" sorts before "node-2", so a 10-node cluster would bully in the
// wrong direction. The numeric suffix is the priority.
func priority(id string) int {
	parts := strings.Split(id, "-")
	number, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0
	}
	return number
}

// isMember reports whether id belongs to this experiment's membership.
func (s *ElectionService) isMember(id string) bool {
	for _, member := range s.members {
		if member == id {
			return true
		}
	}
	return false
}

// higherPeersLocked lists the nodes this one would send ELECTION to: every
// member with a strictly higher priority. Callers hold s.mu.
func (s *ElectionService) higherPeersLocked() []string {
	var higher []string
	for _, member := range s.members {
		if priority(member) > s.priority {
			higher = append(higher, member)
		}
	}
	return higher
}

// logLocked records one algorithm event (callers hold s.mu). The last 12 are
// kept so the UI shows how this node reached its current state.
func (s *ElectionService) logLocked(format string, args ...any) {
	entry := fmt.Sprintf("round %d: %s", s.electionRound, fmt.Sprintf(format, args...))
	s.events = append(s.events, entry)
	if len(s.events) > 12 {
		s.events = s.events[len(s.events)-12:]
	}
}

// logEvent is the lock-taking twin of logLocked, for goroutines that are NOT
// holding the state lock.
func (s *ElectionService) logEvent(format string, args ...any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.logLocked(format, args...)
}

// stateLocked builds the reported snapshot. Callers must hold s.mu. Slices are
// copied with make+copy so an empty list is reported as [] and never as null.
func (s *ElectionService) stateLocked() State {
	recent := make([]string, len(s.events))
	copy(recent, s.events)
	last := ""
	if len(recent) > 0 {
		last = recent[len(recent)-1]
	}
	higher := make([]string, 0, len(s.members))
	for _, member := range s.members {
		if priority(member) > s.priority {
			higher = append(higher, member)
		}
	}
	return State{
		Node: s.id, ID: s.priority, Role: s.status, Coordinator: s.coordinator,
		ElectionRound: s.electionRound, ElectionsWon: s.won, BackedDown: s.backedDown,
		HigherPeers: higher, Recent: recent, LastEvent: last, LastError: s.lastErr,
	}
}

// Read reports this node's state without changing anything.
func (a *Application) Read(_ Empty, reply *State) error {
	s := a.service
	s.mu.Lock()
	*reply = s.stateLocked()
	s.mu.Unlock()
	return s.report(*reply)
}

// Empty is a request with no fields at all (StartElection, Read).
type Empty struct{}
