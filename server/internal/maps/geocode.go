package maps

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Place is a search result.
type Place struct {
	Name string  `json:"name"`
	Kind string  `json:"kind"`
	Lat  float64 `json:"lat"`
	Lng  float64 `json:"lng"`
}

// ErrNoSearch means place search is not configured.
var ErrNoSearch = errors.New("maps: place search unavailable")

// Geocoder searches the `places` table built from the OSM import (deploy/maps/search.sql, PostGIS does the heavy
// lifting at import time; queries are plain trigram + accent-insensitive matching so people can type without
// diacritics: "nguyen hue" finds "Nguyễn Huệ").
type Geocoder struct{ DB *pgxpool.Pool }

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// Search returns up to limit places whose name contains q, preferring prefix matches, bigger places, and the ones
// closest to near (or to the middle of Vietnam when near is nil).
func (g *Geocoder) Search(ctx context.Context, q string, near *LatLng, limit int) ([]Place, error) {
	if g == nil || g.DB == nil {
		return nil, ErrNoSearch
	}
	q = strings.TrimSpace(q)
	if len([]rune(q)) < 2 {
		return []Place{}, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	lat, lng := 16.0, 106.0
	if near != nil && near.valid() {
		lat, lng = near.Lat, near.Lng
	}
	rows, err := g.DB.Query(ctx, `
		WITH q AS (SELECT lower(unaccent($1)) AS s)
		SELECT name, kind, lat, lng FROM places, q
		WHERE name_norm LIKE '%' || q.s || '%'
		ORDER BY (name_norm LIKE q.s || '%') DESC, rank, ((lat - $2)^2 + (lng - $3)^2)
		LIMIT $4`, likeEscaper.Replace(q), lat, lng, limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Place, error) {
		var p Place
		err := r.Scan(&p.Name, &p.Kind, &p.Lat, &p.Lng)
		return p, err
	})
	if out == nil {
		out = []Place{}
	}
	return out, err
}
