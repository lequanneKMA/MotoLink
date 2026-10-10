// Package id generates identifiers (ADR-004: UUIDv7, time-ordered).
package id

import "github.com/google/uuid"

// New returns a new UUIDv7. UUIDv7 values sort by creation time, which the
// location ordering rule (session_id, seq) relies on.
func New() uuid.UUID {
	u, err := uuid.NewV7()
	if err != nil {
		// NewV7 only fails when the system random source fails.
		panic("id: uuid v7: " + err.Error())
	}
	return u
}

// NewString returns New() in canonical lowercase text form.
func NewString() string { return New().String() }
