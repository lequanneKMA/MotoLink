package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"motolink/internal/identity"
	"motolink/internal/platform/id"
	"motolink/internal/platform/ratelimit"
	"motolink/internal/testutil/pgtest"
	"motolink/internal/tracking"
	"motolink/internal/trip"
)

type env struct {
	srv  *httptest.Server
	api  *API
	pool *pgxpool.Pool
	svc  *identity.Service
	hits atomic.Int64 // invocations of the demo idempotent handler
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dsn := pgtest.New(t)
	pool := pgtest.Pool(t, dsn)
	signer, err := identity.NewSigner("", "k1", 15*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	h := identity.NewHasher(identity.ArgonParams{TimeCost: 1, MemoryKiB: 8, Threads: 1, SaltLen: 16, KeyLen: 32, MaxConcurr: 2})
	svc := identity.NewService(identity.Deps{DB: pool, Hasher: h, Signer: signer, AccessTTL: 15 * time.Minute,
		RefreshTTL: 24 * time.Hour * 60, Log: slog.New(slog.NewTextHandler(io.Discard, nil))})
	e := &env{pool: pool, svc: svc}
	trips := trip.NewService(pool, nil)
	sessions := tracking.NewService(pool, trips, svc, nil)
	e.api = &API{Identity: svc, Trips: trips, Sessions: sessions, DB: pool, Limiter: ratelimit.NewMemory(), RegisterPerHour: 10000,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Client: DefaultClientConfig()}
	hnd := e.api.Handler().(http.Handler)
	// Mount a demo idempotent endpoint next to the real router for middleware tests.
	mux := http.NewServeMux()
	mux.Handle("/", hnd)
	mux.Handle("POST /v1/_demo/echo", e.api.base(e.api.authed(e.api.idempotent(func(w http.ResponseWriter, r *http.Request, p Principal) error {
		e.hits.Add(1)
		var in struct {
			N    int  `json:"n"`
			Fail bool `json:"fail"`
			Bad  bool `json:"bad"`
		}
		if err := decode(r, &in); err != nil {
			return err
		}
		if in.Fail {
			return io.ErrUnexpectedEOF // -> 500, must not be cached
		}
		if in.Bad {
			return identityConflict()
		}
		writeJSON(w, http.StatusCreated, map[string]any{"n": in.N, "hit": e.hits.Load()})
		return nil
	}))))
	e.srv = httptest.NewServer(mux)
	t.Cleanup(e.srv.Close)
	return e
}

func do(t *testing.T, method, url string, body any, hdr map[string]string) (*http.Response, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		switch b := body.(type) {
		case string:
			rd = strings.NewReader(b)
		default:
			j, _ := json.Marshal(b)
			rd = bytes.NewReader(j)
		}
	}
	req, _ := http.NewRequestWithContext(context.Background(), method, url, rd)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &m)
	return resp, m
}

func regBody(email string) map[string]any {
	return map[string]any{
		"email": email, "password": "s3cret-pass!", "display_name": "Minh",
		"device":   map[string]any{"device_id": id.NewString(), "model": "Redmi", "os_version": "14", "app_version": "0.1", "has_gms": true},
		"consents": []map[string]string{{"kind": "terms", "version": "1"}, {"kind": "privacy_policy", "version": "1"}},
	}
}

func bearer(tok string) map[string]string { return map[string]string{"Authorization": "Bearer " + tok} }

func TestRegisterMeRefreshLogout(t *testing.T) {
	e := newEnv(t)
	resp, m := do(t, "POST", e.srv.URL+"/v1/auth/register", regBody("a@x.vn"), nil)
	if resp.StatusCode != 201 || resp.Header.Get("X-Request-Id") == "" {
		t.Fatalf("register: %d %v", resp.StatusCode, m)
	}
	access, refresh := m["access_token"].(string), m["refresh_token"].(string)

	if resp, _ := do(t, "GET", e.srv.URL+"/v1/me", nil, nil); resp.StatusCode != 401 {
		t.Fatalf("no token must be 401, got %d", resp.StatusCode)
	}
	resp, m = do(t, "GET", e.srv.URL+"/v1/me", nil, bearer(access))
	if resp.StatusCode != 200 || m["user"].(map[string]any)["email"] != "a@x.vn" {
		t.Fatalf("me: %d %v", resp.StatusCode, m)
	}
	if cs := m["consents"].([]any); len(cs) != 2 {
		t.Fatalf("consents: %v", cs)
	}
	resp, m = do(t, "PATCH", e.srv.URL+"/v1/me", map[string]any{"display_name": "Lan"}, bearer(access))
	if resp.StatusCode != 200 || m["user"].(map[string]any)["display_name"] != "Lan" {
		t.Fatalf("patch: %d %v", resp.StatusCode, m)
	}
	resp, m = do(t, "POST", e.srv.URL+"/v1/auth/refresh", map[string]any{"refresh_token": refresh}, nil)
	if resp.StatusCode != 200 || m["refresh_token"] == refresh {
		t.Fatalf("refresh: %d %v", resp.StatusCode, m)
	}
	newAccess := m["access_token"].(string)
	if resp, _ := do(t, "POST", e.srv.URL+"/v1/auth/logout", nil, bearer(newAccess)); resp.StatusCode != 204 {
		t.Fatalf("logout: %d", resp.StatusCode)
	}
	// session revoked => the same, still-unexpired access token is rejected
	resp, m = do(t, "GET", e.srv.URL+"/v1/me", nil, bearer(newAccess))
	if resp.StatusCode != 401 || m["code"] != "session_revoked" {
		t.Fatalf("after logout: %d %v", resp.StatusCode, m)
	}
}

