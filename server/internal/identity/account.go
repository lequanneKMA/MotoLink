package identity

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"motolink/internal/platform/apperr"
	"motolink/internal/platform/id"
)

var consentKinds = map[string]bool{"location_processing": true, "privacy_policy": true, "terms": true, "background_location": true}

// HasConsent reports whether the latest record of kind is granted and not withdrawn.
func (s *Service) HasConsent(ctx context.Context, userID uuid.UUID, kind string) (bool, error) {
	var withdrawn *time.Time
	err := s.db.QueryRow(ctx,
		`SELECT withdrawn_at FROM consent_records WHERE user_id=$1 AND kind=$2 ORDER BY granted_at DESC LIMIT 1`, userID, kind).Scan(&withdrawn)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return withdrawn == nil, nil
}

// SetConsent grants or withdraws a consent (docs/design/06 §2.2). Withdrawing location processing is
// the caller's cue to end open tracking sessions (done by SessionEnder).
func (s *Service) SetConsent(ctx context.Context, userID uuid.UUID, kind, version string, granted bool) error {
	if !consentKinds[kind] {
		return apperr.Validation(apperr.FieldError{Field: "kind", Code: "unknown"})
	}
	if granted && strings.TrimSpace(version) == "" {
		return apperr.Validation(apperr.FieldError{Field: "version", Code: "required"})
	}
	if granted {
		_, err := s.db.Exec(ctx, `INSERT INTO consent_records (id,user_id,kind,version) VALUES ($1,$2,$3,$4)`, id.New(), userID, kind, version)
		return wrap(err)
	}
	_, err := s.db.Exec(ctx, `UPDATE consent_records SET withdrawn_at=now() WHERE user_id=$1 AND kind=$2 AND withdrawn_at IS NULL`, userID, kind)
	return wrap(err)
}

type DeviceInput struct {
	Platform   string `json:"platform"`
	Model      string `json:"model"`
	OSVersion  string `json:"os_version"`
	AppVersion string `json:"app_version"`
	HasGMS     bool   `json:"has_gms"`
	FCMToken   string `json:"fcm_token"`
}

// PutDevice registers or updates a device and its FCM token (an FCM token belongs to one device).
func (s *Service) PutDevice(ctx context.Context, userID, deviceID uuid.UUID, in DeviceInput) error {
	if in.Platform != "" && in.Platform != "android" {
		return apperr.Validation(apperr.FieldError{Field: "platform", Code: "unsupported"})
	}
	if len(in.FCMToken) > 4096 || len(in.Model) > 100 {
		return apperr.Validation(apperr.FieldError{Field: "device", Code: "too_long"})
	}
	return wrap(pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var tok *string
		if in.FCMToken != "" {
			tok = &in.FCMToken
			// a token moves with the physical device: clear it from any other row first
			if _, err := tx.Exec(ctx, `UPDATE devices SET fcm_token=NULL, fcm_updated_at=NULL WHERE fcm_token=$1 AND id<>$2`, in.FCMToken, deviceID); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `
			INSERT INTO devices (id,user_id,model,os_version,app_version,has_gms,fcm_token,fcm_updated_at,last_seen_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7, CASE WHEN $7::text IS NULL THEN NULL ELSE now() END, now())
			ON CONFLICT (id) DO UPDATE SET user_id=EXCLUDED.user_id, model=EXCLUDED.model, os_version=EXCLUDED.os_version,
			  app_version=EXCLUDED.app_version, has_gms=EXCLUDED.has_gms, last_seen_at=now(),
			  fcm_token=CASE WHEN EXCLUDED.fcm_token IS NOT NULL THEN EXCLUDED.fcm_token ELSE devices.fcm_token END,
			  fcm_updated_at=CASE WHEN EXCLUDED.fcm_token IS NOT NULL THEN now() ELSE devices.fcm_updated_at END
			WHERE devices.user_id = EXCLUDED.user_id`,
			deviceID, userID, in.Model, in.OSVersion, in.AppVersion, in.HasGMS, tok)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 { // the id belongs to another account: login/register reassigns, this call never does
			return apperr.Conflict("device_owned", "Thiết bị thuộc tài khoản khác")
		}
		return nil
	}))
}

func (s *Service) DeleteDevice(ctx context.Context, userID, deviceID uuid.UUID) error {
	_, err := s.db.Exec(ctx, `DELETE FROM devices WHERE id=$1 AND user_id=$2`, deviceID, userID)
	return wrap(err)
}

// DeviceTokens returns FCM tokens of the given users (for the notifier).
func (s *Service) DeviceTokens(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID][]string, error) {
	rows, err := s.db.Query(ctx, `SELECT user_id, fcm_token FROM devices WHERE user_id = ANY($1) AND fcm_token IS NOT NULL`, userIDs)
	if err != nil {
		return nil, wrap(err)
	}
	out := map[uuid.UUID][]string{}
	for rows.Next() {
		var u uuid.UUID
		var t string
		if err := rows.Scan(&u, &t); err != nil {
			return nil, wrap(err)
		}
		out[u] = append(out[u], t)
	}
	return out, wrap(rows.Err())
}

