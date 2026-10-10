// Package httpapi is the REST layer (docs/design/06): routing, middleware and
// thin handlers. Business rules live in domain packages.
package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"motolink/internal/platform/apperr"
)

const maxBody = 1 << 20 // 1 MiB

type problem struct {
	Type      string              `json:"type"`
	Code      string              `json:"code"`
	Title     string              `json:"title"`
	Status    int                 `json:"status"`
	Detail    string              `json:"detail,omitempty"`
	RequestID string              `json:"request_id,omitempty"`
	Errors    []apperr.FieldError `json:"errors,omitempty"`
	Extra     map[string]any      `json:"extra,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// writeError renders err as problem+json. Non-API errors become a generic 500;
// the cause is logged by the caller, never sent to the client.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	e, ok := apperr.As(err)
	if !ok {
		e = apperr.Internal(err)
	}
	if e.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(e.RetryAfter))
	}
	w.Header().Set("Content-Type", "application/problem+json; charset=utf-8")
	w.WriteHeader(e.Status)
	_ = json.NewEncoder(w).Encode(problem{
		Type: "https://motolink.app/errors/" + e.Code, Code: e.Code, Title: e.Title, Status: e.Status,
		Detail: e.Detail, RequestID: RequestID(r.Context()), Errors: e.Fields, Extra: e.Extra,
	})
}

// decode reads a JSON body (unknown fields are tolerated for forward compatibility).
func decode(r *http.Request, v any) error { return decodeLarge(r, v, maxBody) }

// decodeLarge is decode with a custom body cap (bulk endpoints).
func decodeLarge(r *http.Request, v any, limit int64) error {
	r.Body = http.MaxBytesReader(nil, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			return apperr.PayloadTooLarge()
		case errors.Is(err, io.EOF):
			return apperr.BadRequest("thiếu nội dung JSON")
		default:
			return apperr.BadRequest("JSON không hợp lệ")
		}
	}
	if dec.More() {
		return apperr.BadRequest("JSON thừa nội dung phía sau")
	}
	return nil
}
