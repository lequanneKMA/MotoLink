package trip

import (
	"context"
	"errors"
	"regexp"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"motolink/internal/platform/apperr"
)

var colorRe = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Members lists active members (and optionally those who left/were removed).
func (s *Service) Members(ctx context.Context, tripID, userID uuid.UUID, includeLeft bool) ([]Member, error) {
	if _, _, err := s.Access(ctx, tripID, userID); err != nil {
		return nil, err
	}
	return s.members(ctx, tripID, includeLeft)
}

func (s *Service) members(ctx context.Context, tripID uuid.UUID, includeLeft bool) ([]Member, error) {
	rows, err := s.db.Query(ctx, `
		SELECT m.trip_id,m.user_id,m.role,m.status,m.sharing_state,m.share_history,coalesce(m.color,''),m.joined_at,m.left_at,u.display_name
		FROM trip_members m JOIN users u ON u.id=m.user_id
		WHERE m.trip_id=$1 AND ($2 OR m.status='active') ORDER BY m.joined_at, m.user_id`, tripID, includeLeft)
	if err != nil {
		return nil, wrap(err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Member, error) {
		var m Member
		err := r.Scan(&m.TripID, &m.UserID, &m.Role, &m.Status, &m.SharingState, &m.ShareHistory, &m.Color, &m.JoinedAt, &m.LeftAt, &m.DisplayName)
		return m, err
	})
	if err != nil {
		return nil, wrap(err)
	}
	if out == nil {
		out = []Member{}
	}
	return out, nil
}

// ActiveMemberIDs is used by SOS/notifications to find recipients.
func (s *Service) ActiveMemberIDs(ctx context.Context, tripID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.db.Query(ctx, `SELECT user_id FROM trip_members WHERE trip_id=$1 AND status='active'`, tripID)
	if err != nil {
		return nil, wrap(err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
}

type MemberPatch struct {
	SharingState *string `json:"sharing_state"`
	ShareHistory *bool   `json:"share_history"`
	Color        *string `json:"color"`
}

// PatchMe updates the caller's own membership settings.
func (s *Service) PatchMe(ctx context.Context, tripID, userID uuid.UUID, p MemberPatch) (*Membership, error) {
	if p.SharingState != nil && *p.SharingState != SharingOn && *p.SharingState != SharingPaused {
		return nil, apperr.Validation(apperr.FieldError{Field: "sharing_state", Code: "invalid"})
	}
	if p.Color != nil && !colorRe.MatchString(*p.Color) {
		return nil, apperr.Validation(apperr.FieldError{Field: "color", Code: "invalid"})
	}
	var (
		out     *Membership
		ev      *Event
		changed bool
	)
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		t, cur, err := access(ctx, tx, tripID, userID, true)
		if err != nil {
			return err
		}
		if t.Status == StatusArchived {
			return invalidState()
		}
		m := &Membership{}
		err = tx.QueryRow(ctx, `
			UPDATE trip_members SET sharing_state=COALESCE($3,sharing_state), share_history=COALESCE($4,share_history), color=COALESCE($5,color)
			WHERE trip_id=$1 AND user_id=$2
			RETURNING trip_id,user_id,role,status,sharing_state,share_history,coalesce(color,''),joined_at,left_at`,
			tripID, userID, p.SharingState, p.ShareHistory, p.Color).
			Scan(&m.TripID, &m.UserID, &m.Role, &m.Status, &m.SharingState, &m.ShareHistory, &m.Color, &m.JoinedAt, &m.LeftAt)
		if err != nil {
			return err
		}
		out = m
		if m.SharingState != cur.SharingState {
			changed = true
			e, err := AppendEvent(ctx, tx, tripID, &userID, KindSharingChanged,
				map[string]any{"user_id": userID, "sharing_state": m.SharingState})
			if err != nil {
				return err
			}
			ev = &e
		}
		return nil
	})
	if err != nil {
		return nil, wrap(err)
	}
	if ev != nil {
		s.pub.PublishEvent(ctx, *ev)
	}
	if changed {
		s.pub.SharingChanged(ctx, tripID, userID, out.SharingState == SharingOn)
	}
	return out, nil
}