// ClearFCMToken drops a token the push service reported invalid.
func (s *Service) ClearFCMToken(ctx context.Context, token string) error {
	_, err := s.db.Exec(ctx, `UPDATE devices SET fcm_token=NULL, fcm_updated_at=NULL WHERE fcm_token=$1`, token)
	return wrap(err)
}

// HasPassword is used by account deletion to demand re-authentication.
func (s *Service) VerifyPassword(ctx context.Context, userID uuid.UUID, password string) error {
	var hash *string
	if err := s.db.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1 AND deleted_at IS NULL`, userID).Scan(&hash); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		return wrap(err)
	}
	if hash == nil {
		return nil // social-only account (future): re-auth handled by the provider flow
	}
	ok, err := s.hasher.Verify(ctx, password, *hash)
	if err != nil {
		return wrap(err)
	}
	if !ok {
		return apperr.InvalidCredentials()
	}
	return nil
}

// DeleteAccount erases a user (docs/design/05 §6). The caller must already have refused when the user still
// owns a trip with other members. Order: revoke sessions, remove from trips, delete personal data, anonymise the row.
func (s *Service) DeleteAccount(ctx context.Context, userID uuid.UUID) error {
	if err := s.LogoutAll(ctx, userID); err != nil {
		return err
	}
	return wrap(pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		stmts := []string{
			`UPDATE tracking_sessions SET status='ended', end_reason='account_deleted', ended_at=coalesce(ended_at, now()) WHERE user_id=$1 AND status IN ('active','paused')`,
			`UPDATE trip_members SET status='left', left_at=coalesce(left_at, now()) WHERE user_id=$1 AND status='active'`,
			`DELETE FROM location_points WHERE user_id=$1`,
			`DELETE FROM sos_events WHERE user_id=$1`,
			`DELETE FROM notifications WHERE user_id=$1`,
			`DELETE FROM devices WHERE user_id=$1`,
			`DELETE FROM idempotency_keys WHERE user_id=$1`,
			`UPDATE refresh_tokens SET user_agent=NULL, revoked_at=coalesce(revoked_at, now()) WHERE user_id=$1`, // hashes only; kept so revocation stays provable
		}
		for _, q := range stmts {
			if _, err := tx.Exec(ctx, q, userID); err != nil {
				return err
			}
		}
		// Keep the row (trips/trip_events reference it) but strip every personal field and free the e-mail.
		_, err := tx.Exec(ctx, `UPDATE users SET email='deleted+'||id::text||'@invalid.local', password_hash=NULL, display_name='Người dùng đã xóa',
			deleted_at=now(), updated_at=now() WHERE id=$1`, userID)
		return err
	}))
}

// OwnsTripsWithOthers lists trips the user owns that still have other active members.
func (s *Service) OwnsTripsWithOthers(ctx context.Context, userID uuid.UUID) (bool, error) {
	var n int
	err := s.db.QueryRow(ctx, `
		SELECT count(*) FROM trips t WHERE t.owner_id=$1 AND t.status IN ('draft','active')
		AND EXISTS (SELECT 1 FROM trip_members m WHERE m.trip_id=t.id AND m.status='active' AND m.user_id<>$1)`, userID).Scan(&n)
	return n > 0, wrap(err)
}

// Export returns the caller's own data as a JSON-friendly map (right of access, docs/design/11 §5).
func (s *Service) Export(ctx context.Context, userID uuid.UUID) (map[string]any, error) {
	u, err := s.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	cons, err := s.Consents(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"user": u, "consents": cons, "exported_at": time.Now().UTC()}
	type row = map[string]any
	collect := func(name, q string) error {
		rows, err := s.db.Query(ctx, q, userID)
		if err != nil {
			return wrap(err)
		}
		defer rows.Close()
		var list []row
		cols := rows.FieldDescriptions()
		for rows.Next() {
			vals, err := rows.Values()
			if err != nil {
				return wrap(err)
			}
			r := row{}
			for i, c := range cols {
				r[c.Name] = vals[i]
			}
			list = append(list, r)
		}
		if list == nil {
			list = []row{}
		}
		out[name] = list
		return wrap(rows.Err())
	}
	for name, q := range map[string]string{
		"trips":    `SELECT t.id, t.name, t.status, m.role, m.status AS membership_status, m.joined_at FROM trip_members m JOIN trips t ON t.id=m.trip_id WHERE m.user_id=$1`,
		"sessions": `SELECT id, trip_id, status, started_at, ended_at, point_count, distance_m, duration_s FROM tracking_sessions WHERE user_id=$1`,
		"points":   `SELECT session_id, seq, recorded_at, lat, lng, accuracy_m, speed_mps FROM location_points WHERE user_id=$1 ORDER BY session_id, seq LIMIT 200000`,
		"sos":      `SELECT id, trip_id, kind, message, status, created_at FROM sos_events WHERE user_id=$1`,
		"devices":  `SELECT id, model, os_version, app_version, has_gms, created_at FROM devices WHERE user_id=$1`,
	} {
		if err := collect(name, q); err != nil {
			return nil, err
		}
	}
	return out, nil
}
