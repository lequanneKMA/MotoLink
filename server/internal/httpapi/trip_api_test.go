package httpapi

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"motolink/internal/platform/id"
)

// ---- helpers ----

type user struct {
	token string
	id    string
	email string
}

func (e *env) newUser(t *testing.T, name string) user {
	t.Helper()
	email := fmt.Sprintf("%s-%s@x.vn", name, id.NewString()[:8])
	body := regBody(email)
	body["display_name"] = name
	r, m := do(t, "POST", e.srv.URL+"/v1/auth/register", body, nil)
	if r.StatusCode != 201 {
		t.Fatalf("register %s: %d %v", name, r.StatusCode, m)
	}
	return user{token: m["access_token"].(string), id: m["user"].(map[string]any)["id"].(string), email: email}
}

// call performs an authenticated request; mutating requests get a fresh Idempotency-Key.
func (e *env) call(t *testing.T, u user, method, path string, body any) (int, map[string]any) {
	t.Helper()
	h := map[string]string{"Authorization": "Bearer " + u.token}
	if method == "POST" || method == "DELETE" {
		h["Idempotency-Key"] = id.NewString()
	}
	r, m := do(t, method, e.srv.URL+path, body, h)
	return r.StatusCode, m
}

func (e *env) mustCall(t *testing.T, u user, method, path string, body any, want int) map[string]any {
	t.Helper()
	code, m := e.call(t, u, method, path, body)
	if code != want {
		t.Fatalf("%s %s: got %d want %d: %v", method, path, code, want, m)
	}
	return m
}

func (e *env) consent(t *testing.T, u user) {
	t.Helper()
	e.mustCall(t, u, "PUT", "/v1/me/consents/location_processing", map[string]any{"version": "1", "granted": true}, 204)
}

// newTrip creates a trip owned by owner; if start is true it is started.
func (e *env) newTrip(t *testing.T, owner user, start bool) string {
	t.Helper()
	m := e.mustCall(t, owner, "POST", "/v1/trips", map[string]any{"name": "Đà Lạt"}, 201)
	tid := m["trip"].(map[string]any)["id"].(string)
	if start {
		e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/start", nil, 200)
	}
	return tid
}

func (e *env) invite(t *testing.T, owner user, tid string, body any) string {
	t.Helper()
	m := e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/invitations", body, 201)
	return m["code"].(string)
}

func (e *env) join(t *testing.T, u user, code string) {
	t.Helper()
	e.mustCall(t, u, "POST", "/v1/trip-join", map[string]any{"code": code}, 200)
}

// ---- tests ----

func TestTripLifecycleAndEventsAreGapFree(t *testing.T) {
	e := newEnv(t)
	owner, bob := e.newUser(t, "owner"), e.newUser(t, "bob")
	tid := e.newTrip(t, owner, false)

	// draft -> cannot end twice, cannot archive
	e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/archive", nil, 409)
	code := e.invite(t, owner, tid, nil)
	e.join(t, bob, code)
	e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/start", nil, 200)
	e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/start", nil, 409) // already active (fresh IK)
	e.mustCall(t, bob, "POST", "/v1/trips/"+tid+"/end", nil, 403)     // only the owner
	e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/end", nil, 200)
	e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/archive", nil, 200)
	e.mustCall(t, owner, "PATCH", "/v1/trips/"+tid, map[string]any{"name": "x"}, 409) // archived is frozen

	m := e.mustCall(t, bob, "GET", "/v1/trips/"+tid+"/events?after_seq=0", nil, 200)
	items := m["items"].([]any)
	var kinds []string
	for i, it := range items {
		ev := it.(map[string]any)
		if int(ev["event_seq"].(float64)) != i+1 {
			t.Fatalf("event_seq must be 1..n without gaps, got %v at %d", ev["event_seq"], i)
		}
		kinds = append(kinds, ev["kind"].(string))
	}
	want := []string{"trip_created", "member_joined", "trip_started", "trip_ended", "trip_archived"}
	if fmt.Sprint(kinds) != fmt.Sprint(want) || m["complete"] != true || int(m["head_event_seq"].(float64)) != 5 {
		t.Fatalf("kinds=%v want=%v meta=%v", kinds, want, m)
	}
	// catch-up from a cursor
	m = e.mustCall(t, bob, "GET", "/v1/trips/"+tid+"/events?after_seq=3", nil, 200)
	if got := len(m["items"].([]any)); got != 2 {
		t.Fatalf("after_seq=3 should return 2 events, got %d", got)
	}
	// every durable event also produced exactly one outbox row for the relay
	var n int
	if err := e.pool.QueryRow(t.Context(), `SELECT count(*) FROM outbox WHERE msg_key=$1`, tid).Scan(&n); err != nil || n != 5 {
		t.Fatalf("outbox rows = %d err=%v", n, err)
	}
}

