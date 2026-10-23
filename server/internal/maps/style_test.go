package maps

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStyleIsConsistent(t *testing.T) {
	for _, theme := range []string{"light", "dark"} {
		raw, err := Style(theme, "http://tiles.local:3000/")
		if err != nil {
			t.Fatal(err)
		}
		var s struct {
			Version int
			Sources map[string]struct {
				Tiles []string
			}
			Glyphs string
			Layers []struct {
				ID, Type, Source string
				SourceLayer      string `json:"source-layer"`
			}
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatal(err)
		}
		if s.Version != 8 || s.Sources["omt"].Tiles[0] != "http://tiles.local:3000/vietnam/{z}/{x}/{y}" || s.Glyphs != "http://tiles.local:3000/font/{fontstack}/{range}" {
			t.Fatalf("%s: %s", theme, raw)
		}
		ids := map[string]bool{}
		for _, l := range s.Layers {
			if ids[l.ID] {
				t.Fatalf("duplicate layer id %s", l.ID)
			}
			ids[l.ID] = true
			if l.Type != "background" && s.Sources[l.Source].Tiles == nil {
				t.Fatalf("layer %s uses unknown source %q", l.ID, l.Source)
			}
			if l.Type != "background" && l.SourceLayer == "" {
				t.Fatalf("layer %s has no source-layer", l.ID)
			}
		}
		if !ids["road-major"] || !ids["place-town"] || !ids["building"] {
			t.Fatalf("missing core layers: %v", ids)
		}
	}
	light, _ := Style("light", "http://x")
	dark, _ := Style("dark", "http://x")
	if string(light) == string(dark) || !strings.Contains(string(dark), "#141B18") {
		t.Fatal("dark theme must differ from light")
	}
}

func TestStyleRejectsBadInput(t *testing.T) {
	if _, err := Style("neon", "http://x"); err == nil {
		t.Fatal("unknown theme accepted")
	}
	if _, err := Style("light", ""); err == nil {
		t.Fatal("empty tile base accepted")
	}
}
