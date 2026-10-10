package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"motolink/internal/identity"
	"motolink/internal/platform/apperr"
)

func (a *API) accountRoutes(mux *http.ServeMux) {
	mux.Handle("GET /v1/me/consents", a.authed(a.listConsents))
	mux.Handle("PUT /v1/me/consents/{kind}", a.authed(a.putConsent))
	mux.Handle("PUT /v1/devices/{device_id}", a.authed(a.putDevice))
	mux.Handle("DELETE /v1/devices/{device_id}", a.authed(a.deleteDevice))
	mux.Handle("DELETE /v1/me", a.idem(a.deleteMe))
	mux.Handle("GET /v1/me/export", a.authed(a.exportMe))
	mux.Handle("POST /v1/telemetry/client", a.authed(a.telemetry))
}

func (a *API) listConsents(w http.ResponseWriter, r *http.Request, p Principal) error {
	cs, err := a.Identity.Consents(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": cs})
	return nil
}

func (a *API) putConsent(w http.ResponseWriter, r *http.Request, p Principal) error {
	kind := r.PathValue("kind")
	var in struct {
		Version string `json:"version"`
		Granted bool   `json:"granted"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := a.Identity.SetConsent(r.Context(), p.UserID, kind, in.Version, in.Granted); err != nil {
		return err
	}
	// Withdrawing location consent stops recording right away (docs/design/11 §5.1).
	if !in.Granted && kind == "location_processing" && a.Sessions != nil {
		if err := a.Sessions.EndAllOpenForUser(r.Context(), p.UserID, "user"); err != nil {
			return err
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) putDevice(w http.ResponseWriter, r *http.Request, p Principal) error {
	did, err := uuid.Parse(r.PathValue("device_id"))
	if err != nil {
		return apperr.NotFound()
	}
	var in identity.DeviceInput
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := a.Identity.PutDevice(r.Context(), p.UserID, did, in); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (a *API) deleteDevice(w http.ResponseWriter, r *http.Request, p Principal) error {
	did, err := uuid.Parse(r.PathValue("device_id"))
	if err != nil {
		return apperr.NotFound()
	}
	if err := a.Identity.DeleteDevice(r.Context(), p.UserID, did); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// deleteMe erases the account (docs/design/05 §6). Requires the password and refuses while the user owns
// a live trip with other members: they must transfer or end it first.
func (a *API) deleteMe(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Password string `json:"password"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if err := a.limit(r.Context(), "delme:"+p.UserID.String(), 5, 15*60*1e9); err != nil {
		return err
	}
	if err := a.Identity.VerifyPassword(r.Context(), p.UserID, in.Password); err != nil {
		return err
	}
	blocked, err := a.Identity.OwnsTripsWithOthers(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	if blocked {
		return apperr.Conflict("owner_must_transfer", "Hãy chuyển quyền chủ chuyến đi hoặc kết thúc chuyến đi trước khi xóa tài khoản")
	}
	if err := a.Identity.DeleteAccount(r.Context(), p.UserID); err != nil {
		return err
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"status": "deleted"})
	return nil
}

func (a *API) exportMe(w http.ResponseWriter, r *http.Request, p Principal) error {
	data, err := a.Identity.Export(r.Context(), p.UserID)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Disposition", `attachment; filename="motolink-export.json"`)
	writeJSON(w, http.StatusOK, data)
	return nil
}

// telemetry accepts anonymous client-side quality samples. Nothing is stored: values only feed Prometheus
// histograms (docs/design/13 §5.2) and never include coordinates or trip identifiers.
func (a *API) telemetry(w http.ResponseWriter, r *http.Request, p Principal) error {
	if err := a.limit(r.Context(), "telemetry:"+p.UserID.String(), 2, 60*1e9); err != nil {
		return err
	}
	var in struct {
		LocationAgeS []float64 `json:"location_age_s"`
		AckLatencyS  []float64 `json:"ack_latency_s"`
		AppVersion   string    `json:"app_version"`
	}
	if err := decode(r, &in); err != nil {
		return err
	}
	if a.Telemetry != nil {
		a.Telemetry.Observe(in.LocationAgeS, in.AckLatencyS)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}
