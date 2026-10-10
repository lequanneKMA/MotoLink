package durable

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"motolink/internal/realtime"
)

// ErrData marks a failure caused by the data itself (retrying cannot help); anything else is infrastructure.
var ErrData = errors.New("durable: bad data")

// writeBatchFn writes pts in one transaction.
type writeBatchFn func(ctx context.Context, pts []realtime.DurablePoint) error

// isDataError classifies PG errors: constraint/data exceptions are data errors, the rest is infrastructure.
func isDataError(err error) bool {
	type pgErr interface{ SQLState() string }
	var pe pgErr
	if errors.As(err, &pe) {
		c := pe.SQLState()
		return len(c) >= 2 && (c[:2] == "22" || c[:2] == "23")
	}
	return errors.Is(err, ErrData)
}

// writeWithBisect writes the batch; on a data error it halves the batch until the offending point is isolated
// and reports it through bad (docs/design/09 §5.1). Infrastructure errors are returned for a retry.
func writeWithBisect(ctx context.Context, pts []realtime.DurablePoint, write writeBatchFn, bad func(realtime.DurablePoint, error)) error {
	if len(pts) == 0 {
		return nil
	}
	err := write(ctx, pts)
	if err == nil {
		return nil
	}
	if !isDataError(err) {
		return err
	}
	if len(pts) == 1 {
		bad(pts[0], err)
		return nil
	}
	mid := len(pts) / 2
	if err := writeWithBisect(ctx, pts[:mid], write, bad); err != nil {
		return err
	}
	return writeWithBisect(ctx, pts[mid:], write, bad)
}

// HistoryWriter is the cg.history-writer consumer: Kafka → location_points.
type HistoryWriter struct {
	DB      *pgxpool.Pool
	Cfg     KafkaConfig
	Group   string
	Batch   int
	Linger  time.Duration
	dlq     *kgo.Client
	written prometheus.Counter
	dlqCnt  *prometheus.CounterVec
}

func (w *HistoryWriter) init() {
	if w.Group == "" {
		w.Group = "cg.history-writer"
	}
	if w.Batch <= 0 {
		w.Batch = 500
	}
	if w.Linger <= 0 {
		w.Linger = 200 * time.Millisecond
	}
	if w.written == nil {
		w.written = prometheus.NewCounter(prometheus.CounterOpts{Name: "motolink_history_written_points_total", Help: "Points written by the Kafka history writer."})
		w.dlqCnt = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "motolink_kafka_dlq_records_total", Help: "Records sent to a DLQ."}, []string{"topic", "reason"})
		if w.Cfg.Reg != nil {
			w.Cfg.Reg.MustRegister(w.written, w.dlqCnt)
		}
	}
	if w.Cfg.Log == nil {
		w.Cfg.Log = slog.Default()
	}
}

// Run consumes until ctx ends.
func (w *HistoryWriter) Run(ctx context.Context) error {
	w.init()
	cl, err := kgo.NewClient(kgo.SeedBrokers(w.Cfg.Brokers...), kgo.ConsumerGroup(w.Group), kgo.ConsumeTopics(TopicLocation),
		kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(), kgo.FetchMaxWait(250*time.Millisecond),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		return err
	}
	defer cl.Close()
	w.dlq, err = NewProducerClient(w.Cfg)
	if err != nil {
		return err
	}
	defer w.dlq.Close()

	for ctx.Err() == nil {
		pctx, cancel := context.WithTimeout(ctx, w.Linger)
		fetches := cl.PollRecords(pctx, w.Batch)
		cancel()
		if ctx.Err() != nil {
			cl.AllowRebalance()
			return nil
		}
		var recs []*kgo.Record
		fetches.EachError(func(t string, p int32, err error) {
			if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
				w.Cfg.Log.Error("kafka fetch", "topic", t, "partition", p, "err", err)
			}
		})
		fetches.EachRecord(func(r *kgo.Record) { recs = append(recs, r) })
		if len(recs) > 0 {
			if err := w.handle(ctx, recs); err != nil {
				cl.AllowRebalance()
				return err // ctx ended while retrying
			}
			if err := cl.CommitRecords(ctx, recs...); err != nil && ctx.Err() == nil {
				w.Cfg.Log.Error("kafka commit", "err", err)
			}
		}
		cl.AllowRebalance()
	}
	return nil
}

