// Package maps holds the self-hosted map stack of the app: routing through OSRM, place search over an OSM import in
// PostGIS, and the MapLibre style served to clients (OSM → OpenMapTiles vector tiles, docs/design-ui + deploy/maps).
package maps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// LatLng is a WGS84 position.
type LatLng struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

func (p LatLng) valid() bool {
	return p.Lat >= -90 && p.Lat <= 90 && p.Lng >= -180 && p.Lng <= 180 && !(p.Lat == 0 && p.Lng == 0)
}

// Step is one manoeuvre of a route; the text is composed on the client (it knows the language).
type Step struct {
	DistanceM float64 `json:"distance_m"`
	DurationS float64 `json:"duration_s"`
	Name      string  `json:"name"`
	Type      string  `json:"type"`     // OSRM manoeuvre type: turn, roundabout, arrive, ...
	Modifier  string  `json:"modifier"` // left, slight right, uturn, ...
	Exit      int     `json:"exit,omitempty"`
	At        LatLng  `json:"at"`
}

// Route is a normalised OSRM route. Geometry is an encoded polyline with 6 decimals (precision 1e6).
type Route struct {
	DistanceM float64 `json:"distance_m"`
	DurationS float64 `json:"duration_s"`
	Geometry  string  `json:"geometry"`
	Steps     []Step  `json:"steps"`
}

// ErrNoRoute means OSRM found no path between the points; ErrUnavailable that the routing backend is off or failing.
var (
	ErrNoRoute     = errors.New("maps: no route")
	ErrUnavailable = errors.New("maps: routing unavailable")
)

// Router talks to an OSRM server (osrm-routed). The base URL is the server root, e.g. http://osrm:5000.
type Router struct {
	Base string
	HC   *http.Client
}

func NewRouter(base string) *Router {
	return &Router{Base: strings.TrimRight(base, "/"), HC: &http.Client{Timeout: 8 * time.Second}}
}

type osrmResp struct {
	Code   string `json:"code"`
	Routes []struct {
		Distance float64 `json:"distance"`
		Duration float64 `json:"duration"`
		Geometry string  `json:"geometry"`
		Legs     []struct {
			Steps []struct {
				Distance float64 `json:"distance"`
				Duration float64 `json:"duration"`
				Name     string  `json:"name"`
				Maneuver struct {
					Type     string    `json:"type"`
					Modifier string    `json:"modifier"`
					Exit     int       `json:"exit"`
					Location []float64 `json:"location"` // [lon, lat]
				} `json:"maneuver"`
			} `json:"steps"`
		} `json:"legs"`
	} `json:"routes"`
}

// Routes returns up to three alternatives from→to. Motorbikes may not use expressways in Vietnam, so the query
// excludes motorways; servers whose profile has no such class answer 400 and the call is repeated without it.
func (r *Router) Routes(ctx context.Context, from, to LatLng, alternatives bool) ([]Route, error) {
	if r == nil || r.Base == "" {
		return nil, ErrUnavailable
	}
	if !from.valid() || !to.valid() {
		return nil, fmt.Errorf("maps: invalid coordinates")
	}
	out, status, err := r.query(ctx, from, to, alternatives, true)
	if status == http.StatusBadRequest {
		out, status, err = r.query(ctx, from, to, alternatives, false)
	}
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK && out.Code != "NoRoute" {
		return nil, ErrUnavailable
	}
	if out.Code == "NoRoute" || len(out.Routes) == 0 {
		return nil, ErrNoRoute
	}
	return normalize(out), nil
}

func (r *Router) query(ctx context.Context, from, to LatLng, alt, excludeMotorway bool) (*osrmResp, int, error) {
	q := url.Values{"steps": {"true"}, "overview": {"full"}, "geometries": {"polyline6"}, "annotations": {"false"}}
	if alt {
		q.Set("alternatives", "2")
	}
	if excludeMotorway {
		q.Set("exclude", "motorway")
	}
	u := fmt.Sprintf("%s/route/v1/driving/%.6f,%.6f;%.6f,%.6f?%s", r.Base, from.Lng, from.Lat, to.Lng, to.Lat, q.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, 0, err
	}
	resp, err := r.HC.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer resp.Body.Close()
	var out osrmResp
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err := json.Unmarshal(body, &out); err != nil && resp.StatusCode == http.StatusOK {
		return nil, resp.StatusCode, fmt.Errorf("%w: bad response", ErrUnavailable)
	}
	return &out, resp.StatusCode, nil
}

func normalize(o *osrmResp) []Route {
	routes := make([]Route, 0, len(o.Routes))
	for _, rt := range o.Routes {
		n := Route{DistanceM: rt.Distance, DurationS: rt.Duration, Geometry: rt.Geometry}
		for _, leg := range rt.Legs {
			for _, st := range leg.Steps {
				s := Step{DistanceM: st.Distance, DurationS: st.Duration, Name: st.Name, Type: st.Maneuver.Type, Modifier: st.Maneuver.Modifier, Exit: st.Maneuver.Exit}
				if len(st.Maneuver.Location) == 2 {
					s.At = LatLng{Lat: st.Maneuver.Location[1], Lng: st.Maneuver.Location[0]}
				}
				n.Steps = append(n.Steps, s)
			}
		}
		routes = append(routes, n)
	}
	return routes
}
