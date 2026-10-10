package trip

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"motolink/internal/platform/apperr"
	"motolink/internal/platform/id"
)

// Crockford base32 without I, L, O, U: 32 symbols => 5 bits each; 8 symbols => 40 bits (docs/design/05).
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const (
	defaultInviteTTL = 24 * time.Hour
	maxInviteTTL     = 7 * 24 * time.Hour
)

// NewCode returns a random display code like "ABCD-EFGH".
func NewCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, 0, 9)
	for i, x := range b {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, codeAlphabet[int(x)&31])
	}
	return string(out), nil
}

// NormalizeCode folds user input to the canonical form: case-insensitive, no separators,
// and the usual confusions (O->0, I/L->1) per Crockford. Returns "" if it cannot be a code.
func NormalizeCode(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(s) {
		switch r {
		case '-', ' ', '\t':
			continue
		case 'O':
			r = '0'
		case 'I', 'L':
			r = '1'
		}
		if !strings.ContainsRune(codeAlphabet, r) {
			return ""
		}
		b.WriteRune(r)
	}
	if b.Len() != 8 {
		return ""
	}
	return b.String()
}

func hashCode(canonical string) []byte {
	h := sha256.Sum256([]byte(canonical))
	return h[:]
}

type InviteInput struct {
	ExpiresInS int `json:"expires_in_s"`
	MaxUses    int `json:"max_uses"`
}

// CreateInvitation (owner only). The plain code is returned once.
func (s *Service) CreateInvitation(ctx context.Context, tripID, userID uuid.UUID, in InviteInput) (*Invitation, error) {
	t, _, err := s.RequireOwner(ctx, tripID, userID)
	if err != nil {
		return nil, err
	}
	if t.Status != StatusDraft && t.Status != StatusActive {
		return nil, invalidState()
	}
	ttl := defaultInviteTTL
	if in.ExpiresInS != 0 {
		ttl = time.Duration(in.ExpiresInS) * time.Second
		if ttl < time.Minute || ttl > maxInviteTTL {
			return nil, apperr.Validation(apperr.FieldError{Field: "expires_in_s", Code: "out_of_range"})
		}
	}
	maxUses := in.MaxUses
	if maxUses == 0 {
		maxUses = t.MaxMembers
	}
	if maxUses < 1 || maxUses > 50 {
		return nil, apperr.Validation(apperr.FieldError{Field: "max_uses", Code: "out_of_range"})
	}
	code, err := NewCode()
	if err != nil {
		return nil, wrap(err)
	}
	inv := &Invitation{ID: id.New(), TripID: tripID, MaxUses: maxUses, Code: code}
	err = s.db.QueryRow(ctx, `
		INSERT INTO trip_invitations (id,trip_id,created_by,code_hash,expires_at,max_uses)
		VALUES ($1,$2,$3,$4, now() + make_interval(secs => $5), $6) RETURNING expires_at, created_at`,
		inv.ID, tripID, userID, hashCode(NormalizeCode(code)), ttl.Seconds(), maxUses).Scan(&inv.ExpiresAt, &inv.CreatedAt)
	if err != nil {
		return nil, wrap(err)
	}
	return inv, nil
}

func (s *Service) ListInvitations(ctx context.Context, tripID, userID uuid.UUID) ([]Invitation, error) {
	if _, _, err := s.RequireOwner(ctx, tripID, userID); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(ctx, `SELECT id,trip_id,expires_at,max_uses,used_count,revoked_at,created_at FROM trip_invitations
		WHERE trip_id=$1 ORDER BY created_at DESC LIMIT 100`, tripID)
	if err != nil {
		return nil, wrap(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Invitation, error) {
		var i Invitation
		err := r.Scan(&i.ID, &i.TripID, &i.ExpiresAt, &i.MaxUses, &i.UsedCount, &i.RevokedAt, &i.CreatedAt)
		return i, err
	})
	if err != nil {
		return nil, wrap(err)
	}
	if out == nil {
		out = []Invitation{}
	}
	return out, nil
}

func (s *Service) RevokeInvitation(ctx context.Context, tripID, userID, invID uuid.UUID) error {
	if _, _, err := s.RequireOwner(ctx, tripID, userID); err != nil {
		return err
	}
	tag, err := s.db.Exec(ctx, `UPDATE trip_invitations SET revoked_at=coalesce(revoked_at, now()) WHERE id=$1 AND trip_id=$2`, invID, tripID)
	if err != nil {
		return wrap(err)
	}
	if tag.RowsAffected() == 0 {
		return apperr.NotFound()
	}
	return nil
}

// InvitePreview is what a code holder may see before joining.
type InvitePreview struct {
	TripName    string `json:"trip_name"`
	MemberCount int    `json:"member_count"`
	OwnerName   string `json:"owner_name"`
	Valid       bool   `json:"valid"`
}

