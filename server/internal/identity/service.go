// Package identity implements accounts, sessions and tokens (ADR-003).
package identity

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"motolink/internal/platform/apperr"
	"motolink/internal/platform/id"
)

const maxFamiliesPerUser = 10 // docs/design/11 §2

// Service is the identity use-case layer.
type Service struct {
	db         *pgxpool.Pool
	hasher     *Hasher
	signer     *Signer
	revoker    Revoker
	accessTTL  time.Duration
	refreshTTL time.Duration
	log        *slog.Logger
	now        func() time.Time
}

type Deps struct {
	DB         *pgxpool.Pool
	Hasher     *Hasher
	Signer     *Signer
	Revoker    Revoker
	AccessTTL  time.Duration
	RefreshTTL time.Duration
	Log        *slog.Logger
}

func NewService(d Deps) *Service {
	if d.Revoker == nil {
		d.Revoker = NoopRevoker{}
	}
	if d.Log == nil {
		d.Log = slog.Default()
	}
	return &Service{db: d.DB, hasher: d.Hasher, signer: d.Signer, revoker: d.Revoker,
		accessTTL: d.AccessTTL, refreshTTL: d.RefreshTTL, log: d.Log, now: time.Now}
}

// Signer exposes the token verifier to the HTTP layer.
func (s *Service) Signer() *Signer { return s.signer }

// Register creates an account, records consents, binds the device and starts a session (T1).
func (s *Service) Register(ctx context.Context, in RegisterInput) (*TokenPair, error) {
	if err := validateRegister(&in); err != nil {
		return nil, err
	}
	hash, err := s.hasher.Hash(ctx, in.Password)
	if err != nil {
		return nil, apperr.Internal(err)
	}
	u := &User{ID: id.New(), Email: in.Email, DisplayName: in.DisplayName, AvatarColor: "#2E7D32", Locale: "vi"}

	var pair *TokenPair
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`INSERT INTO users (id,email,password_hash,display_name) VALUES ($1,$2,$3,$4) RETURNING created_at, avatar_color, locale`,
			u.ID, u.Email, hash, u.DisplayName).Scan(&u.CreatedAt, &u.AvatarColor, &u.Locale)
		if err != nil {
			return err
		}
		for _, c := range in.Consents {
			if _, err := tx.Exec(ctx, `INSERT INTO consent_records (id,user_id,kind,version) VALUES ($1,$2,$3,$4)`,
				id.New(), u.ID, c.Kind, c.Version); err != nil {
				return err
			}
		}
		if err := upsertDevice(ctx, tx, u.ID, in.Device); err != nil {
			return err
		}
		pair, err = s.startSession(ctx, tx, u, deviceID(in.Device), in.UserAgent)
		return err
	})
	if err != nil {
		if isUnique(err, "users_email_uk") {
			return nil, apperr.EmailTaken()
		}
		return nil, wrap(err)
	}
	return pair, nil
}

// Login verifies credentials and starts a session. Unknown email and wrong
// password are indistinguishable (error and timing).
func (s *Service) Login(ctx context.Context, in LoginInput) (*TokenPair, error) {
	email := normalizeEmail(in.Email)
	if !validEmail(email) || in.Password == "" || len(in.Password) > 256 {
		s.hasher.DummyVerify(ctx, in.Password)
		return nil, apperr.InvalidCredentials()
	}
	if f := validateDevice(in.Device); len(f) > 0 {
		return nil, apperr.Validation(f...)
	}
	u := &User{}
	var hash *string
	err := s.db.QueryRow(ctx,
		`SELECT id,email,display_name,avatar_color,locale,created_at,password_hash FROM users WHERE lower(email)=$1 AND deleted_at IS NULL`,
		email).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarColor, &u.Locale, &u.CreatedAt, &hash)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && hash == nil) {
		s.hasher.DummyVerify(ctx, in.Password)
		return nil, apperr.InvalidCredentials()
	}
	if err != nil {
		return nil, wrap(err)
	}
	ok, err := s.hasher.Verify(ctx, in.Password, *hash)
	if err != nil {
		return nil, wrap(err)
	}
	if !ok {
		return nil, apperr.InvalidCredentials()
	}
	var pair *TokenPair
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := upsertDevice(ctx, tx, u.ID, in.Device); err != nil {
			return err
		}
		var e error
		pair, e = s.startSession(ctx, tx, u, deviceID(in.Device), in.UserAgent)
		return e
	})
	if err != nil {
		return nil, wrap(err)
	}
	return pair, nil
}

