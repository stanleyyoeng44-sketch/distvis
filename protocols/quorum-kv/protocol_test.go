package main

import (
	"errors"
	"testing"
)

func TestVersionOrderAndNoRollback(t *testing.T) {
	s := &Replica{disk: Disk{Values: map[string]Versioned{}}, save: func(any) error { return nil }, report: func(any) error { return nil }}
	high := Versioned{Value: "new", Version: Version{Counter: 2, Writer: "node-1"}}
	low := Versioned{Value: "old", Version: Version{Counter: 1, Writer: "node-2"}}
	for _, v := range []Versioned{high, low, high} {
		if err := s.Store(StoreArgs{Key: "x", Item: v}, &Empty{}); err != nil {
			t.Fatal(err)
		}
	}
	if s.disk.Values["x"] != high {
		t.Fatal("late message rolled value back")
	}
	if !newer(Version{2, "node-2"}, Version{2, "node-1"}) {
		t.Fatal("writer tie break missing")
	}
}
func TestFailedSaveNeverAcknowledges(t *testing.T) {
	s := &Replica{disk: Disk{Values: map[string]Versioned{}}, save: func(any) error { return errors.New("disk") }, report: func(any) error { return nil }}
	args := StoreArgs{Key: "x", Item: Versioned{Value: "1", Version: Version{1, "node-1"}}}
	if s.Store(args, &Empty{}) == nil || s.Store(args, &Empty{}) == nil {
		t.Fatal("acknowledged an undurable value on retry")
	}
	if s.Fetch(KeyArgs{Key: "x"}, &Versioned{}) == nil {
		t.Fatal("served undurable state")
	}
}
