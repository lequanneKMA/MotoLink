package tracking

import (
	"math"
	"testing"
)

func TestPolylineKnownVector(t *testing.T) {
	// Example from Google's documentation.
	pts := []Point{{Lat: 38.5, Lng: -120.2}, {Lat: 40.7, Lng: -120.95}, {Lat: 43.252, Lng: -126.453}}
	const want = "_p~iF~ps|U_ulLnnqC_mqNvxq`@"
	if got := EncodePolyline(pts); got != want {
		t.Fatalf("encode = %q want %q", got, want)
	}
	dec := DecodePolyline(want)
	if len(dec) != 3 || math.Abs(dec[2][0]-43.252) > 1e-5 || math.Abs(dec[2][1]+126.453) > 1e-5 {
		t.Fatalf("decode = %v", dec)
	}
}

func TestSimplifyKeepsCornerDropsStraight(t *testing.T) {
	var pts []Point
	for i := 0; i <= 10; i++ {
		pts = append(pts, Point{Lat: 11.0, Lng: 108.0 + float64(i)*0.0001})
	}
	for j := 1; j <= 10; j++ {
		pts = append(pts, Point{Lat: 11.0 + float64(j)*0.0001, Lng: 108.001})
	}
	keep := SimplifyIdx(pts, 2)
	if keep[0] != 0 || keep[len(keep)-1] != len(pts)-1 {
		t.Fatalf("ends must be kept: %v", keep)
	}
	has10 := false
	for _, k := range keep {
		if k == 10 {
			has10 = true
		}
	}
	if !has10 || len(keep) > 4 {
		t.Fatalf("corner kept, straight runs dropped: %v", keep)
	}
	if got := SimplifyIdx(pts[:2], 5); len(got) != 2 {
		t.Fatalf("two points stay: %v", got)
	}
}

func TestStatsSkipTeleports(t *testing.T) {
	pts := []Point{
		{Lat: 11.0000, Lng: 108, RecordedMs: 0},
		{Lat: 11.0010, Lng: 108, RecordedMs: 10_000},
		{Lat: 12.0000, Lng: 108, RecordedMs: 11_000}, // ~111 km in 1 s
		{Lat: 12.0010, Lng: 108, RecordedMs: 21_000},
	}
	st := ComputeStats(pts, 90)
	if st.DistanceM < 100 || st.DistanceM > 260 || st.DurationS != 21 || st.Count != 4 {
		t.Fatalf("%+v", st)
	}
}

func TestHaversine(t *testing.T) {
	d := Haversine(21.0278, 105.8342, 10.8231, 106.6297)
	if math.Abs(d-1_138_000) > 15_000 {
		t.Fatalf("got %f", d)
	}
}