func TestEventsExpiredWhenRetentionPassed(t *testing.T) {
	e := newEnv(t)
	owner := e.newUser(t, "o")
	tid := e.newTrip(t, owner, true)
	if _, err := e.pool.Exec(t.Context(), `UPDATE trip_events SET created_at = now() - interval '8 days' WHERE trip_id=$1 AND seq=1`, tid); err != nil {
		t.Fatal(err)
	}
	if c, m := e.call(t, owner, "GET", "/v1/trips/"+tid+"/events?after_seq=0", nil); c != 410 || m["code"] != "events_expired" {
		t.Fatalf("want 410 events_expired, got %d %v", c, m)
	}
	e.mustCall(t, owner, "GET", "/v1/trips/"+tid+"/events?after_seq=1", nil, 200)
}

func TestInvitationRules(t *testing.T) {
	e := newEnv(t)
	owner, a, b, c := e.newUser(t, "o"), e.newUser(t, "a"), e.newUser(t, "b"), e.newUser(t, "c")
	tid := e.newTrip(t, owner, true)

	if code, _ := e.call(t, a, "POST", "/v1/trips/"+tid+"/invitations", nil); code != 404 {
		t.Fatalf("non-member creating invitation must see 404, got %d", code)
	}
	code := e.invite(t, owner, tid, map[string]any{"max_uses": 2})
	// resolve is case/separator/confusable tolerant and reveals only what a joiner needs
	pv := e.mustCall(t, a, "POST", "/v1/invitations/resolve", map[string]any{"code": " " + lower(code) + " "}, 200)
	if pv["trip_name"] != "Đà Lạt" || pv["valid"] != true {
		t.Fatalf("preview: %v", pv)
	}
	if c2, _ := e.call(t, a, "POST", "/v1/invitations/resolve", map[string]any{"code": "ZZZZ-ZZZZ"}); c2 != 404 {
		t.Fatalf("unknown code => 404, got %d", c2)
	}
	e.join(t, a, code)
	e.join(t, a, code) // idempotent for active members: consumes nothing
	e.join(t, b, code)
	// max_uses=2 reached
	if c2, m := e.call(t, c, "POST", "/v1/trip-join", map[string]any{"code": code}); c2 != 410 || m["code"] != "exhausted" {
		t.Fatalf("want 410 exhausted, got %d %v", c2, m)
	}
	// revoked => 404 (does not reveal the code once existed)
	code2 := e.invite(t, owner, tid, nil)
	list := e.mustCall(t, owner, "GET", "/v1/trips/"+tid+"/invitations", nil, 200)["items"].([]any)
	var invID string
	for _, it := range list {
		if it.(map[string]any)["used_count"].(float64) == 0 {
			invID = it.(map[string]any)["id"].(string)
		}
	}
	e.mustCall(t, owner, "DELETE", "/v1/trips/"+tid+"/invitations/"+invID, nil, 204)
	if c2, _ := e.call(t, c, "POST", "/v1/trip-join", map[string]any{"code": code2}); c2 != 404 {
		t.Fatalf("revoked => 404, got %d", c2)
	}
	// expired
	code3 := e.invite(t, owner, tid, nil)
	if _, err := e.pool.Exec(t.Context(), `UPDATE trip_invitations SET expires_at = now() - interval '1 minute' WHERE trip_id=$1`, tid); err != nil {
		t.Fatal(err)
	}
	if c2, m := e.call(t, c, "POST", "/v1/trip-join", map[string]any{"code": code3}); c2 != 410 || m["code"] != "expired" {
		t.Fatalf("want 410 expired, got %d %v", c2, m)
	}
	// the stored value is only a hash, never the code
	var n int
	_ = e.pool.QueryRow(t.Context(), `SELECT count(*) FROM trip_invitations WHERE encode(code_hash,'escape') ILIKE '%'||$1||'%'`, code[:4]).Scan(&n)
	if n != 0 {
		t.Fatal("plain code must not be stored")
	}
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func TestJoinNeverExceedsMaxUsesOrMembersUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	owner := e.newUser(t, "o")
	tid := e.newTrip(t, owner, true)
	code := e.invite(t, owner, tid, map[string]any{"max_uses": 3})
	const n = 10
	users := make([]user, n)
	for i := range users {
		users[i] = e.newUser(t, fmt.Sprintf("u%d", i))
	}
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := range users {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes[i], _ = e.call(t, users[i], "POST", "/v1/trip-join", map[string]any{"code": code})
		}()
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		if c == 200 {
			ok++
		}
	}
	if ok != 3 {
		t.Fatalf("exactly max_uses=3 joins may succeed, got %d (%v)", ok, codes)
	}
	var members int
	_ = e.pool.QueryRow(t.Context(), `SELECT count(*) FROM trip_members WHERE trip_id=$1 AND status='active'`, tid).Scan(&members)
	if members != 4 {
		t.Fatalf("owner + 3 members expected, got %d", members)
	}
}

