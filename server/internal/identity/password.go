package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// ArgonParams are argon2id cost parameters. Defaults are a starting point
// [GIẢ ĐỊNH]; tune by benchmark on the target host (docs/design/11 §2).
type ArgonParams struct {
	TimeCost   uint32
	MemoryKiB  uint32
	Threads    uint8
	SaltLen    uint32
	KeyLen     uint32
	MaxConcurr int // memory-hard hashing: bound concurrent hashes (ARGON2_MAX_CONCURRENCY)
}

func DefaultArgon(maxConcurrent int) ArgonParams {
	return ArgonParams{TimeCost: 2, MemoryKiB: 64 * 1024, Threads: 1, SaltLen: 16, KeyLen: 32, MaxConcurr: maxConcurrent}
}

// Hasher hashes and verifies passwords with a concurrency bound.
type Hasher struct {
	p     ArgonParams
	sem   chan struct{}
	once  sync.Once
	dummy string
}

func NewHasher(p ArgonParams) *Hasher {
	if p.MaxConcurr < 1 {
		p.MaxConcurr = 1
	}
	return &Hasher{p: p, sem: make(chan struct{}, p.MaxConcurr)}
}

func (h *Hasher) acquire(ctx context.Context) (func(), error) {
	select {
	case h.sem <- struct{}{}:
		return func() { <-h.sem }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Hash returns a PHC-format argon2id string.
func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	rel, err := h.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer rel()
	salt := make([]byte, h.p.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.p.TimeCost, h.p.MemoryKiB, h.p.Threads, h.p.KeyLen)
	b64 := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version,
		h.p.MemoryKiB, h.p.TimeCost, h.p.Threads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

var errBadHash = errors.New("identity: malformed password hash")

// Verify checks password against an encoded hash in constant time.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errBadHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errBadHash
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, errBadHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, errBadHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, errBadHash
	}
	rel, err := h.acquire(ctx)
	if err != nil {
		return false, err
	}
	defer rel()
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// DummyVerify burns comparable time when the user does not exist, so login
// latency does not reveal which emails are registered.
func (h *Hasher) DummyVerify(ctx context.Context, password string) {
	h.dummyOnce(ctx)
	_, _ = h.Verify(ctx, password, h.dummy)
}

func (h *Hasher) dummyOnce(ctx context.Context) {
	h.once.Do(func() {
		h.dummy, _ = h.Hash(context.WithoutCancel(ctx), "dummy-password-for-timing")
	})
}
