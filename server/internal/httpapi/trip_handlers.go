package httpapi

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"motolink/internal/platform/apperr"
	"motolink/internal/trip"
)

func (a *API) tripRoutes(mux *http.ServeMux) {
	mux.Handle("POST /v1/trips", a.idem(a.createTrip))
	mux.Handle("GET /v1/trips", a.authed(a.listTrips))
	mux.Handle("GET /v1/trips/{id}", a.authed(a.getTrip))
	mux.Handle("PATCH /v1/trips/{id}", a.authed(a.patchTrip))
	mux.Handle("POST /v1/trips/{id}/start", a.idem(a.startTrip))
	mux.Handle("POST /v1/trips/{id}/end", a.idem(a.endTrip))
	mux.Handle("POST /v1/trips/{id}/archive", a.idem(a.archiveTrip))
	mux.Handle("GET /v1/trips/{id}/snapshot", a.authed(a.tripSnapshot))
	mux.Handle("GET /v1/trips/{id}/events", a.authed(a.tripEvents))

	mux.Handle("POST /v1/trips/{id}/invitations", a.idem(a.createInvitation))
	mux.Handle("GET /v1/trips/{id}/invitations", a.authed(a.listInvitations))
	mux.Handle("DELETE /v1/trips/{id}/invitations/{inv}", a.authed(a.revokeInvitation))
	mux.Handle("POST /v1/invitations/resolve", a.authed(a.resolveInvitation))
	mux.Handle("POST /v1/trip-join", a.idem(a.joinTrip))

	mux.Handle("GET /v1/trips/{id}/members", a.authed(a.listMembers))
	mux.Handle("PATCH /v1/trips/{id}/members/me", a.authed(a.patchMember))
	mux.Handle("DELETE /v1/trips/{id}/members/me", a.idem(a.leaveTrip))
	mux.Handle("DELETE /v1/trips/{id}/members/{user_id}", a.idem(a.removeMember))
	mux.Handle("POST /v1/trips/{id}/transfer-ownership", a.idem(a.transferOwnership))
	mux.Handle("GET /v1/sync/bootstrap", a.authed(a.bootstrap))
}

func (a *API) createTrip(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in trip.CreateInput
	if err := decode(r, &in); err != nil {
		return err
	}
	t, err := a.Trips.Create(r.Context(), p.UserID, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"trip": t})
	return nil
}

func (a *API) listTrips(w http.ResponseWriter, r *http.Request, p Principal) error {
	items, err := a.Trips.List(r.Context(), p.UserID, r.URL.Query().Get("status"), false)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
	return nil
}

func (a *API) getTrip(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	t, m, err := a.Trips.Access(r.Context(), id, p.UserID)
	if err != nil {
		return err
	}
	members, err := a.Trips.Members(r.Context(), id, p.UserID, false)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"trip": t, "my_membership": m, "member_count": len(members)})
	return nil
}

func (a *API) patchTrip(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in trip.UpdateInput
	if err := decode(r, &in); err != nil {
		return err
	}
	t, err := a.Trips.Update(r.Context(), id, p.UserID, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"trip": t})
	return nil
}

func (a *API) tripAction(fn func(*trip.Service) func(ctxT, uuid.UUID, uuid.UUID) (*trip.Trip, error)) authedFunc {
	return func(w http.ResponseWriter, r *http.Request, p Principal) error {
		id, err := pathUUID(r, "id")
		if err != nil {
			return err
		}
		t, err := fn(a.Trips)(r.Context(), id, p.UserID)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"trip": t})
		return nil
	}
}

func (a *API) startTrip(w http.ResponseWriter, r *http.Request, p Principal) error {
	return a.tripAction(func(s *trip.Service) func(ctxT, uuid.UUID, uuid.UUID) (*trip.Trip, error) { return s.Start })(w, r, p)
}
func (a *API) endTrip(w http.ResponseWriter, r *http.Request, p Principal) error {
	return a.tripAction(func(s *trip.Service) func(ctxT, uuid.UUID, uuid.UUID) (*trip.Trip, error) { return s.End })(w, r, p)
}
func (a *API) archiveTrip(w http.ResponseWriter, r *http.Request, p Principal) error {
	return a.tripAction(func(s *trip.Service) func(ctxT, uuid.UUID, uuid.UUID) (*trip.Trip, error) { return s.Archive })(w, r, p)
}

func (a *API) tripSnapshot(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	snap, err := a.Trips.Snapshot(r.Context(), id, p.UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, snap)
	return nil
}

func (a *API) tripEvents(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	page, err := a.Trips.Events(r.Context(), id, p.UserID, queryInt64(r, "after_seq", 0), queryInt(r, "limit", 500))
	if err != nil {
		return err
	}
	if page.Expired {
		return apperr.Gone("events_expired", "Sự kiện đã quá cũ, hãy đồng bộ lại từ snapshot")
	}
	writeJSON(w, http.StatusOK, page)
	return nil
}

// ---- invitations ----

func (a *API) createInvitation(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in trip.InviteInput
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			return err
		}
	}
	inv, err := a.Trips.CreateInvitation(r.Context(), id, p.UserID, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, inv)
	return nil
}

func (a *API) listInvitations(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	items, err := a.Trips.ListInvitations(r.Context(), id, p.UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (a *API) revokeInvitation(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	inv, err := pathUUID(r, "inv")
	if err != nil {
		return err
	}
	if err := a.Trips.RevokeInvitation(r.Context(), id, p.UserID, inv); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// codeAttempts bounds guesses per user and per IP (docs/design/06 §2.4). Every attempt counts.
func (a *API) codeAttempts(r *http.Request, p Principal) error {
	if err := a.limit(r.Context(), "code:u:"+p.UserID.String(), 10, time.Hour); err != nil {
		return err
	}
	return a.limit(r.Context(), "code:ip:"+a.clientIP(r), 30, time.Hour)
}

type codeBody struct {
	Code string `json:"code"`
}

func (a *API) resolveInvitation(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := a.codeAttempts(r, p); err != nil {
		return err
	}
	var in codeBody
	if err := decode(r, &in); err != nil {
		return err
	}
	pv, err := a.Trips.Resolve(r.Context(), in.Code)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, pv)
	return nil
}

func (a *API) joinTrip(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := a.codeAttempts(r, p); err != nil {
		return err
	}
	var in codeBody
	if err := decode(r, &in); err != nil {
		return err
	}
	res, err := a.Trips.Join(r.Context(), p.UserID, in.Code)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}

// ---- members ----

func (a *API) listMembers(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	items, err := a.Trips.Members(r.Context(), id, p.UserID, r.URL.Query().Get("include_left") == "true")
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
	return nil
}

func (a *API) patchMember(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in trip.MemberPatch
	if err := decode(r, &in); err != nil {
		return err
	}
	m, err := a.Trips.PatchMe(r.Context(), id, p.UserID, in)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"membership": m})
	return nil
}

func (a *API) leaveTrip(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := a.Trips.Leave(r.Context(), id, p.UserID); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) removeMember(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	target, err := pathUUID(r, "user_id")
	if err != nil {
		return err
	}
	if err := a.Trips.Remove(r.Context(), id, p.UserID, target); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) transferOwnership(w http.ResponseWriter, r *http.Request, p Principal) error {
	id, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		UserID uuid.UUID `json:"user_id"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := a.Trips.TransferOwnership(r.Context(), id, p.UserID, in.UserID); err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return nil
}

func (a *API) bootstrap(w http.ResponseWriter, r *http.Request, p Principal) error {
	items, err := a.Trips.Bootstrap(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"trips": items, "server_time": time.Now().UnixMilli()})
	return nil
}