// Resolve describes the trip behind a code. Every failure is the same 404 so codes cannot be probed.
func (s *Service) Resolve(ctx context.Context, code string) (*InvitePreview, error) {
	c := NormalizeCode(code)
	if c == "" {
		return nil, apperr.NotFound()
	}
	p := &InvitePreview{Valid: true}
	err := s.db.QueryRow(ctx, `
		SELECT t.name, u.display_name, (SELECT count(*) FROM trip_members m WHERE m.trip_id=t.id AND m.status='active')
		FROM trip_invitations i JOIN trips t ON t.id=i.trip_id JOIN users u ON u.id=t.owner_id
		WHERE i.code_hash=$1 AND i.revoked_at IS NULL AND i.expires_at > now() AND i.used_count < i.max_uses
		  AND t.status IN ('draft','active')`, hashCode(c)).Scan(&p.TripName, &p.OwnerName, &p.MemberCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, apperr.NotFound()
	}
	if err != nil {
		return nil, wrap(err)
	}
	return p, nil
}

type JoinResult struct {
	Trip       *Trip       `json:"trip"`
	Membership *Membership `json:"my_membership"`
}

// Join consumes a code (T4). Idempotent for people who are already active members.
func (s *Service) Join(ctx context.Context, userID uuid.UUID, code string) (*JoinResult, error) {
	c := NormalizeCode(code)
	if c == "" {
		return nil, apperr.NotFound()
	}
	var (
		res *JoinResult
		ev  *Event
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var invID, tripID uuid.UUID
		var expires time.Time
		var maxUses, used int
		var revoked *time.Time
		err := tx.QueryRow(ctx, `SELECT id,trip_id,expires_at,max_uses,used_count,revoked_at FROM trip_invitations WHERE code_hash=$1 FOR UPDATE`,
			hashCode(c)).Scan(&invID, &tripID, &expires, &maxUses, &used, &revoked)
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		if revoked != nil {
			return apperr.NotFound() // do not reveal that the code once existed
		}
		if !time.Now().Before(expires) {
			return apperr.Gone("expired", "Mã mời đã hết hạn")
		}
		t, err := scanTrip(tx.QueryRow(ctx, `SELECT `+tripCols+` FROM trips WHERE id=$1 FOR UPDATE`, tripID))
		if err != nil {
			return err
		}
		if t.Status != StatusDraft && t.Status != StatusActive {
			return invalidState()
		}
		var status string
		err = tx.QueryRow(ctx, `SELECT status FROM trip_members WHERE trip_id=$1 AND user_id=$2`, tripID, userID).Scan(&status)
		exists := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if exists && status == MemberRemoved {
			return apperr.Conflict("removed", "Bạn đã bị xóa khỏi chuyến đi này")
		}
		if exists && status == MemberActive { // idempotent: nothing consumed
			t2, m, err := access(ctx, tx, tripID, userID, false)
			if err != nil {
				return err
			}
			res = &JoinResult{Trip: t2, Membership: m}
			return nil
		}
		if used >= maxUses {
			return apperr.Gone("exhausted", "Mã mời đã hết lượt dùng")
		}
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM trip_members WHERE trip_id=$1 AND status='active'`, tripID).Scan(&active); err != nil {
			return err
		}
		if active >= t.MaxMembers {
			return apperr.Conflict("trip_full", "Chuyến đi đã đủ thành viên")
		}
		if exists { // rejoin after leaving
			_, err = tx.Exec(ctx, `UPDATE trip_members SET status='active', left_at=NULL, joined_at=now(), role='member', sharing_state='sharing' WHERE trip_id=$1 AND user_id=$2`, tripID, userID)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO trip_members (trip_id,user_id,role,status,color) VALUES ($1,$2,'member','active',$3)`,
				tripID, userID, colorFor(active))
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE trip_invitations SET used_count = used_count + 1 WHERE id=$1`, invID); err != nil {
			return err
		}
		var name string
		if err := tx.QueryRow(ctx, `SELECT display_name FROM users WHERE id=$1`, userID).Scan(&name); err != nil {
			return err
		}
		e, err := AppendEvent(ctx, tx, tripID, &userID, KindMemberJoined, map[string]any{"user_id": userID, "display_name": name})
		if err != nil {
			return err
		}
		ev = &e
		t2, m, err := access(ctx, tx, tripID, userID, false)
		if err != nil {
			return err
		}
		res = &JoinResult{Trip: t2, Membership: m}
		return nil
	})
	if err != nil {
		return nil, wrap(err)
	}
	if ev != nil {
		s.pub.PublishEvent(ctx, *ev)
	}
	return res, nil
}