// Refresh rotates a refresh token (T2). Presenting an already-rotated or revoked
// token revokes the whole family: it signals theft.
func (s *Service) Refresh(ctx context.Context, presented, userAgent string) (*TokenPair, error) {
	if presented == "" || len(presented) > 200 {
		return nil, apperr.InvalidRefresh()
	}
	h := HashRefresh(presented)
	var (
		pair      *TokenPair
		reuse     *uuid.UUID
		reuseUser uuid.UUID
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var (
			tokID, userID, family uuid.UUID
			devID                 *uuid.UUID
			expires               time.Time
			rotated, revoked      *time.Time
		)
		err := tx.QueryRow(ctx,
			`SELECT id,user_id,family_id,device_id,expires_at,rotated_at,revoked_at FROM refresh_tokens WHERE token_hash=$1 FOR UPDATE`,
			h).Scan(&tokID, &userID, &family, &devID, &expires, &rotated, &revoked)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.InvalidRefresh()
		}
		if err != nil {
			return err
		}
		now := s.now()
		if rotated != nil || revoked != nil {
			// Reuse detected: revoke family (outside this tx's rollback path: we commit the revocation).
			if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE family_id=$1 AND revoked_at IS NULL`, family); err != nil {
				return err
			}
			reuse, reuseUser = &family, userID
			return nil
		}
		if !now.Before(expires) {
			return apperr.InvalidRefresh()
		}
		u := &User{}
		if err := tx.QueryRow(ctx, `SELECT id,email,display_name,avatar_color,locale,created_at FROM users WHERE id=$1 AND deleted_at IS NULL`,
			userID).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarColor, &u.Locale, &u.CreatedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return apperr.InvalidRefresh()
			}
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET rotated_at = now() WHERE id=$1`, tokID); err != nil {
			return err
		}
		pair, err = s.issue(ctx, tx, u, family, devID, userAgent)
		if err == nil {
			pair.User = nil // refresh response carries tokens only (docs/design/06 §2.1)
		}
		return err
	})
	if err != nil {
		return nil, wrap(err)
	}
	if reuse != nil {
		s.revokeSession(ctx, *reuse)
		s.log.Warn("refresh token reuse detected; family revoked", "user_id", reuseUser, "family_id", *reuse)
		return nil, apperr.RefreshReused()
	}
	return pair, nil
}

// Logout revokes one auth session (refresh family).
func (s *Service) Logout(ctx context.Context, sid uuid.UUID) error {
	if _, err := s.db.Exec(ctx, `UPDATE refresh_tokens SET revoked_at = now() WHERE family_id=$1 AND revoked_at IS NULL`, sid); err != nil {
		return wrap(err)
	}
	s.revokeSession(ctx, sid)
	return nil
}

// LogoutAll revokes every session of a user.
func (s *Service) LogoutAll(ctx context.Context, userID uuid.UUID) error {
	rows, err := s.db.Query(ctx,
		`UPDATE refresh_tokens SET revoked_at = now() WHERE user_id=$1 AND revoked_at IS NULL RETURNING family_id`, userID)
	if err != nil {
		return wrap(err)
	}
	fams, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return wrap(err)
	}
	seen := map[uuid.UUID]struct{}{}
	for _, f := range fams {
		if _, ok := seen[f]; !ok {
			seen[f] = struct{}{}
			s.revokeSession(ctx, f)
		}
	}
	return nil
}

// SessionActive reports whether the auth session is still valid. Redis is the
// fast path; PostgreSQL is the fallback when Redis is unavailable (docs/design/08 §5).
func (s *Service) SessionActive(ctx context.Context, sid uuid.UUID) (bool, error) {
	if revoked, err := s.revoker.IsRevoked(ctx, sid); err == nil {
		return !revoked, nil
	}
	var total, revoked int
	err := s.db.QueryRow(ctx,
		`SELECT count(*), count(*) FILTER (WHERE revoked_at IS NOT NULL) FROM refresh_tokens WHERE family_id=$1`, sid).Scan(&total, &revoked)
	if err != nil {
		return false, err
	}
	// An unknown family (rows purged or never existed) is not an active session.
	return total > 0 && revoked == 0, nil
}

// GetUser loads a profile.
func (s *Service) GetUser(ctx context.Context, userID uuid.UUID) (*User, error) {
	u := &User{}
	err := s.db.QueryRow(ctx, `SELECT id,email,display_name,avatar_color,locale,created_at FROM users WHERE id=$1 AND deleted_at IS NULL`,
		userID).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarColor, &u.Locale, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound()
	}
	if err != nil {
		return nil, wrap(err)
	}
	return u, nil
}

// UpdateProfile applies a validated patch.
func (s *Service) UpdateProfile(ctx context.Context, userID uuid.UUID, p ProfilePatch) (*User, error) {
	if err := validatePatch(&p); err != nil {
		return nil, err
	}
	u := &User{}
	err := s.db.QueryRow(ctx, `
		UPDATE users SET display_name = COALESCE($2, display_name), avatar_color = COALESCE($3, avatar_color),
		                 locale = COALESCE($4, locale), updated_at = now()
		WHERE id=$1 AND deleted_at IS NULL
		RETURNING id,email,display_name,avatar_color,locale,created_at`,
		userID, p.DisplayName, p.AvatarColor, p.Locale).Scan(&u.ID, &u.Email, &u.DisplayName, &u.AvatarColor, &u.Locale, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound()
	}
	if err != nil {
		return nil, wrap(err)
	}
	return u, nil
}

