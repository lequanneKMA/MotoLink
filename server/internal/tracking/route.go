package tracking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"motolink/internal/platform/apperr"
)

type RouteResult struct {
	Session   *Session     `json:"session"`
	Source    string       `json:"source"` // points | polyline | empty
	Polyline  string       `json:"polyline,omitempty"`
	Geojson   [][2]float64 `json:"coordinates,omitempty"` // [lng, lat]
	PointsRaw int          `json:"points_raw"`
	Stats     *RouteStats  `json:"stats,omitempty"`
}

type RouteStats struct {
	DistanceM float64 `json:"distance_m"`
	DurationS int64   `json:"duration_s"`
	Count     int     `json:"point_count"`
}

// Route returns the (simplified) path of a session the viewer may see. After raw points were
// downsampled by retention only the stored polyline remains (docs/design/05 §6).
func (s *Service) Route(ctx context.Context, viewer, sessionID uuid.UUID, simplifyM float64, format string) (*RouteResult, error) {
	ses, err := s.Get(ctx, viewer, sessionID)
	if err != nil {
		return nil, err
	}
	if simplifyM <= 0 {
		simplifyM = 10
	}
	if simplifyM > 500 {
		simplifyM = 500
	}
	pts, err := s.loadPoints(ctx, ses)
	if err != nil {
		return nil, err
	}
	res := &RouteResult{Session: ses, PointsRaw: len(pts)}
	switch {
	case len(pts) > 0:
		res.Source = "points"
		st := ComputeStats(pts, 90)
		res.Stats = &RouteStats{DistanceM: st.DistanceM, DurationS: st.DurationS, Count: st.Count}
		keep := SimplifyIdx(pts, simplifyM)
		simp := make([]Point, 0, len(keep))
		for _, i := range keep {
			simp = append(simp, pts[i])
		}
		fill(res, simp, format)
	case ses.RoutePolyline != nil && *ses.RoutePolyline != "":
		res.Source = "polyline"
		if ses.DistanceM != nil && ses.DurationS != nil && ses.PointCount != nil {
			res.Stats = &RouteStats{DistanceM: *ses.DistanceM, DurationS: int64(*ses.DurationS), Count: *ses.PointCount}
		}
		if format == "geojson" {
			for _, c := range DecodePolyline(*ses.RoutePolyline) {
				res.Geojson = append(res.Geojson, [2]float64{c[1], c[0]})
			}
		} else {
			res.Polyline = *ses.RoutePolyline
		}
	default:
		res.Source = "empty"
	}
	return res, nil
}

func fill(res *RouteResult, pts []Point, format string) {
	if format == "geojson" {
		for _, p := range pts {
			res.Geojson = append(res.Geojson, [2]float64{p.Lng, p.Lat})
		}
		return
	}
	res.Polyline = EncodePolyline(pts)
}

func (s *Service) loadPoints(ctx context.Context, ses *Session) ([]Point, error) {
	month, err := time.Parse("2006-01-02", ses.SessionMonth)
	if err != nil {
		return nil, wrap(err)
	}
	rows, err := s.db.Query(ctx, `
		SELECT seq, lat, lng, recorded_at, accuracy_m, speed_mps FROM location_points
		WHERE session_month=$1 AND session_id=$2 AND NOT suspect ORDER BY seq`, month, ses.ID)
	if err != nil {
		return nil, wrap(err)
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Point, error) {
		var p Point
		var at time.Time
		err := r.Scan(&p.Seq, &p.Lat, &p.Lng, &at, &p.AccuracyM, &p.SpeedMps)
		p.RecordedMs = at.UnixMilli()
		return p, err
	})
}