func TestErrorShapeAndValidation(t *testing.T) {
	e := newEnv(t)
	resp, m := do(t, "POST", e.srv.URL+"/v1/auth/register", map[string]any{"email": "bad"}, nil)
	if resp.StatusCode != 422 || m["code"] != "validation_failed" || m["request_id"] == "" || resp.Header.Get("Content-Type") != "application/problem+json; charset=utf-8" {
		t.Fatalf("%d %v", resp.StatusCode, m)
	}
	if errs, _ := m["errors"].([]any); len(errs) == 0 {
		t.Fatal("field errors expected")
	}
	if resp, m := do(t, "POST", e.srv.URL+"/v1/auth/login", "{not json", nil); resp.StatusCode != 400 || m["code"] != "bad_request" {
		t.Fatalf("bad json: %d %v", resp.StatusCode, m)
	}
	if resp, _ := do(t, "POST", e.srv.URL+"/v1/auth/login", map[string]any{"email": "x@y.vn", "password": "wrongwrong"}, nil); resp.StatusCode != 401 {
		t.Fatalf("login unknown: %d", resp.StatusCode)
	}
	big := `{"email":"` + strings.Repeat("a", 2<<20) + `"}`
	if resp, m := do(t, "POST", e.srv.URL+"/v1/auth/login", big, nil); resp.StatusCode != 413 {
		t.Fatalf("too large: %d %v", resp.StatusCode, m)
	}
	if resp, m := do(t, "GET", e.srv.URL+"/v1/config", nil, nil); resp.StatusCode != 200 || m["ws_v_max"] != float64(1) {
		t.Fatalf("config: %d %v", resp.StatusCode, m)
	}
}

