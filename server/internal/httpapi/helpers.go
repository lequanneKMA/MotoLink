package httpapi

import (
	"context"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"motolink/internal/platform/apperr"
)

// pathUUID parses a path parameter. A malformed id is a plain 404: the resource cannot exist.
func pathUUID(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(r.PathValue(name))
	if err != nil {
		return uuid.Nil, apperr.NotFound()
	}
	return id, nil
}

func queryInt(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func queryInt64(r *http.Request, name string, def int64) int64 {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	}
	return def
}

// idem wraps an authenticated handler with the Idempotency-Key protocol.
func (a *API) idem(fn authedFunc) http.Handler { return a.authed(a.idempotent(fn)) }

type ctxT = context.Context

func pathErr() error { return apperr.NotFound() }
