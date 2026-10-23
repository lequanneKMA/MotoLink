package maps

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Theme colours of the generated basemap. Roads keep strong casing so they read at a glance on a handlebar mount.
type palette struct {
	bg, land, park, water, waterway, building, boundary             string
	roadMajor, roadMajorCase, road, roadCase, roadMinor, path, rail string
	text, textHalo, textWater                                       string
}

var (
	lightPalette = palette{
		bg: "#EEF1EA", land: "#E9EEE6", park: "#CFE5C3", water: "#9CCDE3", waterway: "#9CCDE3", building: "#DAD9D2", boundary: "#9A8FB0",
		roadMajor: "#F6C453", roadMajorCase: "#C58F1B", road: "#FFFFFF", roadCase: "#B9BDB5", roadMinor: "#FFFFFF", path: "#C9A27C", rail: "#9AA39B",
		text: "#1B2420", textHalo: "#FFFFFF", textWater: "#2F6B8A",
	}
	darkPalette = palette{
		bg: "#141B18", land: "#1B2420", park: "#1F3A28", water: "#17343F", waterway: "#17343F", building: "#26312C", boundary: "#6C6483",
		roadMajor: "#B88A22", roadMajorCase: "#5A430D", road: "#4A5650", roadCase: "#1A2220", roadMinor: "#3B4540", path: "#6C5A46", rail: "#55605A",
		text: "#E6EDE8", textHalo: "#141B18", textWater: "#7DB6CE",
	}
)

// Style builds a MapLibre style for tiles in the OpenMapTiles schema (what planetiler produces by default), served
// by a tile server at tileBase: vector tiles at {tileBase}/vietnam/{z}/{x}/{y} and glyphs at {tileBase}/font/....
func Style(theme, tileBase string) ([]byte, error) {
	p := lightPalette
	if theme == "dark" {
		p = darkPalette
	} else if theme != "light" {
		return nil, fmt.Errorf("unknown theme %q", theme)
	}
	base := strings.TrimRight(tileBase, "/")
	if base == "" {
		return nil, fmt.Errorf("tile base url not configured")
	}
	type m = map[string]any
	line := func(id, src string, filter any, color string, width any, extra m) m {
		l := m{"id": id, "type": "line", "source": "omt", "source-layer": src,
			"layout": m{"line-cap": "round", "line-join": "round"}, "paint": m{"line-color": color, "line-width": width}}
		if filter != nil {
			l["filter"] = filter
		}
		for k, v := range extra {
			l[k] = v
		}
		return l
	}
	fill := func(id, src string, filter any, color string) m {
		l := m{"id": id, "type": "fill", "source": "omt", "source-layer": src, "paint": m{"fill-color": color}}
		if filter != nil {
			l["filter"] = filter
		}
		return l
	}
	width := func(z12, z16, z19 float64) any {
		return []any{"interpolate", []any{"exponential", 1.4}, []any{"zoom"}, 12, z12, 16, z16, 19, z19}
	}
	cls := func(vals ...string) any {
		f := []any{"in", []any{"get", "class"}}
		for _, v := range vals {
			f = append(f, v)
		}
		return []any{"all", []any{"!=", []any{"get", "brunnel"}, "tunnel"}, f}
	}
	name := []any{"coalesce", []any{"get", "name:vi"}, []any{"get", "name"}}
	label := func(id, src string, filter any, size any, color, halo string, extra m) m {
		l := m{"id": id, "type": "symbol", "source": "omt", "source-layer": src,
			"layout": m{"text-field": name, "text-font": []string{"Noto Sans Bold"}, "text-size": size, "text-max-width": 8},
			"paint":  m{"text-color": color, "text-halo-color": halo, "text-halo-width": 1.6}}
		if filter != nil {
			l["filter"] = filter
		}
		for k, v := range extra {
			l[k] = v
		}
		return l
	}

	layers := []m{
		{"id": "background", "type": "background", "paint": m{"background-color": p.land}},
		fill("landcover", "landcover", nil, p.park),
		fill("park", "park", nil, p.park),
		fill("landuse-green", "landuse", []any{"in", []any{"get", "class"}, "grass", "cemetery", "pitch", "playground"}, p.park),
		fill("water", "water", nil, p.water),
		line("waterway", "waterway", nil, p.waterway, width(1, 3, 6), nil),
		fill("building", "building", nil, p.building),
		line("rail", "transportation", []any{"==", []any{"get", "class"}, "rail"}, p.rail, 1.2, nil),
		line("road-minor-case", "transportation", cls("minor", "service", "track"), p.roadCase, width(1.5, 8, 18), nil),
		line("road-minor", "transportation", cls("minor", "service", "track"), p.roadMinor, width(0.8, 6, 15), nil),
		line("path", "transportation", cls("path"), p.path, 1.4, m{"paint": m{"line-color": p.path, "line-width": 1.4, "line-dasharray": []float64{2, 2}}}),
		line("road-case", "transportation", cls("tertiary", "secondary", "primary", "trunk", "motorway"), p.roadMajorCase, width(2, 12, 24), nil),
		line("road-tertiary", "transportation", cls("tertiary"), p.road, width(1.2, 9, 20), nil),
		line("road-major", "transportation", cls("secondary", "primary", "trunk", "motorway"), p.roadMajor, width(1.6, 10, 21), nil),
		line("boundary", "boundary", []any{"all", []any{"==", []any{"get", "admin_level"}, 4}, []any{"!=", []any{"get", "maritime"}, 1}}, p.boundary, 1.2,
			m{"paint": m{"line-color": p.boundary, "line-width": 1.2, "line-dasharray": []float64{4, 2}}}),
		label("road-name", "transportation_name", nil, []any{"interpolate", []any{"linear"}, []any{"zoom"}, 13, 11, 18, 15}, p.text, p.textHalo,
			m{"minzoom": 13, "layout": m{"text-field": name, "text-font": []string{"Noto Sans Regular"}, "symbol-placement": "line", "text-size": 13}}),
		label("water-name", "water_name", nil, 13, p.textWater, p.textHalo, m{"layout": m{"text-field": name, "text-font": []string{"Noto Sans Regular"}, "text-size": 13}}),
		label("place-village", "place", []any{"in", []any{"get", "class"}, "village", "suburb", "neighbourhood", "hamlet"}, 13, p.text, p.textHalo, m{"minzoom": 11}),
		label("place-town", "place", []any{"in", []any{"get", "class"}, "town", "city"}, []any{"interpolate", []any{"linear"}, []any{"zoom"}, 5, 13, 12, 20}, p.text, p.textHalo, nil),
	}
	style := m{
		"version": 8, "name": "MotoLink " + theme,
		"sources": m{"omt": m{"type": "vector", "tiles": []string{base + "/vietnam/{z}/{x}/{y}"}, "minzoom": 0, "maxzoom": 14,
			"attribution": "© OpenStreetMap contributors"}},
		"glyphs": base + "/font/{fontstack}/{range}",
		"layers": layers,
	}
	return json.Marshal(style)
}
