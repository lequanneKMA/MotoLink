package realtime

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"motolink/internal/platform/apperr"
	"motolink/internal/trip"
)

// eventFrame renders a durable trip event for clients. SOS events use their own message type
// whose payload is the event data prepared by the SOS module (docs/design/07 §3.6).
func eventFrame(ev trip.Event) []byte {
	if ev.Kind == trip.KindSOSCreated {
		return marshalEnv("sos.event", "", ev.TripID.String(), ev.Seq, json.RawMessage(ev.Data))
	}
	p := TripEventPayload{EventID: ev.ID.String(), Kind: ev.Kind, Data: ev.Data}
	if ev.ActorID != nil {
		a := ev.ActorID.String()
		p.ActorID = &a
	}
	return marshalEnv("trip.event", "", ev.TripID.String(), ev.Seq, p)
}

// joinTrip subscribes the connection to a trip: authorise, enter the room, announce presence, send the
// snapshot, then replay the durable events the client has not seen (docs/design/07 §6).
func (c *Client) joinTrip(tripID uuid.UUID, lastSeq int64, ref string) error {
	ctx := c.ctx
	deps := c.h.deps
	t, m, err := deps.Trips.Access(ctx, tripID, c.user)
	if err != nil {
		return err
	}
	if t.Status == trip.StatusArchived {
		return apperr.Conflict("invalid_state", "Chuyến đi đã lưu trữ")
	}
	ts := &tripState{role: m.Role, verifiedAt: time.Now()}
	ts.sharing.Store(m.SharingState == trip.SharingOn)
	c.mu.Lock()
	c.trips[tripID] = ts
	c.mu.Unlock()

	// Enter the room BEFORE reading events: anything committed after the read is then fanned out to us,
	// anything before it is in the read; the overlap is de-duplicated by the client's event_seq cursor.
	if !c.h.shardFor(tripID).send(ctx, cmdJoin{c: c, trip: tripID}) {
		c.dropTrip(tripID)
		return apperr.Unavailable()
	}
	if on, err := deps.Live.PresenceTouch(ctx, tripID, c.user); err == nil && on {
		c.h.announcePresence(tripID, c.user, "online")
	}

	members, err := deps.Trips.Members(ctx, tripID, c.user, false)
	if err != nil {
		return err
	}
	all := make([]uuid.UUID, 0, len(members))
	sharing := make([]uuid.UUID, 0, len(members))
	for _, mm := range members {
		all = append(all, mm.UserID)
		if mm.SharingState == trip.SharingOn {
			sharing = append(sharing, mm.UserID)
		}
	}
	locs, lerr := deps.Live.Locations(ctx, tripID, sharing)
	online, oerr := deps.Live.Online(ctx, tripID, all)
	snap := make([]SnapshotMember, 0, len(members))
	for _, mm := range members {
		sm := SnapshotMember{UserID: mm.UserID.String(), DisplayName: mm.DisplayName, Color: mm.Color, Role: mm.Role,
			SharingState: mm.SharingState, Presence: "offline"}
		if online[mm.UserID] || mm.UserID == c.user {
			sm.Presence = "online"
		}
		// A member who paused sharing never has a location in the snapshot, whatever Redis still holds.
		if p, ok := locs[mm.UserID]; ok && mm.SharingState == trip.SharingOn {
			p.UserID = ""
			sm.Location = &p
		}
		snap = append(snap, sm)
	}

	page, err := deps.Trips.Events(ctx, tripID, c.user, lastSeq, 500)
	if err != nil {
		return err
	}
	replay := ReplayInfo{FromSeq: lastSeq + 1, Count: len(page.Items), Complete: page.Complete}
	if page.Expired {
		replay = ReplayInfo{FromSeq: lastSeq + 1, Complete: false}
	}
	joined := TripJoinedPayload{
		Trip: TripBrief{Status: t.Status, Name: t.Name}, HeadEventSeq: page.HeadEventSeq, Replay: replay,
		Members: snap, Degraded: lerr != nil || oerr != nil,
	}
	c.push(marshalEnv("trip.joined", ref, tripID.String(), 0, joined))
	if page.Expired || !page.Complete {
		c.push(marshalEnv("resync.required", "", tripID.String(), 0, ResyncPayload{Reason: "gap_too_large"}))
		return nil
	}
	for _, ev := range page.Items {
		if ev.Kind == trip.KindSOSCreated && ev.ActorID != nil && *ev.ActorID == c.user {
			continue // the sender does not get its own SOS pushed back
		}
		c.push(eventFrame(ev))
	}
	return nil
}
