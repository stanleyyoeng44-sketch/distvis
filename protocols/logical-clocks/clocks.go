package main

import "fmt"

// Timestamp labels ONE modeled event with three related representations.
// Lamport is a scalar counter. Vector contains one counter per member, in the
// coordinator's membership order. Total implements the lecture's N*L+i formula,
// where i is the zero-based membership index (not a string sort of node IDs).
type Timestamp struct {
	Lamport uint64   `json:"lamport"`
	Vector  []uint64 `json:"vector"`
	Total   uint64   `json:"total"`
}

// uint64 is an unsigned 64-bit integer. Go would wrap on overflow; JSON numbers
// in a browser also lose precision above 2^53-1. A conservative limit keeps
// both counters and N*L+i exact for DistVis's maximum twelve members.
const maxCounter uint64 = ((1 << 53) - 1 - 11) / 12

// validateVector checks dimensions and values before ANY mutation. [][] or a
// wrong-length vector is not silently padded: its coordinate meaning would be
// ambiguous. This lesson assumes a fixed membership throughout one run.
func validateVector(vector []uint64, size int) error {
	if len(vector) != size {
		return fmt.Errorf("vector has %d entries; membership requires %d", len(vector), size)
	}
	for _, value := range vector {
		if value > maxCounter {
			return fmt.Errorf("vector counter exceeds the exact-display limit")
		}
	}
	return nil
}

// advance is the entire logical-clock algorithm, isolated from RPC and files.
// For local/send: increment the scalar and our own vector entry once.
// For receive: first merge scalar/vector maxima, THEN increment once.
// It returns a new value; current and incoming are never modified.
func advance(current Timestamp, index int, incoming *Timestamp) (Timestamp, error) {
	size := len(current.Vector)
	if size < 2 || size > 12 || index < 0 || index >= size || current.Lamport > maxCounter {
		return Timestamp{}, fmt.Errorf("invalid membership index or Lamport counter")
	}
	if err := validateVector(current.Vector, size); err != nil {
		return Timestamp{}, err
	}
	// Slices share backing arrays when assigned! append to a nil slice makes
	// an independent copy; otherwise a later event would rewrite old messages
	// and history timestamps, destroying the meaning of causality.
	next := Timestamp{Lamport: current.Lamport, Vector: append([]uint64(nil), current.Vector...)}
	if incoming != nil {
		if err := validateVector(incoming.Vector, size); err != nil {
			return Timestamp{}, err
		}
		if incoming.Lamport > maxCounter {
			return Timestamp{}, fmt.Errorf("incoming Lamport counter exceeds limit")
		}
		next.Lamport = max(next.Lamport, incoming.Lamport)
		for i := range next.Vector {
			next.Vector[i] = max(next.Vector[i], incoming.Vector[i])
		}
	}
	if next.Lamport >= maxCounter || next.Vector[index] >= maxCounter {
		return Timestamp{}, fmt.Errorf("counter limit reached; start a fresh experiment")
	}
	next.Lamport++
	next.Vector[index]++
	next.Total = uint64(size)*next.Lamport + uint64(index)
	return next, nil
}

// relation uses the COMPONENTWISE partial order, never lexicographic order.
// A < B means all A[i] <= B[i] and at least one is strictly smaller. If some
// entries are smaller and others larger, neither event caused the other.
// Equality is reported separately: it is not strict happens-before and must
// not be mislabeled as concurrency (e.g., compare an event with itself).
func relation(left, right []uint64) (string, error) {
	if len(left) < 2 || len(left) > 12 {
		return "", fmt.Errorf("vectors must have 2..12 entries")
	}
	if err := validateVector(left, len(left)); err != nil {
		return "", err
	}
	if err := validateVector(right, len(left)); err != nil {
		return "", err
	}
	less, greater := false, false
	for i := range left {
		less = less || left[i] < right[i]
		greater = greater || left[i] > right[i]
	}
	switch {
	case less && greater:
		return "concurrent", nil
	case less:
		return "happens-before", nil
	case greater:
		return "happens-after", nil
	default:
		return "equal", nil
	}
}
