package realtime

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	locTTL      = 15 * time.Minute // ml:loc:* (docs/design/08 §2)
	presenceTTL = 45 * time.Second // ml:conn:*, presence score window
)

// LiveStore keeps the volatile live state: latest position and presence (Redis in production,
// docs/design/08). Everything here may be lost at any time; correctness never depends on it.
type LiveStore interface {
	// SetLocation stores p only if it is newer by (session_id, seq). accepted=false means stale or duplicate.
	SetLocation(ctx context.Context, tripID, userID uuid.UUID, p LivePoint) (accepted bool, err error)
	Locations(ctx context.Context, tripID uuid.UUID, users []uuid.UUID) (map[uuid.UUID]LivePoint, error)
	DeleteLocation(ctx context.Context, tripID, userID uuid.UUID) error
	DeleteTripLocations(ctx context.Context, tripID uuid.UUID, users []uuid.UUID) error

	// ClaimConn registers connID as the user's one live connection; prev is the replaced "instance|conn", if any.
	ClaimConn(ctx context.Context, userID uuid.UUID, instanceID, connID string) (prev string, err error)
	// TouchConn refreshes the claim; false means the claim was lost (superseded).
	TouchConn(ctx context.Context, userID uuid.UUID, instanceID, connID string) (bool, error)
	// PresenceTouch marks the user online in the trip; becameOnline is true on offline->online.
	PresenceTouch(ctx context.Context, tripID, userID uuid.UUID) (becameOnline bool, err error)
	// ConnLeave drops the claim (if still ours) and removes presence from the given trips; returns the trips
	// the user was actually removed from (these must emit "offline").
	ConnLeave(ctx context.Context, userID uuid.UUID, instanceID, connID string, trips []uuid.UUID) ([]uuid.UUID, error)
	Online(ctx context.Context, tripID uuid.UUID, users []uuid.UUID) (map[uuid.UUID]bool, error)
	// PresenceRemove drops one user's presence in one trip (membership ended).
	PresenceRemove(ctx context.Context, tripID, userID uuid.UUID) error
	// ReapPresence removes expired presence entries of a trip and returns the users removed by this call.
	ReapPresence(ctx context.Context, tripID uuid.UUID) ([]uuid.UUID, error)
}

// ---- in-memory implementation: single instance, tests, and Redis-less deployments ----

type memStore struct {
	mu    sync.Mutex
	now   func() time.Time
	locs  map[string]memLoc
	conns map[uuid.UUID]memConn
	pres  map[uuid.UUID]map[uuid.UUID]time.Time // trip -> user -> expiry
}
type memLoc struct {
	p   LivePoint
	exp time.Time
}
type memConn struct {
	val string
	exp time.Time
}

func NewMemStore() LiveStore {
	return &memStore{now: time.Now, locs: map[string]memLoc{}, conns: map[uuid.UUID]memConn{}, pres: map[uuid.UUID]map[uuid.UUID]time.Time{}}
}

func locKey(t, u uuid.UUID) string { return t.String() + "/" + u.String() }

func (m *memStore) SetLocation(_ context.Context, t, u uuid.UUID, p LivePoint) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := locKey(t, u)
	if cur, ok := m.locs[k]; ok && m.now().Before(cur.exp) && !p.newer(cur.p) {
		return false, nil
	}
	m.locs[k] = memLoc{p: p, exp: m.now().Add(locTTL)}
	return true, nil
}

func (m *memStore) Locations(_ context.Context, t uuid.UUID, users []uuid.UUID) (map[uuid.UUID]LivePoint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[uuid.UUID]LivePoint{}
	for _, u := range users {
		if l, ok := m.locs[locKey(t, u)]; ok && m.now().Before(l.exp) {
			out[u] = l.p
		}
	}
	return out, nil
}

func (m *memStore) DeleteLocation(_ context.Context, t, u uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.locs, locKey(t, u))
	return nil
}

func (m *memStore) DeleteTripLocations(ctx context.Context, t uuid.UUID, users []uuid.UUID) error {
	for _, u := range users {
		_ = m.DeleteLocation(ctx, t, u)
	}
	return nil
}

func (m *memStore) ClaimConn(_ context.Context, u uuid.UUID, inst, conn string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev := ""
	if c, ok := m.conns[u]; ok && m.now().Before(c.exp) {
		prev = c.val
	}
	m.conns[u] = memConn{val: inst + "|" + conn, exp: m.now().Add(presenceTTL)}
	return prev, nil
}

func (m *memStore) TouchConn(_ context.Context, u uuid.UUID, inst, conn string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[u]
	if !ok || c.val != inst+"|"+conn {
		return false, nil
	}
	m.conns[u] = memConn{val: c.val, exp: m.now().Add(presenceTTL)}
	return true, nil
}

func (m *memStore) PresenceTouch(_ context.Context, t, u uuid.UUID) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pres[t] == nil {
		m.pres[t] = map[uuid.UUID]time.Time{}
	}
	prev, ok := m.pres[t][u]
	m.pres[t][u] = m.now().Add(presenceTTL)
	return !ok || !m.now().Before(prev), nil
}

func (m *memStore) ConnLeave(_ context.Context, u uuid.UUID, inst, conn string, trips []uuid.UUID) ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.conns[u]
	if !ok || c.val != inst+"|"+conn {
		return nil, nil // superseded: the new connection owns presence
	}
	delete(m.conns, u)
	var removed []uuid.UUID
	for _, t := range trips {
		if _, ok := m.pres[t][u]; ok {
			delete(m.pres[t], u)
			removed = append(removed, t)
		}
	}
	return removed, nil
}

func (m *memStore) Online(_ context.Context, t uuid.UUID, users []uuid.UUID) (map[uuid.UUID]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[uuid.UUID]bool{}
	for _, u := range users {
		if exp, ok := m.pres[t][u]; ok && m.now().Before(exp) {
			out[u] = true
		}
	}
	return out, nil
}

func (m *memStore) ReapPresence(_ context.Context, t uuid.UUID) ([]uuid.UUID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []uuid.UUID
	for u, exp := range m.pres[t] {
		if !m.now().Before(exp) {
			delete(m.pres[t], u)
			out = append(out, u)
		}
	}
	return out, nil
}

func (m *memStore) PresenceRemove(_ context.Context, t, u uuid.UUID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.pres[t], u)
	return nil
}