func TestTripFullAndRejoinRules(t *testing.T) {
	e := newEnv(t)
	owner, a, b := e.newUser(t, "o"), e.newUser(t, "a"), e.newUser(t, "b")
	tid := e.newTrip(t, owner, true)
	if _, err := e.pool.Exec(t.Context(), `UPDATE trips SET max_members=2 WHERE id=$1`, tid); err != nil {
		t.Fatal(err)
	}
	code := e.invite(t, owner, tid, map[string]any{"max_uses": 10})
	e.join(t, a, code)
	if c, m := e.call(t, b, "POST", "/v1/trip-join", map[string]any{"code": code}); c != 409 || m["code"] != "trip_full" {
		t.Fatalf("want trip_full, got %d %v", c, m)
	}
	// a leaves, b joins, a rejoins is refused (full) then possible after b leaves
	e.mustCall(t, a, "DELETE", "/v1/trips/"+tid+"/members/me", nil, 204)
	e.join(t, b, code)
	if c, _ := e.call(t, a, "POST", "/v1/trip-join", map[string]any{"code": code}); c != 409 {
		t.Fatalf("a cannot rejoin a full trip, got %d", c)
	}
	e.mustCall(t, b, "DELETE", "/v1/trips/"+tid+"/members/me", nil, 204)
	e.join(t, a, code) // rejoin after leaving is allowed
	// removed members can never come back with a code
	e.mustCall(t, owner, "DELETE", "/v1/trips/"+tid+"/members/"+a.id, nil, 204)
	if c, m := e.call(t, a, "POST", "/v1/trip-join", map[string]any{"code": code}); c != 409 || m["code"] != "removed" {
		t.Fatalf("removed must not rejoin, got %d %v", c, m)
	}
	// ... and see nothing of the trip any more
	for _, path := range []string{"/v1/trips/" + tid, "/v1/trips/" + tid + "/members", "/v1/trips/" + tid + "/snapshot", "/v1/trips/" + tid + "/events"} {
		if c, _ := e.call(t, a, "GET", path, nil); c != 404 {
			t.Fatalf("removed member GET %s => %d, want 404", path, c)
		}
	}
}

