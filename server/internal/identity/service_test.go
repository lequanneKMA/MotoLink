package identity

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"motolink/internal/platform/apperr"
	"motolink/internal/platform/id"
	"motolink/internal/testutil/pgtest"
)

func newSvc(t *testing.T) (*Service, func(string) (string, error)) {
	t.Helper()
	dsn := pgtest.New(t)
	pool := pgtest.Pool(t, dsn)
	signer, err := NewSigner("", "k1", 15*time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	s := NewService(Deps{DB: pool, Hasher: testHasher(), Signer: signer, AccessTTL: 15 * time.Minute, RefreshTTL: 60 * 24 * time.Hour})
	return s, func(q string) (string, error) {
		var out string
		err := pool.QueryRow(context.Background(), q).Scan(&out)
		return out, err
	}
}

func regInput(email string) RegisterInput {
	return RegisterInput{
		Email: email, Password: "s3cret-pass!", DisplayName: "Minh",
		Device:   &DeviceInfo{DeviceID: id.New(), Model: "Redmi", OSVersion: "14", AppVersion: "0.1", HasGMS: true},
		Consents: []ConsentInput{{Kind: "terms", Version: "2026-10"}, {Kind: "privacy_policy", Version: "2026-10"}},
	}
}

func code(t *testing.T, err error) string {
	t.Helper()
	e, ok := apperr.As(err)
	if !ok {
		t.Fatalf("not an apperr: %v", err)
	}
	return e.Code
}

func TestRegisterLoginRoundTrip(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	pair, err := s.Register(ctx, regInput("  Minh@Example.VN "))
	if err != nil {
		t.Fatal(err)
	}
	if pair.User.Email != "minh@example.vn" || pair.AccessToken == "" || pair.RefreshToken == "" || pair.ExpiresIn < 800 {
		t.Fatalf("bad pair: %+v", pair)
	}
	claims, err := s.Signer().Verify(pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if uid, _ := claims.UserID(); uid != pair.User.ID {
		t.Fatal("sub mismatch")
	}
	if _, err := s.Register(ctx, regInput("MINH@example.vn")); code(t, err) != "email_taken" {
		t.Fatalf("want email_taken, got %v", err)
	}
	if _, err := s.Login(ctx, LoginInput{Email: "minh@EXAMPLE.vn", Password: "s3cret-pass!"}); err != nil {
		t.Fatalf("login: %v", err)
	}
	if _, err := s.Login(ctx, LoginInput{Email: "minh@example.vn", Password: "nope-nope"}); code(t, err) != "invalid_credentials" {
		t.Fatal("wrong password must be invalid_credentials")
	}
	if _, err := s.Login(ctx, LoginInput{Email: "ghost@example.vn", Password: "whatever1"}); code(t, err) != "invalid_credentials" {
		t.Fatal("unknown email must be invalid_credentials")
	}
}

func TestRegisterValidation(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	bad := regInput("not-an-email")
	bad.Password = "short"
	bad.DisplayName = "   "
	bad.Consents = nil
	err := func() error { _, e := s.Register(ctx, bad); return e }()
	e, ok := apperr.As(err)
	if !ok || e.Code != "validation_failed" {
		t.Fatalf("want validation_failed, got %v", err)
	}
	have := map[string]bool{}
	for _, f := range e.Fields {
		have[f.Field] = true
	}
	for _, f := range []string{"email", "password", "display_name", "consents"} {
		if !have[f] {
			t.Errorf("missing field error for %s: %+v", f, e.Fields)
		}
	}
	common := regInput("a@b.vn")
	common.Password = "password"
	if _, err := s.Register(ctx, common); code(t, err) != "validation_failed" {
		t.Fatal("common password must be rejected")
	}
}

func TestRefreshRotationAndReuseDetection(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	p0, _ := s.Register(ctx, regInput("a@x.vn"))

	p1, err := s.Refresh(ctx, p0.RefreshToken, "ua")
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if p1.RefreshToken == p0.RefreshToken || p1.AccessToken == "" {
		t.Fatal("refresh must rotate")
	}
	if p1.User != nil {
		t.Fatal("refresh response must not carry user")
	}
	c0, _ := s.Signer().Verify(p0.AccessToken)
	c1, _ := s.Signer().Verify(p1.AccessToken)
	if c0.SID != c1.SID {
		t.Fatal("family/sid must be stable across rotation")
	}
	// Replaying the old token = theft signal: whole family revoked, including the new token.
	if _, err := s.Refresh(ctx, p0.RefreshToken, "ua"); code(t, err) != "refresh_reused" {
		t.Fatalf("want refresh_reused, got %v", err)
	}
	if _, err := s.Refresh(ctx, p1.RefreshToken, "ua"); code(t, err) != "refresh_reused" && code(t, err) != "invalid_refresh" {
		t.Fatalf("new token must be dead after reuse, got %v", err)
	}
	sid, _ := uuid.Parse(c0.SID)
	if active, err := s.SessionActive(ctx, sid); err != nil || active {
		t.Fatalf("session must be inactive: active=%v err=%v", active, err)
	}
	if _, err := s.Refresh(ctx, "garbage", "ua"); code(t, err) != "invalid_refresh" {
		t.Fatal("unknown token must be invalid_refresh")
	}
}

func TestConcurrentRefreshExactlyOneWins(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	p0, _ := s.Register(ctx, regInput("c@x.vn"))
	const n = 8
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, results[i] = s.Refresh(ctx, p0.RefreshToken, "ua") }()
	}
	wg.Wait()
	ok := 0
	for _, e := range results {
		if e == nil {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("exactly one concurrent refresh may succeed, got %d (%v)", ok, results)
	}
}

func TestLogoutAndLogoutAll(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	a, _ := s.Register(ctx, regInput("l@x.vn"))
	b, err := s.Login(ctx, LoginInput{Email: "l@x.vn", Password: "s3cret-pass!"})
	if err != nil {
		t.Fatal(err)
	}
	ca, _ := s.Signer().Verify(a.AccessToken)
	cb, _ := s.Signer().Verify(b.AccessToken)
	sa, _ := uuid.Parse(ca.SID)
	sb, _ := uuid.Parse(cb.SID)
	if err := s.Logout(ctx, sa); err != nil {
		t.Fatal(err)
	}
	if act, _ := s.SessionActive(ctx, sa); act {
		t.Fatal("A must be revoked")
	}
	if act, _ := s.SessionActive(ctx, sb); !act {
		t.Fatal("B must remain active")
	}
	if err := s.LogoutAll(ctx, a.User.ID); err != nil {
		t.Fatal(err)
	}
	if act, _ := s.SessionActive(ctx, sb); act {
		t.Fatal("B must be revoked by logout-all")
	}
	if _, err := s.Refresh(ctx, b.RefreshToken, "ua"); err == nil {
		t.Fatal("refresh after logout-all must fail")
	}
}

func TestMaxTenSessionsPerUser(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	first, _ := s.Register(ctx, regInput("m@x.vn"))
	var last *TokenPair
	for i := 0; i < 11; i++ { // 12 sessions in total
		time.Sleep(2 * time.Millisecond)
		var err error
		if last, err = s.Login(ctx, LoginInput{Email: "m@x.vn", Password: "s3cret-pass!"}); err != nil {
			t.Fatal(err)
		}
	}
	c0, _ := s.Signer().Verify(first.AccessToken)
	s0, _ := uuid.Parse(c0.SID)
	if act, _ := s.SessionActive(ctx, s0); act {
		t.Fatal("oldest session must have been revoked")
	}
	cl, _ := s.Signer().Verify(last.AccessToken)
	sl, _ := uuid.Parse(cl.SID)
	if act, _ := s.SessionActive(ctx, sl); !act {
		t.Fatal("newest session must be active")
	}
}

func TestDeviceMovesBetweenAccountsAndClearsFCM(t *testing.T) {
	s, q := newSvc(t)
	ctx := context.Background()
	in := regInput("d1@x.vn")
	if _, err := s.Register(ctx, in); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(ctx, `UPDATE devices SET fcm_token='tok-1' WHERE id=$1`, in.Device.DeviceID); err != nil {
		t.Fatal(err)
	}
	in2 := regInput("d2@x.vn")
	in2.Device.DeviceID = in.Device.DeviceID // same phone, different account
	if _, err := s.Register(ctx, in2); err != nil {
		t.Fatal(err)
	}
	tok, err := q(`SELECT coalesce(fcm_token,'<null>') FROM devices LIMIT 1`)
	if err != nil || tok != "<null>" {
		t.Fatalf("fcm token must be cleared on owner change, got %q err=%v", tok, err)
	}
}

func TestUpdateProfile(t *testing.T) {
	s, _ := newSvc(t)
	ctx := context.Background()
	p, _ := s.Register(ctx, regInput("p@x.vn"))
	name, color := "Lan", "#1E88E5"
	u, err := s.UpdateProfile(ctx, p.User.ID, ProfilePatch{DisplayName: &name, AvatarColor: &color})
	if err != nil || u.DisplayName != "Lan" || u.AvatarColor != "#1E88E5" || u.Locale != "vi" {
		t.Fatalf("u=%+v err=%v", u, err)
	}
	bad := "red"
	if _, err := s.UpdateProfile(ctx, p.User.ID, ProfilePatch{AvatarColor: &bad}); code(t, err) != "validation_failed" {
		t.Fatal("bad color must fail validation")
	}
}
