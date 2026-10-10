package realtime

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"motolink/internal/trip"
)

// BusFanout publishes committed events to the bus only. It is used by processes that run the outbox relay
// without hosting WebSocket connections (ROLES=worker); every ws instance receives the frame through Pub/Sub.
type BusFanout struct{ Bus Bus }

func (f BusFanout) PublishEvent(ctx context.Context, ev trip.Event) {
	var except uuid.UUID
	if ev.Kind == trip.KindSOSCreated && ev.ActorID != nil {
		except = *ev.ActorID
	}
	m := busMsg{O: "relay", K: kFrame, T: ev.TripID.String(), F: eventFrame(ev)}
	if except != uuid.Nil {
		m.U = except.String()
	}
	b, _ := json.Marshal(m)
	_ = f.Bus.Publish(ctx, tripChannel(ev.TripID), b)
}
