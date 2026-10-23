package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/google/uuid"

	"motolink/internal/maps"
	"motolink/internal/platform/id"
	"motolink/internal/realtime"
	"motolink/internal/trip"
)

// Golden contract (M0-T05): the JSON the server really produces for the endpoints the Android app consumes, with
// volatile values replaced by stable ones. The Android unit test decodes the same files with its DTOs, so a field
// rename or type change on either side fails a test instead of the first field trial.
// Regenerate with: go test ./internal/httpapi -run TestGoldenContract -update
var update = flag.Bool("update", false, "rewrite testdata/contract")

var (
	rfc3339 = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$`)
	month   = regexp.MustCompile(`^\d{4}-\d{2}-01$`)
)

// normalize makes a decoded response reproducible: uuids numbered by first appearance, times fixed, epoch-ms fixed.
func normalize(v any, ids map[string]string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys) // stable numbering of uuids: first appearance in key order
		for _, k := range keys {
			e := x[k]
			if k == "code" {
				if s, ok := e.(string); ok && len(s) >= 6 && len(s) <= 16 {
					out[k] = "ABCD2345"
					continue
				}
			}
			out[k] = normalize(e, ids)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalize(e, ids)
		}
		return out
	case string:
		if _, err := uuid.Parse(x); err == nil {
			if n, ok := ids[x]; ok {
				return n
			}
			n := fmt.Sprintf("00000000-0000-7000-8000-%012d", len(ids)+1)
			ids[x] = n
			return n
		}
		if rfc3339.MatchString(x) {
			return "2026-01-01T00:00:00Z"
		}
		if month.MatchString(x) {
			return "2026-01-01"
		}
		return x
	case float64:
		if x > 1e11 { // epoch milliseconds
			return float64(1760000000000)
		}
		return x
	default:
		return v
	}
}

func TestGoldenContract(t *testing.T) {
	e := newEnv(t)
	ids := map[string]string{}
	save := func(name string, m map[string]any) {
		t.Helper()
		got, err := json.MarshalIndent(normalize(m, ids), "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, '\n')
		path := filepath.Join("..", "..", "testdata", "contract", name+".json")
		if *update {
			if err := os.WriteFile(path, got, 0o644); err != nil {
				t.Fatal(err)
			}
			return
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s missing (run with -update): %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("contract %s changed (run with -update and review the diff; the Android DTOs must follow):\n--- got\n%s\n--- want\n%s", name, got, want)
		}
	}

	osrm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"Ok","routes":[{"distance":4300.5,"duration":720,"geometry":"_p~iF~ps|U_ulLnnqC_mqNvxq` + "`" + `@","legs":[{"steps":[{"distance":300,"duration":40,"name":"Nguyễn Huệ","maneuver":{"type":"turn","modifier":"right","location":[106.70,10.77]}},{"distance":0,"duration":0,"name":"Lê Lợi","maneuver":{"type":"arrive","location":[106.72,10.79]}}]}]}]}`))
	}))
	defer osrm.Close()
	e.api.Router = maps.NewRouter(osrm.URL)
	e.api.TileBase = "http://tiles.local:3000"
	e.api.Client.TileStyleURL, e.api.Client.RoutingOn, e.api.Client.SearchOn = "/v1/map/style/{theme}", true, true
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

	owner, a := e.newUser(t, "owner"), e.newUser(t, "an")
	save("route", e.mustCall(t, a, "POST", "/v1/route", map[string]any{"from": map[string]any{"lat": 10.77, "lng": 106.7}, "to": map[string]any{"lat": 10.79, "lng": 106.72}, "alternatives": true}, 200))
	save("geocode", e.mustCall(t, a, "GET", "/v1/geocode?q=duc+ba", nil, 200))
	_, cfg := do(t, "GET", e.srv.URL+"/v1/config", nil, nil)
	save("config", cfg)
	e.consent(t, owner)
	e.consent(t, a)

	tr := e.mustCall(t, owner, "POST", "/v1/trips", map[string]any{"name": "Đà Lạt"}, 201)
	save("trip_create", tr)
	tid := tr["trip"].(map[string]any)["id"].(string)
	save("trip_get", e.mustCall(t, owner, "GET", "/v1/trips/"+tid, nil, 200))

	inv := e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/invitations", map[string]any{}, 201)
	save("invitation_create", inv)
	code := inv["code"].(string)
	save("invitation_resolve", e.mustCall(t, a, "POST", "/v1/invitations/resolve", map[string]any{"code": code}, 200))
	save("trip_join", e.mustCall(t, a, "POST", "/v1/trip-join", map[string]any{"code": code}, 200))
	save("members", e.mustCall(t, owner, "GET", "/v1/trips/"+tid+"/members", nil, 200))
	save("trip_start", e.mustCall(t, owner, "POST", "/v1/trips/"+tid+"/start", nil, 200))
	save("member_patch", e.mustCall(t, a, "PATCH", "/v1/trips/"+tid+"/members/me", map[string]any{"sharing_state": "paused"}, 200))
	e.mustCall(t, a, "PATCH", "/v1/trips/"+tid+"/members/me", map[string]any{"sharing_state": "sharing"}, 200)

	ses := e.mustCall(t, a, "POST", "/v1/trips/"+tid+"/tracking-sessions", map[string]any{}, 201)
	save("session_start", ses)
	sid := ses["session"].(map[string]any)["id"].(string)
	save("bootstrap", e.mustCall(t, a, "GET", "/v1/sync/bootstrap", nil, 200))
	save("snapshot", e.mustCall(t, a, "GET", "/v1/trips/"+tid+"/snapshot", nil, 200))
	save("session_pause", e.mustCall(t, a, "POST", "/v1/tracking-sessions/"+sid+"/pause", nil, 200))

	e.api.Points = &memPoints{}
	save("points", e.mustCall(t, a, "POST", "/v1/tracking-sessions/"+sid+"/points", map[string]any{"points": []any{
		map[string]any{"seq": 1, "client_ts": 1760000000000, "lat": 10.77, "lng": 106.7, "accuracy_m": 8},
		map[string]any{"seq": 2, "client_ts": 1760000001000, "lat": 99, "lng": 106.7, "accuracy_m": 8},
	}}, 200))

	sos := e.mustCall(t, a, "POST", "/v1/trips/"+tid+"/sos", map[string]any{"client_sos_id": id.NewString(), "kind": "breakdown", "message": "Hỏng xe", "lat": 11.94, "lng": 108.45, "accuracy_m": 12}, 201)
	save("sos_create", sos)
	save("sos_list", e.mustCall(t, a, "GET", "/v1/trips/"+tid+"/sos", nil, 200))

	evs, err := trip.ListEvents(context.Background(), e.pool, uuid.MustParse(tid), 0, 100, time_zero())
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range evs {
		if err := e.notif.OnEvent(context.Background(), ev); err != nil {
			t.Fatal(err)
		}
	}
	save("notifications", e.mustCall(t, owner, "GET", "/v1/notifications", nil, 200))
}

type memPoints struct{}

func (*memPoints) WritePoints(context.Context, []realtime.DurablePoint) error { return nil }
