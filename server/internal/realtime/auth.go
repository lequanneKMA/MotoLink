package realtime

import (
	"errors"

	"github.com/google/uuid"

	"motolink/internal/identity"
)

// IdentityAuth validates access tokens with the identity service (signature, expiry, revocation).
type IdentityAuth struct{ Svc *identity.Service }

func (a IdentityAuth) Authenticate(ctx Ctx, token string) (Identity, error) {
	if token == "" {
		return Identity{}, ErrUnauthorized
	}
	claims, err := a.Svc.Signer().Verify(token)
	if err != nil {
		if errors.Is(err, identity.ErrTokenExpired) {
			return Identity{}, ErrTokenExpired
		}
		return Identity{}, ErrUnauthorized
	}
	uid, _ := claims.UserID()
	sid, _ := uuid.Parse(claims.SID)
	active, err := a.Svc.SessionActive(ctx, sid)
	if err != nil || !active {
		return Identity{}, ErrUnauthorized
	}
	return Identity{UserID: uid, SessionID: sid, ExpiresAt: claims.ExpiresAt.Time}, nil
}
