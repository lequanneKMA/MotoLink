package httpapi

import (
	"context"
	"net/http"
	"time"

	"motolink/internal/platform/apperr"
	"motolink/internal/realtime"
	"motolink/internal/tracking"
)

// PointWriter stores points durably and returns once they are acknowledged-safe (COMMIT / Kafka acks=all).
type PointWriter interface {
	WritePoints(ctx context.Context, pts []realtime.DurablePoint) error
}

const maxRESTPoints = 500

type restPoint struct {
	Seq        int64    `json:"seq"`
	ClientTs   int64    `json:"client_ts"`
	Lat        float64  `json:"lat"`
	Lng        float64  `json:"lng"`
	AccuracyM  *float32 `json:"accuracy_m"`
	SpeedMps   *float32 `json:"speed_mps"`
	HeadingDeg *float32 `json:"heading_deg"`
	AltitudeM  *float64 `json:"altitude_m"`
}

// postPoints is the fallback / backlog-drain path for GPS points (no live broadcast; same validation as WS).
func (a *API) postPoints(w http.ResponseWriter, r *http.Request, p Principal) error {
	sid, err := pathUUID(r, "sid")
	if err != nil {
		return err
	}
	var in struct {
		Points []restPoint `json:"points"`
	}
	if err := decodeLarge(r, &in, 256<<10); err != nil {
		return err
	}
	if len(in.Points) == 0 || len(in.Points) > maxRESTPoints {
		return apperr.BadRequest("points phải có 1–500 điểm")
	}
	if a.Points == nil {
		return apperr.Unavailable()
	}
	ses, err := a.Sessions.LookupOpen(r.Context(), sid)
	if err != nil || ses.UserID != p.UserID {
		return apperr.NotFound()
	}
	if ses.Status != tracking.StatusActive && ses.Status != tracking.StatusPaused {
		return apperr.Conflict("session_not_open", "Phiên ghi đã kết thúc")
	}
	month, _ := time.Parse("2006-01-02", ses.SessionMonth)
	now := time.Now()
	type rej struct {
		Seq    int64  `json:"seq"`
		Reason string `json:"reason"`
	}
	rejected := []rej{}
	good := make([]realtime.DurablePoint, 0, len(in.Points))
	var prev, upTo int64
	for _, pt := range in.Points {
		if pt.Seq <= prev || pt.Seq <= 0 {
			return apperr.BadRequest("seq phải tăng dần")
		}
		prev, upTo = pt.Seq, pt.Seq
		switch {
		case !realtime.ValidCoord(pt.Lat, pt.Lng):
			rejected = append(rejected, rej{pt.Seq, "invalid_coordinates"})
			continue
		case pt.AccuracyM == nil || *pt.AccuracyM <= 0 || float64(*pt.AccuracyM) > realtime.MaxAccuracyM:
			rejected = append(rejected, rej{pt.Seq, "low_accuracy"})
			continue
		}
		suspect := pt.SpeedMps != nil && (*pt.SpeedMps < 0 || float64(*pt.SpeedMps) > realtime.MaxSpeedMps)
		rec := time.UnixMilli(pt.ClientTs)
		if pt.ClientTs == 0 || rec.Before(now.Add(-30*24*time.Hour)) || rec.After(now.Add(5*time.Second)) {
			suspect, rec = true, now
		}
		good = append(good, realtime.DurablePoint{TripID: ses.TripID, UserID: p.UserID, SessionID: sid, SessionMonth: month,
			Seq: pt.Seq, RecordedAt: rec, ReceivedAt: now, Lat: pt.Lat, Lng: pt.Lng, AccuracyM: pt.AccuracyM,
			SpeedMps: pt.SpeedMps, HeadingDeg: pt.HeadingDeg, AltitudeM: pt.AltitudeM, Suspect: suspect})
	}
	if err := a.Points.WritePoints(r.Context(), good); err != nil {
		a.Log.Error("rest points write failed", "err", err)
		return apperr.Unavailable()
	}
	_ = a.Sessions.Touch(r.Context(), sid, upTo)
	writeJSON(w, http.StatusOK, map[string]any{"up_to_seq": upTo, "rejected": rejected})
	return nil
}
