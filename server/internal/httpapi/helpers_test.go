package httpapi

import (
	"github.com/google/uuid"

	"motolink/internal/platform/apperr"
)

func mustUUID() uuid.UUID { return uuid.New() }

func identityConflict() error { return apperr.Conflict("demo_conflict", "demo") }
