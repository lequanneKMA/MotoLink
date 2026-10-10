// Package jobs runs the periodic maintenance tasks of docs/design/05 §6 and 09 §5.3. Several worker instances may
// run it: every job takes a short lock first so it executes on one instance at a time.
package jobs

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"

	"motolink/internal/tracking"
	"motolink/internal/trip"
)

// Locker serialises a job across instances.
type Locker interface {
	// TryLock returns an unlock func when the lock was taken, nil when somebody else holds it.
	TryLock(ctx context.Context, name string, ttl time.Duration) func()
}

// RedisLocker uses SET NX PX (a lease, not a fence: jobs are idempotent).
type RedisLocker struct{ C redis.UniversalClient }

func (l RedisLocker) TryLock(ctx context.Context, name string, ttl time.Duration) func() {
	ok, err := l.C.SetNX(ctx, "ml:job:"+name, "1", ttl).Result()
	if err != nil || !ok {
		return nil
	}
	return func() { _ = l.C.Del(context.Background(), "ml:job:"+name).Err() }
}

// LocalLocker is for single-instance runs (no Redis).
type LocalLocker struct {
	mu   sync.Mutex
	held map[string]bool
}

func (l *LocalLocker) TryLock(_ context.Context, name string, _ time.Duration) func() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held == nil {
		l.held = map[string]bool{}
	}
	if l.held[name] {
		return nil
	}
	l.held[name] = true
	return func() { l.mu.Lock(); delete(l.held, name); l.mu.Unlock() }
}

// Job is one periodic task.
type Job struct {
	Name  string
	Every time.Duration
	Run   func(ctx context.Context) (int, error) // returns how many items it handled
}

// Scheduler runs jobs until its context ends.
type Scheduler struct {
	Jobs  []Job
	Lock  Locker
	Log   *slog.Logger
	runs  *prometheus.CounterVec
	items *prometheus.CounterVec
}

func NewScheduler(jobs []Job, lock Locker, log *slog.Logger, reg prometheus.Registerer) *Scheduler {
	if lock == nil {
		lock = &LocalLocker{}
	}
	if log == nil {
		log = slog.Default()
	}
	s := &Scheduler{Jobs: jobs, Lock: lock, Log: log,
		runs:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "motolink_job_runs_total", Help: "Job executions by result."}, []string{"job", "result"}),
		items: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "motolink_job_items_total", Help: "Items handled by jobs."}, []string{"job"})}
	if reg != nil {
		reg.MustRegister(s.runs, s.items)
	}
	return s
}

// Run blocks until ctx ends.
func (s *Scheduler) Run(ctx context.Context) {
	var wg sync.WaitGroup
	for _, j := range s.Jobs {
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			// stagger first runs a little so instances/jobs do not start in lockstep
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(len(j.Name)%5+1) * time.Second):
			}
			t := time.NewTicker(j.Every)
			defer t.Stop()
			for {
				s.RunOnce(ctx, j)
				select {
				case <-ctx.Done():
					return
				case <-t.C:
				}
			}
		}(j)
	}
	wg.Wait()
}

// RunOnce executes one job under its lock.
func (s *Scheduler) RunOnce(ctx context.Context, j Job) {
	unlock := s.Lock.TryLock(ctx, j.Name, j.Every+30*time.Second)
	if unlock == nil {
		s.runs.WithLabelValues(j.Name, "locked").Inc()
		return
	}
	defer unlock()
	n, err := j.Run(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.Log.Error("job failed", "job", j.Name, "err", err)
		}
		s.runs.WithLabelValues(j.Name, "error").Inc()
		return
	}
	s.runs.WithLabelValues(j.Name, "ok").Inc()
	if n > 0 {
		s.items.WithLabelValues(j.Name).Add(float64(n))
		s.Log.Info("job done", "job", j.Name, "items", n)
	}
}

// Config is the policy of the standard job set.
type Config struct {
	FullresDays   int
	PartitionDays int
	StatsGrace    time.Duration // wait this long after a session ended for late points
	SessionIdle   time.Duration // no heartbeat for this long => timeout
	TripIdle      time.Duration // no open session for this long => trip auto-ended
}

func (c *Config) defaults() {
	if c.FullresDays <= 0 {
		c.FullresDays = 30
	}
	if c.PartitionDays <= 0 {
		c.PartitionDays = 90
	}
	if c.StatsGrace <= 0 {
		c.StatsGrace = 2 * time.Minute
	}
	if c.SessionIdle <= 0 {
		c.SessionIdle = 3 * time.Minute
	}
	if c.TripIdle <= 0 {
		c.TripIdle = 12 * time.Hour
	}
}

// Standard builds the job set of the design documents.
func Standard(db *pgxpool.Pool, sessions *tracking.Service, trips *trip.Service, c Config) []Job {
	c.defaults()
	exec := func(q string, args ...any) func(context.Context) (int, error) {
		return func(ctx context.Context) (int, error) {
			tag, err := db.Exec(ctx, q, args...)
			return int(tag.RowsAffected()), err
		}
	}
	return []Job{
		{"session_timeout", 30 * time.Second, func(ctx context.Context) (int, error) { return sessions.MarkTimedOut(ctx, c.SessionIdle) }},
		{"finalize_stats", 30 * time.Second, func(ctx context.Context) (int, error) { return sessions.FinalizeStats(ctx, c.StatsGrace, 50) }},
		{"trip_auto_end", 5 * time.Minute, func(ctx context.Context) (int, error) { return trips.AutoEndIdle(ctx, c.TripIdle) }},
		{"ensure_partitions", 6 * time.Hour, func(ctx context.Context) (int, error) { return 0, sessions.EnsurePartitions(ctx, 3) }},
		{"downsample_old", time.Hour, func(ctx context.Context) (int, error) {
			return sessions.DownsampleOld(ctx, time.Now().AddDate(0, 0, -c.FullresDays), 20)
		}},
		{"drop_old_partitions", 24 * time.Hour, func(ctx context.Context) (int, error) {
			return sessions.DropOldPartitions(ctx, time.Now().AddDate(0, 0, -c.PartitionDays))
		}},
		{"cleanup_outbox", time.Hour, exec(`DELETE FROM outbox WHERE published_at IS NOT NULL AND published_at < now() - interval '7 days'`)},
		{"cleanup_idempotency", time.Hour, exec(`DELETE FROM idempotency_keys WHERE expires_at < now()`)},
		{"cleanup_refresh_tokens", 6 * time.Hour, exec(`DELETE FROM refresh_tokens WHERE expires_at < now() - interval '7 days'`)},
		{"cleanup_notifications", 24 * time.Hour, exec(`DELETE FROM notifications WHERE created_at < now() - interval '90 days'`)},
		{"cleanup_invitations", 24 * time.Hour, exec(`DELETE FROM trip_invitations WHERE expires_at < now() - interval '30 days'`)},
	}
}