func TestExpiredAndForgedTokens(t *testing.T) {
	e := newEnv(t)
	if resp, m := do(t, "GET", e.srv.URL+"/v1/me", nil, bearer("a.b.c")); resp.StatusCode != 401 || m["code"] != "unauthorized" {
		t.Fatalf("forged: %d %v", resp.StatusCode, m)
	}
	short, _ := identity.NewSigner("", "k1", -time.Minute, true) // already expired when issued
	tok, _, _ := short.AccessToken(mustUUID(), mustUUID())
	_ = tok // signed by a different key => unauthorized, not token_expired
	if resp, m := do(t, "GET", e.srv.URL+"/v1/me", nil, bearer(tok)); resp.StatusCode != 401 {
		t.Fatalf("foreign: %d %v", resp.StatusCode, m)
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	do(t, "POST", e.srv.URL+"/v1/auth/register", regBody("r@x.vn"), nil)
	var last *http.Response
	for i := 0; i < 6; i++ {
		last, _ = do(t, "POST", e.srv.URL+"/v1/auth/login", map[string]any{"email": "r@x.vn", "password": "wrong-password"}, nil)
	}
	if last.StatusCode != 429 || last.Header.Get("Retry-After") == "" {
		t.Fatalf("6th attempt must be 429 with Retry-After, got %d", last.StatusCode)
	}
	// the correct password is also blocked in the window (brute-force protection)
	if resp, _ := do(t, "POST", e.srv.URL+"/v1/auth/login", map[string]any{"email": "r@x.vn", "password": "s3cret-pass!"}, nil); resp.StatusCode != 429 {
		t.Fatalf("still limited, got %d", resp.StatusCode)
	}
}

func TestIdempotencyReplayAndConflicts(t *testing.T) {
	e := newEnv(t)
	_, m := do(t, "POST", e.srv.URL+"/v1/auth/register", regBody("i@x.vn"), nil)
	auth := bearer(m["access_token"].(string))
	url := e.srv.URL + "/v1/_demo/echo"
	with := func(key string) map[string]string {
		h := map[string]string{"Idempotency-Key": key}
		for k, v := range auth {
			h[k] = v
		}
		return h
	}

	if resp, _ := do(t, "POST", url, map[string]any{"n": 1}, auth); resp.StatusCode != 400 {
		t.Fatalf("missing key must be 400, got %d", resp.StatusCode)
	}
	r1, b1 := do(t, "POST", url, map[string]any{"n": 1}, with("k-1"))
	if r1.StatusCode != 201 || e.hits.Load() != 1 {
		t.Fatalf("first: %d hits=%d", r1.StatusCode, e.hits.Load())
	}
	// same key, same body (different key order/whitespace is the same canonical request)
	r2, b2 := do(t, "POST", url, `{ "n" : 1 }`, with("k-1"))
	if r2.StatusCode != 201 || r2.Header.Get("Idempotent-Replay") != "true" || e.hits.Load() != 1 || b2["hit"] != b1["hit"] {
		t.Fatalf("replay: %d replay=%q hits=%d %v vs %v", r2.StatusCode, r2.Header.Get("Idempotent-Replay"), e.hits.Load(), b2, b1)
	}
	// same key, different body
	if r, mm := do(t, "POST", url, map[string]any{"n": 2}, with("k-1")); r.StatusCode != 422 || mm["code"] != "idempotency_key_reuse" {
		t.Fatalf("reuse: %d %v", r.StatusCode, mm)
	}
	// 5xx is not cached: the retry executes again
	do(t, "POST", url, map[string]any{"fail": true}, with("k-fail"))
	before := e.hits.Load()
	do(t, "POST", url, map[string]any{"fail": true}, with("k-fail"))
	if e.hits.Load() != before+1 {
		t.Fatal("a 5xx response must not be replayed")
	}
	// deterministic client errors are replayed
	do(t, "POST", url, map[string]any{"bad": true}, with("k-bad"))
	before = e.hits.Load()
	rb, mb := do(t, "POST", url, map[string]any{"bad": true}, with("k-bad"))
	if rb.StatusCode != 409 || rb.Header.Get("Idempotent-Replay") != "true" || e.hits.Load() != before || mb["code"] != "demo_conflict" {
		t.Fatalf("client error replay: %d %v hits=%d/%d", rb.StatusCode, mb, e.hits.Load(), before)
	}
	// keys are scoped per user
	_, m2 := do(t, "POST", e.srv.URL+"/v1/auth/register", regBody("i2@x.vn"), nil)
	other := map[string]string{"Idempotency-Key": "k-1", "Authorization": "Bearer " + m2["access_token"].(string)}
	before = e.hits.Load()
	if r, _ := do(t, "POST", url, map[string]any{"n": 99}, other); r.StatusCode != 201 || e.hits.Load() != before+1 {
		t.Fatalf("other user's same key must execute independently: %d", r.StatusCode)
	}
}

func TestIdempotencyConcurrentDuplicatesExecuteOnce(t *testing.T) {
	e := newEnv(t)
	_, m := do(t, "POST", e.srv.URL+"/v1/auth/register", regBody("c@x.vn"), nil)
	h := map[string]string{"Idempotency-Key": "same", "Authorization": "Bearer " + m["access_token"].(string)}
	const n = 10
	codes := make(chan int, n)
	for i := 0; i < n; i++ {
		go func() {
			r, _ := do(t, "POST", e.srv.URL+"/v1/_demo/echo", map[string]any{"n": 5}, h)
			codes <- r.StatusCode
		}()
	}
	ok := 0
	for i := 0; i < n; i++ {
		c := <-codes
		if c != 201 && c != 409 {
			t.Fatalf("unexpected status %d", c)
		}
		if c == 201 {
			ok++
		}
	}
	if e.hits.Load() != 1 || ok < 1 {
		t.Fatalf("handler must run exactly once (hits=%d, 201s=%d)", e.hits.Load(), ok)
	}
}

// Regression: the access log key "route" is treated as a coordinate key by logx and
// was redacted, hiding which endpoint was called. The access log must stay useful.
func TestAccessLogKeepsEndpointAndNoBodies(t *testing.T) {
	e := newEnv(t)
	var buf bytes.Buffer
	e.api.Log = slog.New(slog.NewJSONHandler(&buf, nil))
	body := regBody("log@x.vn")
	body["password"] = "very-secret-password-123"
	do(t, "POST", e.srv.URL+"/v1/auth/register", body, nil)
	out := buf.String()
	if !strings.Contains(out, `"endpoint":"POST /v1/auth/register"`) {
		t.Fatalf("endpoint missing from access log: %s", out)
	}
	for _, leak := range []string{"very-secret-password-123", "log@x.vn"} {
		if strings.Contains(out, leak) {
			t.Fatalf("access log leaked request content %q: %s", leak, out)
		}
	}
}
