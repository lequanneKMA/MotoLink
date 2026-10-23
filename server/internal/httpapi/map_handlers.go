package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"motolink/internal/maps"
	"motolink/internal/platform/apperr"
)

func (a *API) mapRoutes(mux *http.ServeMux) {
	mux.Handle("GET /v1/map/style/{theme}", a.h(a.mapStyle)) // public: MapLibre fetches it without our token
	mux.Handle("POST /v1/route", a.authed(a.route))
	mux.Handle("GET /v1/geocode", a.authed(a.geocode))
}

// mapStyle serves the generated MapLibre style; changing the tile server only needs a server restart, not a new app.
func (a *API) mapStyle(w http.ResponseWriter, r *http.Request) error {
	if a.TileBase == "" {
		return apperr.NotFound()
	}
	raw, err := maps.Style(r.PathValue("theme"), a.TileBase)
	if err != nil {
		return apperr.NotFound()
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(raw)
	return nil
}

type routeRequest struct {
	From         maps.LatLng `json:"from"`
	To           maps.LatLng `json:"to"`
	Alternatives bool        `json:"alternatives"`
}

// route proxies OSRM so the app never needs to know where it runs (and tokens/rate limits apply).
func (a *API) route(w http.ResponseWriter, r *http.Request, p Principal) error {
	if a.Router == nil {
		return apperr.Unavailable()
	}
	if err := a.limit(r.Context(), "route:"+p.UserID.String(), 60, time.Minute); err != nil {
		return err
	}
	var in routeRequest
	if err := decode(r, &in); err != nil {
		return err
	}
	if in.From.Lat == 0 && in.From.Lng == 0 || in.To.Lat == 0 && in.To.Lng == 0 {
		return apperr.BadRequest("thiếu điểm đi hoặc điểm đến")
	}
	routes, err := a.Router.Routes(r.Context(), in.From, in.To, in.Alternatives)
	switch {
	case errors.Is(err, maps.ErrNoRoute):
		return apperr.Conflict("no_route", "Không tìm thấy đường đi")
	case errors.Is(err, maps.ErrUnavailable):
		a.Log.Warn("routing backend unavailable", "err", err)
		return apperr.Unavailable()
	case err != nil:
		return apperr.BadRequest("toạ độ không hợp lệ")
	}
	writeJSON(w, http.StatusOK, map[string]any{"routes": routes})
	return nil
}

func (a *API) geocode(w http.ResponseWriter, r *http.Request, p Principal) error {
	if a.Geocoder == nil {
		return apperr.Unavailable()
	}
	if err := a.limit(r.Context(), "geocode:"+p.UserID.String(), 90, time.Minute); err != nil {
		return err
	}
	q := r.URL.Query()
	var near *maps.LatLng
	if lat, e1 := strconv.ParseFloat(q.Get("lat"), 64); e1 == nil {
		if lng, e2 := strconv.ParseFloat(q.Get("lng"), 64); e2 == nil {
			near = &maps.LatLng{Lat: lat, Lng: lng}
		}
	}
	places, err := a.Geocoder.Search(r.Context(), q.Get("q"), near, 8)
	if err != nil {
		a.Log.Warn("place search failed", "err", err)
		return apperr.Unavailable()
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": places})
	return nil
}