func (w *HistoryWriter) handle(ctx context.Context, recs []*kgo.Record) error {
	pts := make([]realtime.DurablePoint, 0, len(recs))
	byKey := map[string]*kgo.Record{}
	for _, r := range recs {
		p, err := decodePoint(r.Value)
		if err != nil {
			w.sendDLQ(ctx, r, "malformed", err)
			continue
		}
		pts = append(pts, p)
		byKey[p.SessionID.String()+":"+strconv.FormatInt(p.Seq, 10)] = r
	}
	bad := func(p realtime.DurablePoint, err error) {
		reason := "bad_data"
		if r := byKey[p.SessionID.String()+":"+strconv.FormatInt(p.Seq, 10)]; r != nil {
			w.sendDLQ(ctx, r, reason, err)
		}
	}
	backoff := 500 * time.Millisecond
	for {
		err := writeWithBisect(ctx, pts, func(c context.Context, b []realtime.DurablePoint) error { return InsertPoints(c, w.DB, b) }, bad)
		if err == nil {
			w.written.Add(float64(len(pts)))
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.Cfg.Log.Error("history write failed; retrying", "err", err, "backoff", backoff)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

func (w *HistoryWriter) sendDLQ(ctx context.Context, r *kgo.Record, reason string, cause error) {
	sendDLQ(ctx, w.dlq, w.Cfg.Log, w.dlqCnt, r, reason, cause)
}

func sendDLQ(ctx context.Context, dlq *kgo.Client, log *slog.Logger, cnt *prometheus.CounterVec, r *kgo.Record, reason string, cause error) {
	hdrs := append([]kgo.RecordHeader{}, r.Headers...)
	hdrs = append(hdrs,
		kgo.RecordHeader{Key: "dlq.reason", Value: []byte(reason)},
		kgo.RecordHeader{Key: "dlq.source.topic", Value: []byte(r.Topic)},
		kgo.RecordHeader{Key: "dlq.source.partition", Value: []byte(strconv.Itoa(int(r.Partition)))},
		kgo.RecordHeader{Key: "dlq.source.offset", Value: []byte(strconv.FormatInt(r.Offset, 10))},
		kgo.RecordHeader{Key: "dlq.at", Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))})
	out := &kgo.Record{Topic: r.Topic + TopicDLQSuffix, Key: r.Key, Value: r.Value, Headers: hdrs}
	log.Error("record to DLQ", "topic", r.Topic, "partition", r.Partition, "offset", r.Offset, "reason", reason, "err", fmt.Sprint(cause))
	if cnt != nil {
		cnt.WithLabelValues(r.Topic, reason).Inc()
	}
	if err := dlq.ProduceSync(ctx, out).FirstErr(); err != nil {
		log.Error("DLQ produce failed", "err", err)
	}
}

// Notifier is the cg.notifier consumer for the trip-events and SOS topics (OUTBOX_SINK=kafka). The actual
// work (fan-out to clients, notifications, push) is done by the Dispatcher so inproc and Kafka behave alike.
type Notifier struct {
	D      *Dispatcher
	Cfg    KafkaConfig
	Group  string
	Topics []string
}

func (n *Notifier) Run(ctx context.Context) error {
	if n.Group == "" {
		n.Group = "cg.notifier"
	}
	if len(n.Topics) == 0 {
		n.Topics = []string{"motolink.trip-events.v1", "motolink.sos.v1"}
	}
	log := n.Cfg.Log
	if log == nil {
		log = slog.Default()
	}
	cl, err := kgo.NewClient(kgo.SeedBrokers(n.Cfg.Brokers...), kgo.ConsumerGroup(n.Group), kgo.ConsumeTopics(n.Topics...),
		kgo.DisableAutoCommit(), kgo.BlockRebalanceOnPoll(), kgo.FetchMaxWait(250*time.Millisecond))
	if err != nil {
		return err
	}
	defer cl.Close()
	dlq, err := NewProducerClient(n.Cfg)
	if err != nil {
		return err
	}
	defer dlq.Close()
	var cnt *prometheus.CounterVec
	for ctx.Err() == nil {
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			cl.AllowRebalance()
			return nil
		}
		var done []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			// bounded in-process retries with backoff, then DLQ (docs/design/09 §5.2)
			var err error
			for attempt := 0; attempt < 5; attempt++ {
				if err = n.D.Handle(ctx, r.Value); err == nil || ctx.Err() != nil {
					break
				}
				select {
				case <-ctx.Done():
				case <-time.After(time.Second << attempt):
				}
			}
			if err != nil && ctx.Err() == nil {
				sendDLQ(ctx, dlq, log, cnt, r, "handler_failed", err)
			}
			if ctx.Err() == nil {
				done = append(done, r)
			}
		})
		if len(done) > 0 {
			if err := cl.CommitRecords(ctx, done...); err != nil && ctx.Err() == nil {
				log.Error("kafka commit", "err", err)
			}
		}
		cl.AllowRebalance()
	}
	return nil
}