func TestOwnerLeaveTransferAndMemberSettings(t *testing.T) {
	e := newEnv(t)
	owner, a := e.newUser(t, "o"), e.newUser(t, "a")
	tid := e.newTrip(t, owner, true)
	e.join(t, a, e.invite(t, owner, tid, nil))

	if c, m := e.call(t, owner, "DELETE", "/v1/trips/"+tid+"/members/me", nil); c != 409 || m["code"] != "owner_must_transfer" {
		t.Fatalf("owner must transfer first: %d %v", c, m)
	}
	if c, _ := e.call(t, a, "POST", "/v1/trips/"+tid+"/transfer-ownership", map[string]any{"user_id": a.id}); c != 403 {
		t.Fatalf("member cannot transfer: %d", c)
	}
	e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/transfer-ownership", map[string]any{"user_id": a.id}, 200)
	// roles swapped: old owner is a plain member now
	if c, _ := e.call(t, owner, "POST", "/v1/trips/"+tid+"/end", nil); c != 403 {
		t.Fatalf("old owner lost rights: %d", c)
	}
	e.mustCall(t, a, "PATCH", "/v1/trips/"+tid, map[string]any{"name": "Phượt Hà Giang"}, 200)
	// member settings
	m := e.mustCall(t, owner, "PATCH", "/v1/trips/"+tid+"/members/me", map[string]any{"sharing_state": "paused", "share_history": false}, 200)
	mem := m["membership"].(map[string]any)
	if mem["sharing_state"] != "paused" || mem["share_history"] != false {
		t.Fatalf("%v", mem)
	}
	if c, _ := e.call(t, owner, "PATCH", "/v1/trips/"+tid+"/members/me", map[string]any{"sharing_state": "bogus"}); c != 422 {
		t.Fatalf("invalid sharing_state => 422, got %d", c)
	}
	// the new owner is alone after the old one leaves: leaving then closes the trip
	e.mustCall(t, owner, "DELETE", "/v1/trips/"+tid+"/members/me", nil, 204)
	e.mustCall(t, a, "DELETE", "/v1/trips/"+tid+"/members/me", nil, 204)
	var status string
	_ = e.pool.QueryRow(t.Context(), `SELECT status FROM trips WHERE id=$1`, tid).Scan(&status)
	if status != "ended" {
		t.Fatalf("sole owner leaving must end the trip, status=%s", status)
	}
}

func TestIdempotentCreateTripDoesNotDuplicate(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t, "o")
	h := map[string]string{"Authorization": "Bearer " + u.token, "Idempotency-Key": "create-1"}
	r1, m1 := do(t, "POST", e.srv.URL+"/v1/trips", map[string]any{"name": "A"}, h)
	r2, m2 := do(t, "POST", e.srv.URL+"/v1/trips", map[string]any{"name": "A"}, h)
	if r1.StatusCode != 201 || r2.StatusCode != 201 || r2.Header.Get("Idempotent-Replay") != "true" {
		t.Fatalf("%d %d replay=%q", r1.StatusCode, r2.StatusCode, r2.Header.Get("Idempotent-Replay"))
	}
	if m1["trip"].(map[string]any)["id"] != m2["trip"].(map[string]any)["id"] {
		t.Fatal("replay must return the same trip")
	}
	var n int
	_ = e.pool.QueryRow(t.Context(), `SELECT count(*) FROM trips WHERE owner_id=$1`, u.id).Scan(&n)
	if n != 1 {
		t.Fatalf("one trip expected, got %d", n)
	}
}

