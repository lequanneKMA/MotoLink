package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"motolink/internal/maps"
)

func TestMapEndpoints(t *testing.T) {
	e := newEnv(t)
	u := e.newUser(t, "rider")

	// nothing configured: routing/search are 503, the style is 404
	if c, _ := e.call(t, u, "POST", "/v1/route", map[string]any{"from": map[string]any{"lat": 10.77, "lng": 106.7}, "to": map[string]any{"lat": 10.8, "lng": 106.72}}); c != 503 {
		t.Fatalf("route unconfigured: %d", c)
	}
	if c, _ := e.call(t, u, "GET", "/v1/geocode?q=huế", nil); c != 503 {
		t.Fatalf("geocode unconfigured: %d", c)
	}
	if r, _ := do(t, "GET", e.srv.URL+"/v1/map/style/light", nil, nil); r.StatusCode != 404 {
		t.Fatalf("style unconfigured: %d", r.StatusCode)
	}

	osrm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":1000,"duration":120,"geometry":"xyz","legs":[{"steps":[{"distance":1000,"duration":120,"name":"A","maneuver":{"type":"depart","modifier":"left","location":[106.7,10.77]}}]}]}]}`))
	}))
	defer osrm.Close()
	e.api.Router = maps.NewRouter(osrm.URL)
	e.api.TileBase = "http://tiles.local:3000"
	for _, ddl := range []string{
		`CREATE EXTENSION IF NOT EXISTS unaccent`,
		`CREATE TABLE places (name text, kind text, rank int, lat float8, lng float8, name_norm text)`,
		`INSERT INTO places VALUES ('Nhà thờ Đức Bà','tourism',3,10.7798,106.6990,lower(unaccent('Nhà thờ Đức Bà')))`,
	} {
		if _, err := e.pool.Exec(context.Background(), ddl); err != nil {
			t.Fatal(err)
		}
	}
	e.api.Geocoder = &maps.Geocoder{DB: e.pool}

	m := e.mustCall(t, u, "POST", "/v1/route", map[string]any{"from": map[string]any{"lat": 10.77, "lng": 106.7}, "to": map[string]any{"lat": 10.8, "lng": 106.72}}, 200)
	rt := m["routes"].([]any)[0].(map[string]any)
	if rt["geometry"] != "xyz" || rt["distance_m"].(float64) != 1000 || len(rt["steps"].([]any)) != 1 {
		t.Fatalf("%v", m)
	}
	if c, _ := e.call(t, u, "POST", "/v1/route", map[string]any{"from": map[string]any{"lat": 0, "lng": 0}, "to": map[string]any{"lat": 10.8, "lng": 106.72}}); c != 400 {
		t.Fatalf("missing origin: %d", c)
	}
	g := e.mustCall(t, u, "GET", "/v1/geocode?q=duc+ba&lat=10.77&lng=106.70", nil, 200)
	items := g["items"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["name"] != "Nhà thờ Đức Bà" {
		t.Fatalf("%v", g)
	}
	// routing and search need a login; the style does not (MapLibre fetches it without our token)
	if r, _ := do(t, "POST", e.srv.URL+"/v1/route", map[string]any{}, nil); r.StatusCode != 401 {
		t.Fatalf("anonymous route: %d", r.StatusCode)
	}
	if r, _ := do(t, "GET", e.srv.URL+"/v1/map/style/dark", nil, nil); r.StatusCode != 200 || r.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("style: %d %s", r.StatusCode, r.Header.Get("Content-Type"))
	}
	if r, _ := do(t, "GET", e.srv.URL+"/v1/map/style/neon", nil, nil); r.StatusCode != 404 {
		t.Fatalf("unknown theme: %d", r.StatusCode)
	}
}
