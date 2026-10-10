// Package tracking owns tracking sessions, the GPS point pipeline and route reads
// (docs/design/05, 07, 09; ADR-004/005).
package tracking

import (
	"math"
	"strings"
)

const earthRadiusM = 6371008.8

// Point is a stored GPS point.
type Point struct {
	Seq        int32    `json:"seq"`
	Lat        float64  `json:"lat"`
	Lng        float64  `json:"lng"`
	RecordedMs int64    `json:"recorded_at"`
	AccuracyM  *float32 `json:"accuracy_m,omitempty"`
	SpeedMps   *float32 `json:"speed_mps,omitempty"`
}

// Haversine returns metres between two coordinates.
func Haversine(lat1, lng1, lat2, lng2 float64) float64 {
	p1, p2 := lat1*math.Pi/180, lat2*math.Pi/180
	dp, dl := p2-p1, (lng2-lng1)*math.Pi/180
	a := math.Sin(dp/2)*math.Sin(dp/2) + math.Cos(p1)*math.Cos(p2)*math.Sin(dl/2)*math.Sin(dl/2)
	return 2 * earthRadiusM * math.Atan2(math.Sqrt(a), math.Sqrt(math.Max(0, 1-a)))
}

// SimplifyIdx is Ramer–Douglas–Peucker over local projected metres; returns indices to keep.
func SimplifyIdx(pts []Point, epsilonM float64) []int {
	n := len(pts)
	if n <= 2 {
		idx := make([]int, n)
		for i := range idx {
			idx[i] = i
		}
		return idx
	}
	var refLat float64
	for _, p := range pts {
		refLat += p.Lat
	}
	refLat /= float64(n)
	cosLat := math.Cos(refLat * math.Pi / 180)
	xs, ys := make([]float64, n), make([]float64, n)
	for i, p := range pts {
		xs[i] = p.Lng * math.Pi / 180 * earthRadiusM * cosLat
		ys[i] = p.Lat * math.Pi / 180 * earthRadiusM
	}
	keep := make([]bool, n)
	keep[0], keep[n-1] = true, true
	type span struct{ s, e int }
	stack := []span{{0, n - 1}}
	for len(stack) > 0 {
		sp := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if sp.e <= sp.s+1 {
			continue
		}
		maxD, at := -1.0, -1
		for i := sp.s + 1; i < sp.e; i++ {
			if d := perp(xs[i], ys[i], xs[sp.s], ys[sp.s], xs[sp.e], ys[sp.e]); d > maxD {
				maxD, at = d, i
			}
		}
		if maxD > epsilonM && at >= 0 {
			keep[at] = true
			stack = append(stack, span{sp.s, at}, span{at, sp.e})
		}
	}
	out := make([]int, 0, n)
	for i, k := range keep {
		if k {
			out = append(out, i)
		}
	}
	return out
}

func perp(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	l2 := dx*dx + dy*dy
	if l2 == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := math.Max(0, math.Min(1, ((px-ax)*dx+(py-ay)*dy)/l2))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

// Stats over points in order, skipping teleports (> maxJumpMps) like the client does.
type Stats struct {
	DistanceM float64
	DurationS int64
	Count     int
}

func ComputeStats(pts []Point, maxJumpMps float64) Stats {
	st := Stats{Count: len(pts)}
	if len(pts) < 2 {
		return st
	}
	for i := 1; i < len(pts); i++ {
		d := Haversine(pts[i-1].Lat, pts[i-1].Lng, pts[i].Lat, pts[i].Lng)
		dt := float64(pts[i].RecordedMs-pts[i-1].RecordedMs) / 1000
		if dt > 0 && d/dt > maxJumpMps {
			continue
		}
		st.DistanceM += d
	}
	if dur := (pts[len(pts)-1].RecordedMs - pts[0].RecordedMs) / 1000; dur > 0 {
		st.DurationS = dur
	}
	return st
}

// EncodePolyline is the Google encoded polyline algorithm at 1e5 precision.
func EncodePolyline(pts []Point) string {
	var sb strings.Builder
	var pLat, pLng int64
	for _, p := range pts {
		lat, lng := int64(math.Round(p.Lat*1e5)), int64(math.Round(p.Lng*1e5))
		encodeVal(&sb, lat-pLat)
		encodeVal(&sb, lng-pLng)
		pLat, pLng = lat, lng
	}
	return sb.String()
}

func encodeVal(sb *strings.Builder, v int64) {
	v <<= 1
	if v < 0 {
		v = ^v
	}
	for v >= 0x20 {
		sb.WriteByte(byte((0x20 | (v & 0x1f)) + 63))
		v >>= 5
	}
	sb.WriteByte(byte(v + 63))
}

// DecodePolyline reverses EncodePolyline (used by tests and by route fallback merging).
func DecodePolyline(s string) [][2]float64 {
	var out [][2]float64
	var lat, lng int64
	i := 0
	next := func() (int64, bool) {
		var shift uint
		var res int64
		for {
			if i >= len(s) {
				return 0, false
			}
			b := int64(s[i]) - 63
			i++
			res |= (b & 0x1f) << shift
			shift += 5
			if b < 0x20 {
				break
			}
		}
		if res&1 != 0 {
			return ^(res >> 1), true
		}
		return res >> 1, true
	}
	for i < len(s) {
		dl, ok := next()
		if !ok {
			break
		}
		dg, ok := next()
		if !ok {
			break
		}
		lat += dl
		lng += dg
		out = append(out, [2]float64{float64(lat) / 1e5, float64(lng) / 1e5})
	}
	return out
}
