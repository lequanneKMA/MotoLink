package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"motolink/internal/platform/apperr"
)

const idemStaleAfter = 5 * time.Minute // an in-progress claim older than this is a crashed request

// idempotent makes a POST safe to retry (docs/design/06 §1.3). Same key + same
// request => the first response is replayed; same key + different request => 422;
// concurrent duplicate => 409. 5xx responses are not stored so the client may retry.
func (a *API) idempotent(fn authedFunc) authedFunc {
	return func(w http.ResponseWriter, r *http.Request, p Principal) error {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 128 {
			return apperr.BadRequest("thiếu hoặc sai header Idempotency-Key (1–128 ký tự)")
		}
		for _, c := range key {
			if c < 0x21 || c > 0x7e {
				return apperr.BadRequest("Idempotency-Key chỉ gồm ký tự ASCII in được")
			}
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
		if err != nil {
			return apperr.PayloadTooLarge()
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		hash := requestHash(r.Method, r.URL.Path, body)

		for attempt := 0; attempt < 2; attempt++ {
			tag, err := a.DB.Exec(r.Context(), `
				INSERT INTO idempotency_keys (user_id,key,method,path,request_hash,expires_at)
				VALUES ($1,$2,$3,$4,$5, now() + interval '24 hours') ON CONFLICT DO NOTHING`,
				p.UserID, key, r.Method, r.URL.Path, hash)
			if err != nil {
				return apperr.Internal(err)
			}
			if tag.RowsAffected() == 1 {
				return a.runClaimed(w, r, p, key, fn)
			}
			var (
				prevHash []byte
				status   *int
				resp     []byte
				created  time.Time
				expires  time.Time
			)
			err = a.DB.QueryRow(r.Context(),
				`SELECT request_hash,response_status,response_body,created_at,expires_at FROM idempotency_keys WHERE user_id=$1 AND key=$2`,
				p.UserID, key).Scan(&prevHash, &status, &resp, &created, &expires)
			if errors.Is(err, pgx.ErrNoRows) {
				continue // deleted between statements (failed first attempt): claim again
			}
			if err != nil {
				return apperr.Internal(err)
			}
			now := time.Now()
			if now.After(expires) || (status == nil && now.Sub(created) > idemStaleAfter) {
				if _, err := a.DB.Exec(r.Context(), `DELETE FROM idempotency_keys WHERE user_id=$1 AND key=$2 AND created_at=$3`, p.UserID, key, created); err != nil {
					return apperr.Internal(err)
				}
				continue
			}
			if !bytes.Equal(prevHash, hash) {
				return apperr.IdempotencyKeyReuse()
			}
			if status == nil {
				return apperr.RequestInProgress()
			}
			w.Header().Set("Idempotent-Replay", "true")
			if len(resp) == 0 || string(resp) == "null" {
				w.WriteHeader(*status)
				return nil
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(*status)
			_, _ = w.Write(resp)
			return nil
		}
		return apperr.RequestInProgress()
	}
}

// runClaimed executes the handler against a buffering writer, stores the result, then replies.
func (a *API) runClaimed(w http.ResponseWriter, r *http.Request, p Principal, key string, fn authedFunc) (err error) {
	rec := &bufWriter{header: http.Header{}, status: http.StatusOK}
	released := false
	release := func() {
		// Use a fresh context: the request context may already be cancelled.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
		defer cancel()
		_, _ = a.DB.Exec(ctx, `DELETE FROM idempotency_keys WHERE user_id=$1 AND key=$2 AND response_status IS NULL`, p.UserID, key)
		released = true
	}
	defer func() {
		if rec := recover(); rec != nil {
			release()
			panic(rec)
		}
	}()

	herr := fn(rec, r, p)
	if herr != nil {
		e, ok := apperr.As(herr)
		if !ok || e.Status >= 500 {
			release()
			return herr
		}
		// A deterministic client error is also replayed: render it through the normal path.
		er := &bufWriter{header: http.Header{}, status: http.StatusOK}
		writeError(er, r, herr)
		rec = er
	}
	if rec.status >= 500 {
		release()
		copyOut(w, rec)
		return nil
	}
	var body any
	if rec.buf.Len() > 0 {
		body = json.RawMessage(bytes.TrimSpace(rec.buf.Bytes()))
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Second)
	defer cancel()
	if _, uerr := a.DB.Exec(ctx, `UPDATE idempotency_keys SET response_status=$3, response_body=$4 WHERE user_id=$1 AND key=$2`,
		p.UserID, key, rec.status, body); uerr != nil {
		a.Log.Error("idempotency store failed", "request_id", RequestID(r.Context()), "err", uerr)
		if !released {
			release()
		}
	}
	copyOut(w, rec)
	return nil
}

func requestHash(method, path string, body []byte) []byte {
	canon := body
	var v any
	if len(bytes.TrimSpace(body)) > 0 && json.Unmarshal(body, &v) == nil {
		if b, err := json.Marshal(v); err == nil { // map keys sorted => canonical
			canon = b
		}
	}
	h := sha256.New()
	h.Write([]byte(method))
	h.Write([]byte{'\n'})
	h.Write([]byte(path))
	h.Write([]byte{'\n'})
	h.Write(canon)
	return h.Sum(nil)
}

type bufWriter struct {
	header http.Header
	status int
	buf    bytes.Buffer
}

func (b *bufWriter) Header() http.Header         { return b.header }
func (b *bufWriter) WriteHeader(c int)           { b.status = c }
func (b *bufWriter) Write(p []byte) (int, error) { return b.buf.Write(p) }

func copyOut(w http.ResponseWriter, b *bufWriter) {
	for k, v := range b.header {
		w.Header()[k] = v
	}
	w.WriteHeader(b.status)
	_, _ = w.Write(b.buf.Bytes())
}
