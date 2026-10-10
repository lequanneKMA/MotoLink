// Package durable holds the long-term branch of the location pipeline and the transactional outbox relay
// (ADR-005/007/021, docs/design/09).
package durable

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"

	"motolink/internal/realtime"
)

// InsertPoints writes a batch idempotently (PK = session_month, session_id, seq). It returns only after COMMIT,
// so callers may acknowledge the points afterwards.
func InsertPoints(ctx context.Context, db *pgxpool.Pool, pts []realtime.DurablePoint) error {
	if len(pts) == 0 {
		return nil
	}
	n := len(pts)
	var (
		months   = make([]time.Time, n)
		sessions = make([]uuid.UUID, n)
		seqs     = make([]int32, n)
		trips    = make([]uuid.UUID, n)
		users    = make([]uuid.UUID, n)
		recAt    = make([]time.Time, n)
		rcvAt    = make([]time.Time, n)
		lat      = make([]float64, n)
		lng      = make([]float64, n)
		acc      = make([]*float32, n)
		spd      = make([]*float32, n)
		hdg      = make([]*float32, n)
		alt      = make([]*float32, n)
		sus      = make([]bool, n)
	)
	for i, p := range pts {
		months[i], sessions[i], seqs[i], trips[i], users[i] = p.SessionMonth, p.SessionID, int32(p.Seq), p.TripID, p.UserID
		recAt[i], rcvAt[i], lat[i], lng[i] = p.RecordedAt, p.ReceivedAt, p.Lat, p.Lng
		acc[i], spd[i], hdg[i], sus[i] = p.AccuracyM, p.SpeedMps, p.HeadingDeg, p.Suspect
		if p.AltitudeM != nil {
			v := float32(*p.AltitudeM)
			alt[i] = &v
		}
	}
	_, err := db.Exec(ctx, `
INSERT INTO location_points (session_month, session_id, seq, trip_id, user_id, recorded_at, received_at, lat, lng,
                             accuracy_m, speed_mps, heading_deg, altitude_m, suspect)
SELECT * FROM unnest($1::date[], $2::uuid[], $3::int[], $4::uuid[], $5::uuid[], $6::timestamptz[], $7::timestamptz[],
                     $8::float8[], $9::float8[], $10::real[], $11::real[], $12::real[], $13::real[], $14::bool[])
ON CONFLICT (session_month, session_id, seq) DO NOTHING`,
		months, sessions, seqs, trips, users, recAt, rcvAt, lat, lng, acc, spd, hdg, alt, sus)
	return err
}

// PGSinkOptions tune the batch writer.
type PGSinkOptions struct {
	QueueSize  int           // bounded queue; a full queue surfaces as ErrSinkFull (backpressure)
	BatchSize  int           // flush when this many points are buffered
	FlushEvery time.Duration // ... or after this long
	Log        *slog.Logger
	Reg        prometheus.Registerer
}

// PGSink is the free-tier history sink (ADR-021): a single batching goroutine writing straight to PostgreSQL.
type PGSink struct {
	db   *pgxpool.Pool
	opt  PGSinkOptions
	in   chan realtime.DurablePoint
	mu   sync.RWMutex
	done func(uuid.UUID, int64)
	wg   sync.WaitGroup

	batches  prometheus.Counter
	failures prometheus.Counter
	depth    prometheus.GaugeFunc
}

func NewPGSink(db *pgxpool.Pool, opt PGSinkOptions) *PGSink {
	if opt.QueueSize <= 0 {
		opt.QueueSize = 4096
	}
	if opt.BatchSize <= 0 {
		opt.BatchSize = 200
	}
	if opt.FlushEvery <= 0 {
		opt.FlushEvery = 250 * time.Millisecond
	}
	if opt.Log == nil {
		opt.Log = slog.Default()
	}
	s := &PGSink{db: db, opt: opt, in: make(chan realtime.DurablePoint, opt.QueueSize)}
	s.batches = prometheus.NewCounter(prometheus.CounterOpts{Name: "motolink_history_batches_total", Help: "Committed history batches."})
	s.failures = prometheus.NewCounter(prometheus.CounterOpts{Name: "motolink_history_batch_failures_total", Help: "Failed history batch writes (retried)."})
	s.depth = prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "motolink_history_queue_depth", Help: "Points waiting for the history writer."},
		func() float64 { return float64(len(s.in)) })
	if opt.Reg != nil {
		opt.Reg.MustRegister(s.batches, s.failures, s.depth)
	}
	return s
}

func (s *PGSink) SetDone(fn func(uuid.UUID, int64)) {
	s.mu.Lock()
	s.done = fn
	s.mu.Unlock()
}

// Enqueue never blocks.
func (s *PGSink) Enqueue(p realtime.DurablePoint) error {
	select {
	case s.in <- p:
		return nil
	default:
		return realtime.ErrSinkFull
	}
}

// Start runs the writer until ctx ends; remaining queued points are flushed once before it returns.
func (s *PGSink) Start(ctx context.Context) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.loop(ctx)
	}()
}

// Wait blocks until the writer goroutine exited (after ctx cancellation).
func (s *PGSink) Wait() { s.wg.Wait() }

func (s *PGSink) loop(ctx context.Context) {
	buf := make([]realtime.DurablePoint, 0, s.opt.BatchSize)
	t := time.NewTicker(s.opt.FlushEvery)
	defer t.Stop()
	flush := func(final bool) {
		if len(buf) == 0 {
			return
		}
		backoff := 100 * time.Millisecond
		for {
			wctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := InsertPoints(wctx, s.db, buf)
			cancel()
			if err == nil {
				break
			}
			s.failures.Inc()
			s.opt.Log.Error("history batch failed", "err", err, "points", len(buf))
			if final || ctx.Err() != nil {
				buf = buf[:0] // not acknowledged: the clients resend after reconnect
				return
			}
			select {
			case <-ctx.Done():
			case <-time.After(backoff):
			}
			if backoff < 5*time.Second {
				backoff *= 2
			}
		}
		s.batches.Inc()
		s.mu.RLock()
		done := s.done
		s.mu.RUnlock()
		if done != nil {
			for _, p := range buf {
				done(p.SessionID, p.Seq)
			}
		}
		buf = buf[:0]
	}
	for {
		select {
		case p := <-s.in:
			buf = append(buf, p)
			if len(buf) >= s.opt.BatchSize {
				flush(false)
			}
		case <-t.C:
			flush(false)
		case <-ctx.Done():
			for {
				select {
				case p := <-s.in:
					buf = append(buf, p)
					continue
				default:
				}
				break
			}
			flush(true)
			return
		}
	}
}

// WritePoints writes synchronously and returns after COMMIT (REST backfill path, docs/design/06 §2.6).
func (s *PGSink) WritePoints(ctx context.Context, pts []realtime.DurablePoint) error {
	return InsertPoints(ctx, s.db, pts)
}
