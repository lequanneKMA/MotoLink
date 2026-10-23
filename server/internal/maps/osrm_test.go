package maps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const okBody = `{"code":"Ok","routes":[
 {"distance":4300.5,"duration":720,"geometry":"abc","legs":[{"steps":[
   {"distance":300,"duration":40,"name":"Nguyễn Huệ","maneuver":{"type":"depart","modifier":"right","location":[106.70,10.77]}},
   {"distance":4000,"duration":680,"name":"","maneuver":{"type":"roundabout","modifier":"right","exit":2,"location":[106.71,10.78]}},
   {"distance":0,"duration":0,"name":"Lê Lợi","maneuver":{"type":"arrive","location":[106.72,10.79]}}]}]},
 {"distance":5100,"duration":700,"geometry":"def","legs":[{"steps":[]}]}]}`

func TestRoutesNormalisesAndExcludesMotorways(t *testing.T) {
	var gotQuery, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery, gotPath = r.URL.RawQuery, r.URL.Path
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	rs, err := NewRouter(srv.URL).Routes(context.Background(), LatLng{10.77, 106.70}, LatLng{10.79, 106.72}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 || rs[0].DistanceM != 4300.5 || rs[0].Geometry != "abc" || len(rs[0].Steps) != 3 {
		t.Fatalf("%+v", rs)
	}
	s := rs[0].Steps[1]
	if s.Type != "roundabout" || s.Exit != 2 || s.At.Lat != 10.78 || s.At.Lng != 106.71 {
		t.Fatalf("%+v", s)
	}
	if !strings.Contains(gotQuery, "exclude=motorway") || !strings.Contains(gotQuery, "geometries=polyline6") || !strings.Contains(gotQuery, "alternatives=2") {
		t.Fatalf("query %q", gotQuery)
	}
	if gotPath != "/route/v1/driving/106.700000,10.770000;106.720000,10.790000" { // OSRM wants lon,lat
		t.Fatalf("path %q", gotPath)
	}
}

func TestRoutesRetriesWithoutExcludeWhenProfileHasNoSuchClass(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if strings.Contains(r.URL.RawQuery, "exclude=") {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":"InvalidValue"}`))
			return
		}
		_, _ = w.Write([]byte(okBody))
	}))
	defer srv.Close()
	rs, err := NewRouter(srv.URL).Routes(context.Background(), LatLng{10.77, 106.70}, LatLng{10.79, 106.72}, false)
	if err != nil || len(rs) != 2 || calls.Load() != 2 {
		t.Fatalf("rs=%d err=%v calls=%d", len(rs), err, calls.Load())
	}
}

func TestRoutesErrors(t *testing.T) {
	if _, err := (*Router)(nil).Routes(context.Background(), LatLng{1, 1}, LatLng{2, 2}, false); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("nil router: %v", err)
	}
	noRoute := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"NoRoute","routes":[]}`))
	}))
	defer noRoute.Close()
	if _, err := NewRouter(noRoute.URL).Routes(context.Background(), LatLng{10, 106}, LatLng{11, 107}, false); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("no route: %v", err)
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	down.Close()
	if _, err := NewRouter(down.URL).Routes(context.Background(), LatLng{10, 106}, LatLng{11, 107}, false); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("down: %v", err)
	}
	if _, err := NewRouter("http://x").Routes(context.Background(), LatLng{0, 0}, LatLng{11, 107}, false); err == nil {
		t.Fatal("invalid coordinates accepted")
	}
}