func TestTrackingSessionLifecycleAndRules(t *testing.T) {
	e := newEnv(t)
	owner, a, outsider := e.newUser(t, "o"), e.newUser(t, "a"), e.newUser(t, "x")
	tid := e.newTrip(t, owner, false)
	e.join(t, a, e.invite(t, owner, tid, nil))

	// trip not started
	e.consent(t, a)
	if c, m := e.call(t, a, "POST", "/v1/trips/"+tid+"/tracking-sessions", nil); c != 409 || m["code"] != "trip_not_active" {
		t.Fatalf("%d %v", c, m)
	}
	e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/start", nil, 200)
	// no consent => 403
	if c, m := e.call(t, owner, "POST", "/v1/trips/"+tid+"/tracking-sessions", nil); c != 403 || m["code"] != "consent_required" {
		t.Fatalf("%d %v", c, m)
	}
	// non-member => 404
	e.consent(t, outsider)
	if c, _ := e.call(t, outsider, "POST", "/v1/trips/"+tid+"/tracking-sessions", nil); c != 404 {
		t.Fatalf("outsider: %d", c)
	}
	m := e.mustCall(t, a, "POST", "/v1/trips/"+tid+"/tracking-sessions", map[string]any{}, 201)
	sid := m["session"].(map[string]any)["id"].(string)
	// second open session for same (user, trip): 409 with the existing one attached for resume
	c, m := e.call(t, a, "POST", "/v1/trips/"+tid+"/tracking-sessions", nil)
	if c != 409 || m["code"] != "session_already_open" || m["extra"].(map[string]any)["session"].(map[string]any)["id"] != sid {
		t.Fatalf("%d %v", c, m)
	}
	// only the owner of a session may change it
	if c, _ := e.call(t, owner, "POST", "/v1/tracking-sessions/"+sid+"/pause", nil); c != 404 {
		t.Fatalf("others get 404, got %d", c)
	}
	e.mustCall(t, a, "POST", "/v1/tracking-sessions/"+sid+"/pause", nil, 200)
	if c, _ := e.call(t, a, "POST", "/v1/tracking-sessions/"+sid+"/pause", nil); c != 409 {
		t.Fatalf("pause twice => 409, got %d", c)
	}
	e.mustCall(t, a, "POST", "/v1/tracking-sessions/"+sid+"/resume", nil, 200)

	// visibility: another member sees the session while history is shared, not after the owner opts out
	e.mustCall(t, owner, "GET", "/v1/tracking-sessions/"+sid, nil, 200)
	e.mustCall(t, a, "PATCH", "/v1/trips/"+tid+"/members/me", map[string]any{"share_history": false}, 200)
	if c, _ := e.call(t, owner, "GET", "/v1/tracking-sessions/"+sid, nil); c != 404 {
		t.Fatalf("hidden history => 404 for others, got %d", c)
	}
	e.mustCall(t, a, "GET", "/v1/tracking-sessions/"+sid, nil, 200)

	// ending the trip closes the session with reason trip_ended
	e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/end", nil, 200)
	got := e.mustCall(t, a, "GET", "/v1/tracking-sessions/"+sid, nil, 200)["session"].(map[string]any)
	if got["status"] != "ended" || got["end_reason"] != "trip_ended" {
		t.Fatalf("%v", got)
	}
	// withdrawing location consent ends open sessions too (new trip/session)
}

func TestWithdrawingLocationConsentEndsSessions(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t, "o")
	tid := e.newTrip(t, u, true)
	e.consent(t, u)
	sid := e.mustCall(t, u, "POST", "/v1/trips/"+tid+"/tracking-sessions", nil, 201)["session"].(map[string]any)["id"].(string)
	e.mustCall(t, u, "PUT", "/v1/me/consents/location_processing", map[string]any{"granted": false}, 204)
	got := e.mustCall(t, u, "GET", "/v1/tracking-sessions/"+sid, nil, 200)["session"].(map[string]any)
	if got["status"] != "ended" {
		t.Fatalf("session must end when consent is withdrawn: %v", got)
	}
	if c, m := e.call(t, u, "POST", "/v1/trips/"+tid+"/tracking-sessions", nil); c != 403 {
		t.Fatalf("no new session without consent: %d %v", c, m)
	}
}

func TestSnapshotHidesPausedMembersAndBootstrapListsTrips(t *testing.T) {
	e := newEnv(t)
	owner, a := e.newUser(t, "o"), e.newUser(t, "a")
	tid := e.newTrip(t, owner, true)
	e.join(t, a, e.invite(t, owner, tid, nil))
	e.mustCall(t, a, "PATCH", "/v1/trips/"+tid+"/members/me", map[string]any{"sharing_state": "paused"}, 200)
	snap := e.mustCall(t, owner, "GET", "/v1/trips/"+tid+"/snapshot", nil, 200)
	mem := snap["members"].([]any)
	if len(mem) != 2 {
		t.Fatalf("members: %v", mem)
	}
	for _, x := range mem {
		mm := x.(map[string]any)
		if mm["user_id"] == a.id && (mm["sharing_state"] != "paused" || mm["location"] != nil) {
			t.Fatalf("paused member must expose no location: %v", mm)
		}
	}
	if snap["head_event_seq"].(float64) < 4 {
		t.Fatalf("head_event_seq: %v", snap["head_event_seq"])
	}
	bs := e.mustCall(t, owner, "GET", "/v1/sync/bootstrap", nil, 200)
	if len(bs["trips"].([]any)) != 1 || bs["server_time"].(float64) < float64(time.Now().Add(-time.Minute).UnixMilli()) {
		t.Fatalf("%v", bs)
	}
}

