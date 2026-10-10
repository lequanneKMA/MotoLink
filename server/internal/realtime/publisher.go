package realtime

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"motolink/internal/trip"
)

// Hub implements trip.EventPublisher (fast path after commits) and trip.LiveState (snapshots).
var (
	_ trip.EventPublisher = (*Hub)(nil)
	_ trip.LiveState      = (*Hub)(nil)
)

// PublishEvent fans a committed durable event to this instance's room and to the other instances.
// The outbox relay publishes again later (at-least-once); clients de-duplicate by event_seq.
func (h *Hub) PublishEvent(ctx context.Context, ev trip.Event) {
	frame := eventFrame(ev)
	var except uuid.UUID
	if ev.Kind == trip.KindSOSCreated && ev.ActorID != nil {
		except = *ev.ActorID
	}
	h.shardFor(ev.TripID).send(ctx, cmdFrame{trip: ev.TripID, frame: frame, except: except})
	m := busMsg{K: kFrame, T: ev.TripID.String(), F: frame}
	if except != uuid.Nil {
		m.U = except.String()
	}
	h.publishBus(tripChannel(ev.TripID), m)

	// A tracking state change must reach the owner's connection so its session cache is not stale.
	switch ev.Kind {
	case trip.KindTrackingStarted, trip.KindTrackingPaused, trip.KindTrackingResumed, trip.KindTrackingEnded:
		var d struct {
			UserID    uuid.UUID `json:"user_id"`
			SessionID uuid.UUID `json:"session_id"`
		}
		if json.Unmarshal(ev.Data, &d) == nil && d.UserID != uuid.Nil {
			if c := h.clientOf(d.UserID); c != nil {
				c.invalidateSessions()
			}
			h.publishUserCtl(d.UserID, busMsg{K: kSession, T: ev.TripID.String(), S: d.SessionID.String()})
			if ev.Kind == trip.KindTrackingEnded {
				h.acks.Forget(d.SessionID)
			}
		}
	}
}

func (h *Hub) MembershipChanged(ctx context.Context, tripID, userID uuid.UUID, status string) {
	h.localMembershipChanged(ctx, tripID, userID, status)
	h.publishUserCtl(userID, busMsg{K: kMembership, T: tripID.String(), S: status})
}

func (h *Hub) SharingChanged(ctx context.Context, tripID, userID uuid.UUID, sharing bool) {
	h.localSharingChanged(tripID, userID, sharing)
	s := "off"
	if sharing {
		s = "on"
	} else {
		_ = h.deps.Live.DeleteLocation(ctx, tripID, userID) // a paused member must not linger in snapshots
	}
	h.publishUserCtl(userID, busMsg{K: kSharing, T: tripID.String(), S: s})
}

// TripEnded clears the volatile state of a finished trip.
func (h *Hub) TripEnded(ctx context.Context, tripID uuid.UUID) {
	if h.deps.Trips == nil {
		return
	}
	ids, err := h.deps.Trips.ActiveMemberIDs(ctx, tripID)
	if err == nil {
		_ = h.deps.Live.DeleteTripLocations(ctx, tripID, ids)
	}
}

// RevokeSession is called when an auth session is revoked (logout): its connection closes at once.
func (h *Hub) RevokeSession(userID, sid uuid.UUID) {
	if c := h.clientOf(userID); c != nil && c.sid == sid {
		c.push(errFrame("", "token_expired", "Phiên đã bị thu hồi", false, 0))
		c.kick(CloseAuthExpired, "revoked")
	}
	h.publishUserCtl(userID, busMsg{K: kRevoke, S: sid.String()})
}

// ---- trip.LiveState ----

func (h *Hub) Locations(ctx context.Context, tripID uuid.UUID, users []uuid.UUID) (map[uuid.UUID]trip.LiveLocation, error) {
	m, err := h.deps.Live.Locations(ctx, tripID, users)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]trip.LiveLocation, len(m))
	for u, p := range m {
		out[u] = trip.LiveLocation{SessionID: p.SessionID, Seq: p.Seq, Lat: p.Lat, Lng: p.Lng, AccuracyM: p.AccuracyM,
			SpeedMps: p.SpeedMps, Heading: p.Heading, ServerTs: p.ServerTs}
	}
	return out, nil
}

func (h *Hub) Online(ctx context.Context, tripID uuid.UUID, users []uuid.UUID) (map[uuid.UUID]bool, error) {
	return h.deps.Live.Online(ctx, tripID, users)
}
