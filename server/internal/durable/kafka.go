package durable

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"

	"motolink/internal/realtime"
)

// Topic names (docs/design/09 §2).
const (
	TopicLocation  = "motolink.location.v1"
	TopicDLQSuffix = ".dlq"
)

// KafkaConfig is the connection setup shared by producers and consumers.
type KafkaConfig struct {
	Brokers []string
	Log     *slog.Logger
	Reg     prometheus.Registerer
}

// NewProducerClient builds the idempotent, acks=all producer client of docs/design/09 §4.
func NewProducerClient(cfg KafkaConfig) (*kgo.Client, error) {
	return kgo.NewClient(
		kgo.SeedBrokers(cfg.Brokers...),
		kgo.RequiredAcks(kgo.AllISRAcks()),
		kgo.ProducerBatchCompression(kgo.ZstdCompression(), kgo.SnappyCompression(), kgo.NoCompression()),
		kgo.ProducerLinger(50*time.Millisecond),
		kgo.ProducerBatchMaxBytes(256<<10),
		kgo.MaxBufferedRecords(10_000),
		kgo.MaxBufferedBytes(16<<20),
		kgo.RecordPartitioner(kgo.UniformBytesPartitioner(64<<10, true, true, nil)),
	)
}

// locationJSON is the location.point envelope (docs/design/09 §3.1).
type locationJSON struct {
	TripID       uuid.UUID `json:"trip_id"`
	UserID       uuid.UUID `json:"user_id"`
	SessionID    uuid.UUID `json:"session_id"`
	SessionMonth string    `json:"session_month"`
	Seq          int64     `json:"seq"`
	RecordedAt   int64     `json:"recorded_at"`
	ReceivedAt   int64     `json:"received_at"`
	Lat          float64   `json:"lat"`
	Lng          float64   `json:"lng"`
	AccuracyM    *float32  `json:"accuracy_m,omitempty"`
	SpeedMps     *float32  `json:"speed_mps,omitempty"`
	HeadingDeg   *float32  `json:"heading_deg,omitempty"`
	AltitudeM    *float64  `json:"altitude_m,omitempty"`
	Suspect      bool      `json:"suspect"`
}

func encodePoint(p realtime.DurablePoint) ([]byte, error) {
	return json.Marshal(locationJSON{TripID: p.TripID, UserID: p.UserID, SessionID: p.SessionID,
		SessionMonth: p.SessionMonth.Format("2006-01-02"), Seq: p.Seq, RecordedAt: p.RecordedAt.UnixMilli(),
		ReceivedAt: p.ReceivedAt.UnixMilli(), Lat: p.Lat, Lng: p.Lng, AccuracyM: p.AccuracyM, SpeedMps: p.SpeedMps,
		HeadingDeg: p.HeadingDeg, AltitudeM: p.AltitudeM, Suspect: p.Suspect})
}

func decodePoint(b []byte) (realtime.DurablePoint, error) {
	var j locationJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return realtime.DurablePoint{}, err
	}
	month, err := time.Parse("2006-01-02", j.SessionMonth)
	if err != nil || j.Seq <= 0 || j.SessionID == uuid.Nil || j.TripID == uuid.Nil || j.UserID == uuid.Nil {
		return realtime.DurablePoint{}, fmt.Errorf("invalid location record")
	}
	return realtime.DurablePoint{TripID: j.TripID, UserID: j.UserID, SessionID: j.SessionID, SessionMonth: month, Seq: j.Seq,
		RecordedAt: time.UnixMilli(j.RecordedAt), ReceivedAt: time.UnixMilli(j.ReceivedAt), Lat: j.Lat, Lng: j.Lng,
		AccuracyM: j.AccuracyM, SpeedMps: j.SpeedMps, HeadingDeg: j.HeadingDeg, AltitudeM: j.AltitudeM, Suspect: j.Suspect}, nil
}

// KafkaSink is the default history sink (ADR-005). Enqueue never blocks: a full producer buffer yields
// ErrSinkFull, so the point is not acknowledged and the client resends it.
type KafkaSink struct {
	cl     *kgo.Client
	mu     sync.RWMutex
	done   func(uuid.UUID, int64)
	log    *slog.Logger
	errors *prometheus.CounterVec
}

func NewKafkaSink(cl *kgo.Client, log *slog.Logger, reg prometheus.Registerer) *KafkaSink {
	if log == nil {
		log = slog.Default()
	}
	s := &KafkaSink{cl: cl, log: log, errors: prometheus.NewCounterVec(
		prometheus.CounterOpts{Name: "motolink_kafka_produce_errors_total", Help: "Kafka produce failures by reason."}, []string{"reason"})}
	if reg != nil {
		reg.MustRegister(s.errors)
	}
	return s
}

func (s *KafkaSink) SetDone(fn func(uuid.UUID, int64)) {
	s.mu.Lock()
	s.done = fn
	s.mu.Unlock()
}

func (s *KafkaSink) Enqueue(p realtime.DurablePoint) error {
	val, err := encodePoint(p)
	if err != nil {
		return err
	}
	rec := &kgo.Record{Topic: TopicLocation, Key: []byte(p.SessionID.String()), Value: val, Headers: []kgo.RecordHeader{
		{Key: "event_id", Value: []byte(p.SessionID.String() + ":" + strconv.FormatInt(p.Seq, 10))},
		{Key: "type", Value: []byte("location.point")}, {Key: "schema", Value: []byte("1")},
		{Key: "produced_at", Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))}}}
	var full atomic.Bool
	sid, seq := p.SessionID, p.Seq
	s.cl.TryProduce(context.Background(), rec, func(_ *kgo.Record, err error) {
		switch {
		case err == nil:
			s.mu.RLock()
			done := s.done
			s.mu.RUnlock()
			if done != nil {
				done(sid, seq)
			}
		case errors.Is(err, kgo.ErrMaxBuffered):
			full.Store(true) // reported synchronously by TryProduce
		default:
			s.errors.WithLabelValues("produce").Inc()
			s.log.Error("kafka produce failed", "session_id", sid, "seq", seq, "err", err) // never log coordinates
		}
	})
	if full.Load() {
		return realtime.ErrSinkFull
	}
	return nil
}

// WritePoints produces synchronously (REST backfill path).
func (s *KafkaSink) WritePoints(ctx context.Context, pts []realtime.DurablePoint) error {
	recs := make([]*kgo.Record, 0, len(pts))
	for _, p := range pts {
		val, err := encodePoint(p)
		if err != nil {
			return err
		}
		recs = append(recs, &kgo.Record{Topic: TopicLocation, Key: []byte(p.SessionID.String()), Value: val})
	}
	return s.cl.ProduceSync(ctx, recs...).FirstErr()
}

// KafkaPublisher ships outbox rows to their topic keyed by trip (OUTBOX_SINK=kafka).
func KafkaPublisher(cl *kgo.Client) Publisher {
	return PublisherFunc(func(ctx context.Context, msgs []Message) error {
		recs := make([]*kgo.Record, len(msgs))
		for i, m := range msgs {
			recs[i] = &kgo.Record{Topic: m.Topic, Key: []byte(m.Key), Value: m.Payload, Headers: []kgo.RecordHeader{
				{Key: "event_id", Value: []byte(m.EventID.String())}, {Key: "schema", Value: []byte("1")},
				{Key: "produced_at", Value: []byte(strconv.FormatInt(time.Now().UnixMilli(), 10))}}}
		}
		return cl.ProduceSync(ctx, recs...).FirstErr()
	})
}
