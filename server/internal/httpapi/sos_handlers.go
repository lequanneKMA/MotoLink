package httpapi

import (
	"net/http"

	"motolink/internal/sos"
)

func (a *API) sosRoutes(mux *http.ServeMux) {
	mux.Handle("POST /v1/trips/{id}/sos", a.authed(a.createSOS))
	mux.Handle("GET /v1/trips/{id}/sos", a.authed(a.listSOS))
	mux.Handle("GET /v1/sos/{sos_id}", a.authed(a.getSOS))
	mux.Handle("POST /v1/sos/{sos_id}/receipt", a.authed(a.sosReceipt))
	mux.Handle("POST /v1/sos/{sos_id}/resolve", a.authed(a.resolveSOS))
}

// createSOS is naturally idempotent on (user, client_sos_id), so it does not need the Idempotency-Key cache.
func (a *API) createSOS(w http.ResponseWriter, r *http.Request, p Principal) error {
	tid, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	var in sos.CreateInput
	if err := decode(r, &in); err != nil {
		return err
	}
	res, err := a.SOS.Create(r.Context(), p.UserID, tid, in)
	if err != nil {
		return err
	}
	code := http.StatusCreated
	if res.Duplicate {
		code = http.StatusOK
	}
	writeJSON(w, code, res)
	return nil
}

func (a *API) listSOS(w http.ResponseWriter, r *http.Request, p Principal) error {
	tid, err := pathUUID(r, "id")
	if err != nil {
		return err
	}
	items, err := a.SOS.List(r.Context(), p.UserID, tid, r.URL.Query().Get("status"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next_cursor": nil})
	return nil
}

func (a *API) getSOS(w http.ResponseWriter, r *http.Request, p Principal) error {
	sid, err := pathUUID(r, "sos_id")
	if err != nil {
		return err
	}
	x, err := a.SOS.Get(r.Context(), p.UserID, sid)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"sos": x})
	return nil
}

func (a *API) sosReceipt(w http.ResponseWriter, r *http.Request, p Principal) error {
	sid, err := pathUUID(r, "sos_id")
	if err != nil {
		return err
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := a.SOS.Receipt(r.Context(), p.UserID, sid, in.Status); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) resolveSOS(w http.ResponseWriter, r *http.Request, p Principal) error {
	sid, err := pathUUID(r, "sos_id")
	if err != nil {
		return err
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	x, err := a.SOS.Resolve(r.Context(), p.UserID, sid, in.Status)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"sos": x})
	return nil
}
