package httpapi

import (
	"github.com/google/uuid"
	"time"

	"motolink/internal/identity"

	"motolink/internal/platform/apperr"
)

func mustUUID() uuid.UUID { return uuid.New() }

func identityConflict() error { return apperr.Conflict("demo_conflict", "demo") }

func mustParse(s string) uuid.UUID { return uuid.MustParse(s) }
func identityDev() identity.DeviceInput {
	return identity.DeviceInput{Model: "m", FCMToken: "tok"}
}

func time_zero() time.Time { return time.Time{} }
