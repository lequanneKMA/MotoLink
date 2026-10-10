package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"motolink/internal/tracking"
)

func (a *API) sessionRoutes(mux *http.ServeMux) {
	mux.Handle("POST /v1/trips/{id}/tracking-sessions", a.idem(a.startSession))
	mux.Handle("GET /v1/trips/{id}/tracking-sessions", a.authed(a.listSessions))
	mux.Handle("GET /v1/tracking-sessions/{sid}", a.authed(a.getSession))
	mux.Handle("POST /v1/tracking-sessions/{sid}/pause", a.idem(a.sessionOp((*tracking.Service).Pause)))
	mux.Handle("POST /v1/tracking-sessions/{sid}/resume", a.idem(a.sessionOp((*tracking.Service).Resume)))
	mux.Handle("POST /v1/tracking-sessions/{sid}/end", a.idem(a.sessionOp((*tracking.Service).End)))
	mux.Handle("GET /v1/tracking-sessions/{sid}/route", a.authed(a.sessionRoute))
}

func (a *API) startSession(w http.ResponseWriter, r *http.Request, p Principal) error {
	tid, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in struct {
		DeviceID *uuid.UUID `json:"device_id"`
	}
	if r.ContentLength != 0 {
		if err := decode(r, &in); err != nil {
			return err
		}
	}
	s, err := a.Sessions.Start(r.Context(), p.UserID, tid, in.DeviceID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, map[string]any{"session": s})
	return nil
}

func (a *API) listSessions(w http.ResponseWriter, r *http.Request, p Principal) error {
	tid, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var uid *uuid.UUID
	if v := r.URL.Query().Get("user_id"); v != "" {
		u, err := uuid.Parse(v)
		if err != nil {
			return pathErr()
		}
		uid = &u
	}
	items, err := a.Sessions.ListForTrip(r.Context(), p.UserID, tid, uid)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
	return nil
}

func (a *API) getSession(w http.ResponseWriter, r *http.Request, p Principal) error {
	sid, err := pathUUID(r, "sid")
	if err != nil {
		return err
	}
	s, err := a.Sessions.Get(r.Context(), p.UserID, sid)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"session": s})
	return nil
}

func (a *API) sessionOp(op func(*tracking.Service, ctxT, uuid.UUID, uuid.UUID) (*tracking.Session, error)) authedFunc {
	return func(w http.ResponseWriter, r *http.Request, p Principal) error {
		sid, err := pathUUID(r, "sid")
		if err != nil {
			return err
		}
		s, err := op(a.Sessions, r.Context(), p.UserID, sid)
		if err != nil {
			return err
		}
		writeJSON(w, http.StatusOK, map[string]any{"session": s})
		return nil
	}
}

func (a *API) sessionRoute(w http.ResponseWriter, r *http.Request, p Principal) error {
	sid, err := pathUUID(r, "sid")
	if err != nil {
		return err
	}
	var simplify float64
	if v := r.URL.Query().Get("simplify_m"); v != "" {
		simplify = float64(queryInt(r, "simplify_m", 0))
	}
	res, err := a.Sessions.Route(r.Context(), p.UserID, sid, simplify, r.URL.Query().Get("format"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}
