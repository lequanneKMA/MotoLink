package maps

import (
	"context"
	"testing"

	"motolink/internal/testutil/pgtest"
)

func TestGeocoderAccentInsensitiveAndRanked(t *testing.T) {
	pool := pgtest.Pool(t, pgtest.New(t))
	ctx := context.Background()
	for _, ddl := range []string{
		`CREATE EXTENSION IF NOT EXISTS unaccent`,
		`CREATE TABLE places (name text, kind text, rank int, lat float8, lng float8, name_norm text)`,
		`INSERT INTO places (name, kind, rank, lat, lng, name_norm) SELECT n, k, r, la, lo, lower(unaccent(n)) FROM (VALUES
		   ('Nguyễn Huệ','road',4,10.7745,106.7040),
		   ('Nguyễn Huệ','road',4,16.4637,107.5909),
		   ('Phố đi bộ Nguyễn Huệ','tourism',3,10.7740,106.7035),
		   ('Đà Lạt','city',1,11.9404,108.4583),
		   ('Đà Nẵng','city',1,16.0544,108.2022),
		   ('100% Cà phê','shop',3,10.0,106.0)) v(n,k,r,la,lo)`,
	} {
		if _, err := pool.Exec(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}
	g := &Geocoder{DB: pool}

	// typed without diacritics, near Ho Chi Minh City: the local street comes first, prefix matches before substrings
	got, err := g.Search(ctx, "nguyen hue", &LatLng{10.77, 106.70}, 5)
	if err != nil || len(got) != 3 {
		t.Fatalf("%v %+v", err, got)
	}
	if got[0].Lat != 10.7745 || got[1].Lat != 16.4637 || got[2].Name != "Phố đi bộ Nguyễn Huệ" {
		t.Fatalf("ranking: %+v", got)
	}
	// bigger places win among prefix matches, regardless of distance
	got, _ = g.Search(ctx, "đà", &LatLng{16.0, 108.0}, 5)
	if len(got) != 2 {
		t.Fatalf("%+v", got)
	}
	// LIKE wildcards in the input are literals, not patterns
	got, _ = g.Search(ctx, "100%", nil, 5)
	if len(got) != 1 || got[0].Name != "100% Cà phê" {
		t.Fatalf("%+v", got)
	}
	if got, _ := g.Search(ctx, "%%", nil, 5); len(got) != 0 {
		t.Fatalf("wildcard matched everything: %+v", got)
	}
	if got, err := g.Search(ctx, "a", nil, 5); err != nil || len(got) != 0 {
		t.Fatalf("too short: %v %+v", err, got)
	}
	if _, err := (*Geocoder)(nil).Search(ctx, "abc", nil, 5); err != ErrNoSearch {
		t.Fatalf("unconfigured: %v", err)
	}
}