// RecomputeStats recomputes distance/duration/count and the simplified polyline of an ended
// session from its stored points. Idempotent: always recomputed from scratch (docs/design/09 §5.3).
func (s *Service) RecomputeStats(ctx context.Context, sessionID uuid.UUID) error {
	ses, err := scanSession(s.db.QueryRow(ctx, `SELECT `+sessionCols+` FROM tracking_sessions WHERE id=$1`, sessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return apperr.NotFound()
	}
	if err != nil {
		return wrap(err)
	}
	pts, err := s.loadPoints(ctx, ses)
	if err != nil {
		return err
	}
	st := ComputeStats(pts, 90)
	keep := SimplifyIdx(pts, 10)
	simp := make([]Point, 0, len(keep))
	for _, i := range keep {
		simp = append(simp, pts[i])
	}
	var poly *string
	if len(simp) > 0 {
		p := EncodePolyline(simp)
		poly = &p
	}
	_, err = s.db.Exec(ctx, `UPDATE tracking_sessions SET point_count=$2, distance_m=$3, duration_s=$4, route_polyline=coalesce($5, route_polyline),
		stats_version=stats_version+1 WHERE id=$1`, sessionID, st.Count, st.DistanceM, int(st.DurationS), poly)
	return wrap(err)
}

// FinalizeStats computes stats for ended sessions after a grace period for late points, and re-computes
// when the stored point count no longer matches (late points arrived). Returns how many were updated.
func (s *Service) FinalizeStats(ctx context.Context, grace time.Duration, limit int) (int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT s.id FROM tracking_sessions s
		WHERE s.status='ended' AND s.ended_at < now() - make_interval(secs => $1) AND s.ended_at > now() - interval '3 days'
		  AND (s.stats_version = 0 OR s.point_count IS DISTINCT FROM
		       (SELECT count(*) FROM location_points p WHERE p.session_month=s.session_month AND p.session_id=s.id AND NOT p.suspect))
		LIMIT $2`, grace.Seconds(), limit)
	if err != nil {
		return 0, wrap(err)
	}
	idsList, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
	if err != nil {
		return 0, wrap(err)
	}
	n := 0
	for _, sid := range idsList {
		if err := s.RecomputeStats(ctx, sid); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// DownsampleOld deletes raw points of sessions that ended before cutoff, after making sure the
// polyline/stats exist (docs/design/05 §6, ADR-010). Returns sessions processed.
func (s *Service) DownsampleOld(ctx context.Context, cutoff time.Time, limit int) (int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT s.id, s.session_month FROM tracking_sessions s
		WHERE s.status='ended' AND s.ended_at < $1
		  AND EXISTS (SELECT 1 FROM location_points p WHERE p.session_month=s.session_month AND p.session_id=s.id)
		LIMIT $2`, cutoff, limit)
	if err != nil {
		return 0, wrap(err)
	}
	type c struct {
		id    uuid.UUID
		month time.Time
	}
	cands, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (c, error) { var x c; err := r.Scan(&x.id, &x.month); return x, err })
	if err != nil {
		return 0, wrap(err)
	}
	for _, x := range cands {
		if err := s.RecomputeStats(ctx, x.id); err != nil {
			return 0, err
		}
		if _, err := s.db.Exec(ctx, `DELETE FROM location_points WHERE session_month=$1 AND session_id=$2`, x.month, x.id); err != nil {
			return 0, wrap(err)
		}
	}
	return len(cands), nil
}

// EnsurePartitions creates monthly location_points partitions from last month through `ahead` months.
func (s *Service) EnsurePartitions(ctx context.Context, ahead int) error {
	_, err := s.db.Exec(ctx, `
		DO $$
		DECLARE m date;
		BEGIN
		  FOR i IN -1..`+itoa(ahead)+` LOOP
		    m := (date_trunc('month', now() AT TIME ZONE 'UTC') + make_interval(months => i))::date;
		    EXECUTE format('CREATE TABLE IF NOT EXISTS %I PARTITION OF location_points FOR VALUES FROM (%L) TO (%L)',
		      'location_points_' || to_char(m, 'YYYY_MM'), m, (m + interval '1 month')::date);
		  END LOOP;
		END $$`)
	return wrap(err)
}

// DropOldPartitions drops whole monthly partitions that end before cutoff (retention, ADR-010).
func (s *Service) DropOldPartitions(ctx context.Context, cutoff time.Time) (int, error) {
	rows, err := s.db.Query(ctx, `
		SELECT c.relname FROM pg_inherits i JOIN pg_class c ON c.oid=i.inhrelid JOIN pg_class p ON p.oid=i.inhparent
		WHERE p.relname='location_points' AND c.relname ~ '^location_points_[0-9]{4}_[0-9]{2}$'`)
	if err != nil {
		return 0, wrap(err)
	}
	names, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return 0, wrap(err)
	}
	n := 0
	for _, name := range names {
		var y, m int
		if _, err := scanYM(name, &y, &m); err != nil {
			continue
		}
		end := time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
		if end.Before(cutoff) {
			if _, err := s.db.Exec(ctx, `DROP TABLE IF EXISTS `+pgx.Identifier{name}.Sanitize()); err != nil {
				return n, wrap(err)
			}
			n++
		}
	}
	return n, nil
}
