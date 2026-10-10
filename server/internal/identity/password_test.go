package identity

import (
	"context"
	"strings"
	"testing"
	"time"
)

func testHasher() *Hasher {
	return NewHasher(ArgonParams{TimeCost: 1, MemoryKiB: 8, Threads: 1, SaltLen: 16, KeyLen: 32, MaxConcurr: 2})
}

func TestHashVerifyRoundTrip(t *testing.T) {
	h := testHasher()
	ctx := context.Background()
	enc, err := h.Hash(ctx, "correct horse battery")
	if err != nil || !strings.HasPrefix(enc, "$argon2id$v=19$") {
		t.Fatalf("hash=%q err=%v", enc, err)
	}
	if ok, err := h.Verify(ctx, "correct horse battery", enc); err != nil || !ok {
		t.Fatalf("verify ok=%v err=%v", ok, err)
	}
	if ok, _ := h.Verify(ctx, "wrong", enc); ok {
		t.Fatal("wrong password verified")
	}
	enc2, _ := h.Hash(ctx, "correct horse battery")
	if enc == enc2 {
		t.Fatal("salts must differ")
	}
}

func TestVerifyRejectsMalformed(t *testing.T) {
	h := testHasher()
	for _, bad := range []string{"", "plain", "$argon2i$v=19$m=8,t=1,p=1$AAAA$BBBB", "$argon2id$v=18$m=8,t=1,p=1$AAAA$BBBB"} {
		if ok, err := h.Verify(context.Background(), "x", bad); ok || err == nil {
			t.Errorf("%q: ok=%v err=%v", bad, ok, err)
		}
	}
}

func TestConcurrencyBoundHonoursContext(t *testing.T) {
	h := NewHasher(ArgonParams{TimeCost: 1, MemoryKiB: 8, Threads: 1, SaltLen: 16, KeyLen: 32, MaxConcurr: 1})
	h.sem <- struct{}{} // occupy the only slot
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := h.Hash(ctx, "x"); err == nil {
		t.Fatal("expected context error while semaphore is full")
	}
}
