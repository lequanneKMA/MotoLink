package identity

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const (
	Issuer   = "motolink"
	Audience = "motolink-api"
)

// Signer issues and verifies access tokens (ADR-003: EdDSA, short TTL, `kid` header).
type Signer struct {
	kid  string
	priv ed25519.PrivateKey
	pubs map[string]ed25519.PublicKey // kid -> public key (supports rotation by adding keys)
	ttl  time.Duration
	now  func() time.Time
}

// Claims are the access token claims. No per-trip rights are embedded (docs/design/11 §2).
type Claims struct {
	jwt.RegisteredClaims
	SID string `json:"sid"` // auth session id == refresh token family id
}

// UserID returns the subject.
func (c *Claims) UserID() (uuid.UUID, error) { return uuid.Parse(c.Subject) }

// NewSigner builds a Signer from a PKCS#8 PEM file. If file is empty and allowEphemeral
// is true, a throwaway key is generated (dev/test only; tokens die on restart).
func NewSigner(file, kid string, ttl time.Duration, allowEphemeral bool) (*Signer, error) {
	var priv ed25519.PrivateKey
	switch {
	case file != "":
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read jwt key: %w", err)
		}
		blk, _ := pem.Decode(b)
		if blk == nil {
			return nil, errors.New("jwt key: no PEM block")
		}
		k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
		if err != nil {
			return nil, fmt.Errorf("jwt key: %w", err)
		}
		ek, ok := k.(ed25519.PrivateKey)
		if !ok {
			return nil, errors.New("jwt key: not Ed25519")
		}
		priv = ek
	case allowEphemeral:
		_, k, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			return nil, err
		}
		priv = k
	default:
		return nil, errors.New("JWT_ED25519_PRIVATE_KEY_FILE is required outside dev/test")
	}
	return &Signer{kid: kid, priv: priv, pubs: map[string]ed25519.PublicKey{kid: priv.Public().(ed25519.PublicKey)}, ttl: ttl, now: time.Now}, nil
}

// AccessToken returns a signed token for (userID, sid) and its expiry.
func (s *Signer) AccessToken(userID uuid.UUID, sid uuid.UUID) (string, time.Time, error) {
	now := s.now()
	exp := now.Add(s.ttl)
	jti, _ := uuid.NewV7()
	c := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: Issuer, Subject: userID.String(), Audience: jwt.ClaimStrings{Audience},
			IssuedAt: jwt.NewNumericDate(now), ExpiresAt: jwt.NewNumericDate(exp), ID: jti.String(),
		},
		SID: sid.String(),
	}
	t := jwt.NewWithClaims(jwt.SigningMethodEdDSA, c)
	t.Header["kid"] = s.kid
	str, err := t.SignedString(s.priv)
	return str, exp, err
}

// Errors from Verify.
var (
	ErrTokenExpired = errors.New("identity: token expired")
	ErrTokenInvalid = errors.New("identity: token invalid")
)

// Verify parses and validates a token (signature, exp, iss, aud).
func (s *Signer) Verify(tok string) (*Claims, error) {
	var c Claims
	_, err := jwt.ParseWithClaims(tok, &c, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		pub, ok := s.pubs[kid]
		if !ok {
			return nil, ErrTokenInvalid
		}
		return pub, nil
	}, jwt.WithValidMethods([]string{"EdDSA"}), jwt.WithIssuer(Issuer), jwt.WithAudience(Audience),
		jwt.WithTimeFunc(s.now), jwt.WithExpirationRequired())
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, ErrTokenInvalid
	}
	if _, err := uuid.Parse(c.Subject); err != nil {
		return nil, ErrTokenInvalid
	}
	if _, err := uuid.Parse(c.SID); err != nil {
		return nil, ErrTokenInvalid
	}
	return &c, nil
}

// NewRefreshToken returns an opaque 256-bit token and its SHA-256 hash (only the hash is stored).
func NewRefreshToken() (token string, hash []byte, err error) {
	b := make([]byte, 32)
	if _, err = rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, HashRefresh(token), nil
}

// HashRefresh hashes a presented refresh token.
func HashRefresh(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}
