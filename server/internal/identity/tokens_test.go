package identity

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestAccessTokenRoundTripAndExpiry(t *testing.T) {
	s, err := NewSigner("", "k1", 15*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	u, sid := uuid.New(), uuid.New()
	tok, exp, err := s.AccessToken(u, sid)
	if err != nil {
		t.Fatal(err)
	}
	c, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got, _ := c.UserID(); got != u || c.SID != sid.String() {
		t.Fatalf("claims mismatch: %+v", c)
	}
	if d := exp.Sub(time.Now()); d < 14*time.Minute || d > 16*time.Minute {
		t.Fatalf("exp %v not ~15m", d)
	}
	s.now = func() time.Time { return time.Now().Add(16 * time.Minute) }
	if _, err := s.Verify(tok); err != ErrTokenExpired {
		t.Fatalf("want ErrTokenExpired, got %v", err)
	}
}

func TestVerifyRejectsTamperAndForeignKey(t *testing.T) {
	a, _ := NewSigner("", "k1", time.Minute, true)
	b, _ := NewSigner("", "k1", time.Minute, true) // same kid, different key
	tok, _, _ := a.AccessToken(uuid.New(), uuid.New())
	if _, err := b.Verify(tok); err != ErrTokenInvalid {
		t.Fatalf("foreign key must be invalid, got %v", err)
	}
	if _, err := a.Verify(tok + "x"); err == nil {
		t.Fatal("tampered token accepted")
	}
	parts := strings.Split(tok, ".")
	if _, err := a.Verify(parts[0] + "." + parts[1] + "."); err == nil {
		t.Fatal("unsigned token accepted")
	}
}

func TestEphemeralKeyForbiddenInProd(t *testing.T) {
	if _, err := NewSigner("", "k1", time.Minute, false); err == nil {
		t.Fatal("must require a key file when ephemeral not allowed")
	}
}

func TestRefreshTokenHashing(t *testing.T) {
	tok, h, err := NewRefreshToken()
	if err != nil || len(h) != 32 || len(tok) < 40 {
		t.Fatalf("tok=%q hashlen=%d err=%v", tok, len(h), err)
	}
	if string(HashRefresh(tok)) != string(h) {
		t.Fatal("hash mismatch")
	}
	tok2, _, _ := NewRefreshToken()
	if tok == tok2 {
		t.Fatal("tokens must be random")
	}
}