// Authorization matrix (docs/design/06 §3, 14 §1): every trip-scoped endpoint x caller role.
func TestAuthorizationMatrix(t *testing.T) {
	e := newEnv(t)
	owner, member, outsider, removed := e.newUser(t, "o"), e.newUser(t, "m"), e.newUser(t, "x"), e.newUser(t, "r")
	tid := e.newTrip(t, owner, true)
	code := e.invite(t, owner, tid, nil)
	e.join(t, member, code)
	e.join(t, removed, code)
	e.mustCall(t, owner, "DELETE", "/v1/trips/"+tid+"/members/"+removed.id, nil, 204)
	e.consent(t, owner)
	sid := e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/tracking-sessions", nil, 201)["session"].(map[string]any)["id"].(string)

	T := "/v1/trips/" + tid
	cases := []struct {
		method, path string
		body         any
		// expected status per role: owner, member, outsider, removed
		owner, member, outsider, removed int
	}{
		{"GET", T, nil, 200, 200, 404, 404},
		{"PATCH", T, map[string]any{"name": "z"}, 200, 403, 404, 404},
		{"GET", T + "/snapshot", nil, 200, 200, 404, 404},
		{"GET", T + "/events", nil, 200, 200, 404, 404},
		{"GET", T + "/members", nil, 200, 200, 404, 404},
		{"GET", T + "/invitations", nil, 200, 403, 404, 404},
		{"POST", T + "/invitations", nil, 201, 403, 404, 404},
		{"PATCH", T + "/members/me", map[string]any{"color": "#112233"}, 200, 200, 404, 404},
		{"GET", T + "/tracking-sessions", nil, 200, 200, 404, 404},
		{"GET", "/v1/tracking-sessions/" + sid, nil, 200, 200, 404, 404},
		{"GET", "/v1/tracking-sessions/" + sid + "/route", nil, 200, 200, 404, 404},
		{"POST", T + "/transfer-ownership", map[string]any{"user_id": member.id}, 200, 403, 404, 404},
	}
	roles := []struct {
		name string
		u    user
		want func(i int) int
	}{
		{"owner", owner, func(i int) int { return cases[i].owner }},
		{"member", member, func(i int) int { return cases[i].member }},
		{"outsider", outsider, func(i int) int { return cases[i].outsider }},
		{"removed", removed, func(i int) int { return cases[i].removed }},
	}
	// the transfer case changes who the owner is, so it must run last and only once
	for i, c := range cases {
		if c.method == "POST" && c.path == T+"/transfer-ownership" {
			continue
		}
		for _, r := range roles {
			got, m := e.call(t, r.u, c.method, c.path, c.body)
			if got != r.want(i) {
				t.Errorf("%-8s %-6s %s => %d, want %d (%v)", r.name, c.method, c.path, got, r.want(i), m)
			}
		}
	}
	for _, r := range roles[1:] {
		if got, _ := e.call(t, r.u, "POST", T+"/transfer-ownership", map[string]any{"user_id": member.id}); got != cases[len(cases)-1].member && r.name == "member" {
			t.Errorf("member transfer => %d", got)
		}
	}
	// unauthenticated calls never reach trip data
	for _, path := range []string{T, T + "/snapshot", T + "/members"} {
		if r, _ := do(t, "GET", e.srv.URL+path, nil, nil); r.StatusCode != http.StatusUnauthorized {
			t.Errorf("unauthenticated GET %s => %d", path, r.StatusCode)
		}
	}
}