// ---- internals ----

func (s *Service) startSession(ctx context.Context, tx pgx.Tx, u *User, dev *uuid.UUID, ua string) (*TokenPair, error) {
	family := id.New()
	pair, err := s.issue(ctx, tx, u, family, dev, ua)
	if err != nil {
		return nil, err
	}
	// Keep at most maxFamiliesPerUser live sessions: revoke the oldest extras.
	rows, err := tx.Query(ctx, `
		SELECT family_id FROM refresh_tokens
		WHERE user_id=$1 AND revoked_at IS NULL AND expires_at > now()
		GROUP BY family_id ORDER BY min(issued_at) DESC OFFSET $2`, u.ID, maxFamiliesPerUser)
	if err != nil {
		return nil, err
	}
	old, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return nil, err
	}
	for _, f := range old {
		if _, err := tx.Exec(ctx, `UPDATE refresh_tokens SET revoked_at=now() WHERE family_id=$1 AND revoked_at IS NULL`, f); err != nil {
			return nil, err
		}
		s.revokeSession(ctx, f)
	}
	return pair, nil
}

// issue mints an access token and stores a new refresh token in the family.
func (s *Service) issue(ctx context.Context, tx pgx.Tx, u *User, family uuid.UUID, dev *uuid.UUID, ua string) (*TokenPair, error) {
	rt, rh, err := NewRefreshToken()
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO refresh_tokens (id,user_id,device_id,family_id,token_hash,expires_at,user_agent) VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		id.New(), u.ID, dev, family, rh, s.now().Add(s.refreshTTL), truncate(ua, 200)); err != nil {
		return nil, err
	}
	at, exp, err := s.signer.AccessToken(u.ID, family)
	if err != nil {
		return nil, err
	}
	return &TokenPair{User: u, AccessToken: at, ExpiresIn: int(time.Until(exp).Seconds()), RefreshToken: rt}, nil
}

func (s *Service) revokeSession(ctx context.Context, sid uuid.UUID) {
	if err := s.revoker.Revoke(ctx, sid, s.accessTTL); err != nil {
		// Not fatal: PostgreSQL holds the truth and SessionActive falls back to it.
		s.log.Warn("revoke marker failed", "sid", sid, "err", err)
	}
}

func upsertDevice(ctx context.Context, tx pgx.Tx, userID uuid.UUID, d *DeviceInfo) error {
	if d == nil {
		return nil
	}
	// A device row may move between accounts (shared phone); its FCM token is then
	// cleared until the new owner registers it (docs/design/06 §2.2).
	_, err := tx.Exec(ctx, `
		INSERT INTO devices (id,user_id,model,os_version,app_version,has_gms,last_seen_at)
		VALUES ($1,$2,$3,$4,$5,$6,now())
		ON CONFLICT (id) DO UPDATE SET
		  fcm_token      = CASE WHEN devices.user_id = EXCLUDED.user_id THEN devices.fcm_token ELSE NULL END,
		  fcm_updated_at = CASE WHEN devices.user_id = EXCLUDED.user_id THEN devices.fcm_updated_at ELSE NULL END,
		  user_id=EXCLUDED.user_id, model=EXCLUDED.model, os_version=EXCLUDED.os_version,
		  app_version=EXCLUDED.app_version, has_gms=EXCLUDED.has_gms, last_seen_at=now()`,
		d.DeviceID, userID, d.Model, d.OSVersion, d.AppVersion, d.HasGMS)
	return err
}

func deviceID(d *DeviceInfo) *uuid.UUID {
	if d == nil {
		return nil
	}
	return &d.DeviceID
}

func isUnique(err error, constraint string) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505" && pe.ConstraintName == constraint
}

// wrap passes API errors through and turns anything else into a 500.
func wrap(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return apperr.Internal(fmt.Errorf("identity: %w", err))
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// ConsentView is the current state of one consent kind for GET /v1/me.
type ConsentView struct {
	Kind        string     `json:"kind"`
	Version     string     `json:"version"`
	GrantedAt   time.Time  `json:"granted_at"`
	WithdrawnAt *time.Time `json:"withdrawn_at"`
}

// Consents returns the latest record per kind.
func (s *Service) Consents(ctx context.Context, userID uuid.UUID) ([]ConsentView, error) {
	rows, err := s.db.Query(ctx, `
		SELECT DISTINCT ON (kind) kind, version, granted_at, withdrawn_at
		FROM consent_records WHERE user_id=$1 ORDER BY kind, granted_at DESC`, userID)
	if err != nil {
		return nil, wrap(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ConsentView, error) {
		var c ConsentView
		err := r.Scan(&c.Kind, &c.Version, &c.GrantedAt, &c.WithdrawnAt)
		return c, err
	})
	if err != nil {
		return nil, wrap(err)
	}
	if out == nil {
		out = []ConsentView{}
	}
	return out, nil
}
