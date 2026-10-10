package id

import (
	"testing"
	"time"
)

func TestUUIDv7IsTimeOrdered(t *testing.T) {
	prev := NewString()
	for i := 0; i < 50; i++ {
		time.Sleep(2 * time.Millisecond)
		cur := NewString()
		if !(cur > prev) {
			t.Fatalf("expected %s > %s (lexicographic order must follow creation time)", cur, prev)
		}
		prev = cur
	}
}

func TestUUIDv7Version(t *testing.T) {
	if v := New().Version(); v != 7 {
		t.Fatalf("version = %d, want 7", v)
	}
}