// Leave removes the caller from the trip (active -> left). The owner must transfer first unless alone.
func (s *Service) Leave(ctx context.Context, tripID, userID uuid.UUID) error {
	var evs []Event
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		t, m, err := access(ctx, tx, tripID, userID, true)
		if err != nil {
			return err
		}
		if m.Role == RoleOwner {
			var others int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM trip_members WHERE trip_id=$1 AND status='active' AND user_id<>$2`, tripID, userID).Scan(&others); err != nil {
				return err
			}
			if others > 0 {
				return apperr.Conflict("owner_must_transfer", "Chủ chuyến đi phải chuyển quyền trước khi rời")
			}
			if t.Status == StatusDraft || t.Status == StatusActive { // sole owner leaving closes the trip
				if evs, err = endTx(ctx, tx, tripID, &userID, "owner_left"); err != nil {
					return err
				}
			}
		}
		more, err := leaveTx(ctx, tx, tripID, userID, MemberLeft, "user", KindMemberLeft)
		evs = append(evs, more...)
		return err
	})
	if err != nil {
		return wrap(err)
	}
	s.publish(ctx, evs)
	s.pub.MembershipChanged(ctx, tripID, userID, MemberLeft)
	return nil
}

// Remove lets the owner remove a member (active -> removed).
func (s *Service) Remove(ctx context.Context, tripID, ownerID, target uuid.UUID) error {
	if ownerID == target {
		return apperr.ForbiddenCode("forbidden", "Không thể tự xóa mình, hãy dùng chức năng rời chuyến đi")
	}
	var evs []Event
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		_, m, err := access(ctx, tx, tripID, ownerID, true)
		if err != nil {
			return err
		}
		if m.Role != RoleOwner {
			return apperr.Forbidden()
		}
		var role, status string
		err = tx.QueryRow(ctx, `SELECT role,status FROM trip_members WHERE trip_id=$1 AND user_id=$2`, tripID, target).Scan(&role, &status)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != MemberActive) {
			return apperr.NotFound()
		}
		if err != nil {
			return err
		}
		evs, err = leaveTx(ctx, tx, tripID, target, MemberRemoved, "removed", KindMemberRemoved)
		return err
	})
	if err != nil {
		return wrap(err)
	}
	s.publish(ctx, evs)
	s.pub.MembershipChanged(ctx, tripID, target, MemberRemoved)
	return nil
}

// leaveTx flips membership, ends the user's open sessions and records events.
func leaveTx(ctx context.Context, tx pgx.Tx, tripID, userID uuid.UUID, newStatus, endReason, kind string) ([]Event, error) {
	if _, err := tx.Exec(ctx, `UPDATE trip_members SET status=$3, left_at=now() WHERE trip_id=$1 AND user_id=$2`, tripID, userID, newStatus); err != nil {
		return nil, err
	}
	var evs []Event
	ses, err := EndOpenSessionsTx(ctx, tx, tripID, &userID, endReason)
	if err != nil {
		return nil, err
	}
	for _, se := range ses {
		ev, err := AppendEvent(ctx, tx, tripID, &userID, KindTrackingEnded, se)
		if err != nil {
			return nil, err
		}
		evs = append(evs, ev)
	}
	ev, err := AppendEvent(ctx, tx, tripID, &userID, kind, map[string]any{"user_id": userID})
	if err != nil {
		return nil, err
	}
	return append(evs, ev), nil
}

// TransferOwnership hands the owner role to another active member.
func (s *Service) TransferOwnership(ctx context.Context, tripID, ownerID, target uuid.UUID) error {
	var ev Event
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		t, m, err := access(ctx, tx, tripID, ownerID, true)
		if err != nil {
			return err
		}
		if m.Role != RoleOwner {
			return apperr.Forbidden()
		}
		if ownerID == target {
			return apperr.Conflict("invalid_target", "Người nhận quyền phải là thành viên khác")
		}
		if t.Status == StatusArchived {
			return invalidState()
		}
		var status string
		err = tx.QueryRow(ctx, `SELECT status FROM trip_members WHERE trip_id=$1 AND user_id=$2`, tripID, target).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != MemberActive) {
			return apperr.Conflict("invalid_target", "Người nhận quyền phải là thành viên đang hoạt động")
		}
		if err != nil {
			return err
		}
		// demote first: the partial unique index allows one active owner at a time
		if _, err := tx.Exec(ctx, `UPDATE trip_members SET role='member' WHERE trip_id=$1 AND user_id=$2`, tripID, ownerID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE trip_members SET role='owner' WHERE trip_id=$1 AND user_id=$2`, tripID, target); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE trips SET owner_id=$2 WHERE id=$1`, tripID, target); err != nil {
			return err
		}
		ev, err = AppendEvent(ctx, tx, tripID, &ownerID, KindOwnershipTransfer, map[string]any{"from": ownerID, "to": target})
		return err
	})
	if err != nil {
		return wrap(err)
	}
	s.pub.PublishEvent(ctx, ev)
	return nil
}