func TestAccountDeletionAndExport(t *testing.T) {
	e := newEnv(t)
	owner, a := e.newUser(t, "o"), e.newUser(t, "a")
	tid := e.newTrip(t, owner, true)
	e.join(t, a, e.invite(t, owner, tid, nil))
	e.consent(t, a)
	e.mustCall(t, a, "POST", "/v1/trips/"+tid+"/tracking-sessions", nil, 201)

	// export contains the caller's own data
	exp := e.mustCall(t, a, "GET", "/v1/me/export", nil, 200)
	if exp["user"].(map[string]any)["email"] != a.email || len(exp["sessions"].([]any)) != 1 {
		t.Fatalf("export: %v", exp)
	}
	// wrong password
	if c, _ := e.call(t, a, "DELETE", "/v1/me", map[string]any{"password": "wrong-password"}); c != 401 {
		t.Fatalf("wrong password => 401, got %d", c)
	}
	// owner with other members must transfer first
	if c, m := e.call(t, owner, "DELETE", "/v1/me", map[string]any{"password": "s3cret-pass!"}); c != 409 || m["code"] != "owner_must_transfer" {
		t.Fatalf("%d %v", c, m)
	}
	e.mustCall(t, a, "DELETE", "/v1/me", map[string]any{"password": "s3cret-pass!"}, 202)
	// the old token is dead, the e-mail is free, personal rows are gone
	if c, _ := e.call(t, a, "GET", "/v1/me", nil); c != 401 {
		t.Fatalf("deleted user's token must stop working, got %d", c)
	}
	var email string
	var sessions, devices int
	_ = e.pool.QueryRow(t.Context(), `SELECT email FROM users WHERE id=$1`, a.id).Scan(&email)
	_ = e.pool.QueryRow(t.Context(), `SELECT count(*) FROM devices WHERE user_id=$1`, a.id).Scan(&devices)
	_ = e.pool.QueryRow(t.Context(), `SELECT count(*) FROM tracking_sessions WHERE user_id=$1 AND status<>'ended'`, a.id).Scan(&sessions)
	if email == a.email || devices != 0 || sessions != 0 {
		t.Fatalf("email=%s devices=%d open sessions=%d", email, devices, sessions)
	}
	r, _ := do(t, "POST", e.srv.URL+"/v1/auth/register", regBodyWith(a.email), nil)
	if r.StatusCode != 201 {
		t.Fatalf("e-mail should be reusable after deletion, got %d", r.StatusCode)
	}
}

func regBodyWith(email string) map[string]any { return regBody(email) }

func TestDeviceRegistrationDoesNotStealOtherUsersDevice(t *testing.T) {
	e := newEnv(t)
	a, b := e.newUser(t, "a"), e.newUser(t, "b")
	dev := id.NewString()
	e.mustCall(t, a, "PUT", "/v1/devices/"+dev, map[string]any{"model": "Redmi", "fcm_token": "tok-A"}, 204)
	if c, m := e.call(t, b, "PUT", "/v1/devices/"+dev, map[string]any{"model": "Evil", "fcm_token": "tok-B"}); c != 409 {
		t.Fatalf("takeover must be refused, got %d %v", c, m)
	}
	var tok string
	_ = e.pool.QueryRow(t.Context(), `SELECT fcm_token FROM devices WHERE id=$1`, dev).Scan(&tok)
	if tok != "tok-A" {
		t.Fatalf("token changed: %q", tok)
	}
	// an FCM token follows the physical device: re-registering it on another device row clears the old one
	dev2 := id.NewString()
	e.mustCall(t, a, "PUT", "/v1/devices/"+dev2, map[string]any{"model": "Pixel", "fcm_token": "tok-A"}, 204)
	var n int
	_ = e.pool.QueryRow(t.Context(), `SELECT count(*) FROM devices WHERE fcm_token='tok-A'`).Scan(&n)
	if n != 1 {
		t.Fatalf("a token must exist on exactly one device, got %d", n)
	}
	e.mustCall(t, a, "DELETE", "/v1/devices/"+dev2, nil, 204)
}
