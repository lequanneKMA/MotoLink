package realtime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// bucket is a token bucket: rate tokens/second up to burst.
type bucket struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	tokens  float64
	last    time.Time
	now     func() time.Time
	strikes atomic.Int32
}

func newBucket(rate, burst float64) *bucket {
	return &bucket{rate: rate, burst: burst, tokens: burst, last: time.Now(), now: time.Now}
}

func (b *bucket) allow(n float64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.now()
	b.tokens += t.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = t
	if b.tokens < n {
		return false
	}
	b.tokens -= n
	return true
}

func codeLabel(c fmt.Stringer) string { return c.String() }

// Metrics (docs/design/13 §5.2). With a nil registerer the collectors still work, they are just not exported.
type Metrics struct {
	connections      prometheus.Gauge
	connects         *prometheus.CounterVec
	closes           *prometheus.CounterVec
	slowClose        prometheus.Counter
	dropped          *prometheus.CounterVec
	coalesced        prometheus.Counter
	points           *prometheus.CounterVec
	fanoutLatency    prometheus.Histogram
	pubsubErrors     prometheus.Counter
	pubsubReconnects prometheus.Counter
	msgsIn           *prometheus.CounterVec
	ingestQueue      *prometheus.GaugeVec
}

func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		connections:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "ws_connections", Help: "Open WebSocket connections."}),
		connects:         prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ws_connect_total", Help: "Upgrade attempts by result."}, []string{"result"}),
		closes:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ws_close_total", Help: "Closed connections by close code."}, []string{"code"}),
		slowClose:        prometheus.NewCounter(prometheus.CounterOpts{Name: "ws_slow_consumer_closes_total", Help: "Connections closed because their control queue overflowed."}),
		dropped:          prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ws_location_dropped_total", Help: "Live positions dropped by reason."}, []string{"reason"}),
		coalesced:        prometheus.NewCounter(prometheus.CounterOpts{Name: "ws_location_coalesced_total", Help: "Queued positions replaced by a newer one."}),
		points:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gps_points_total", Help: "GPS points by result."}, []string{"result"}),
		fanoutLatency:    prometheus.NewHistogram(prometheus.HistogramOpts{Name: "fanout_latency_seconds", Help: "Server receive to socket write of a live position.", Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5}}),
		pubsubErrors:     prometheus.NewCounter(prometheus.CounterOpts{Name: "pubsub_publish_errors_total", Help: "Failed bus publishes."}),
		pubsubReconnects: prometheus.NewCounter(prometheus.CounterOpts{Name: "pubsub_reconnects_total", Help: "Bus reconnects that may have lost messages."}),
		msgsIn:           prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ws_msgs_in_total", Help: "Inbound messages by type."}, []string{"type"}),
		ingestQueue:      prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "ingest_queue_depth", Help: "Ingest queue depth per worker."}, []string{"worker"}),
	}
	if reg != nil {
		reg.MustRegister(m.connections, m.connects, m.closes, m.slowClose, m.dropped, m.coalesced, m.points,
			m.fanoutLatency, m.pubsubErrors, m.pubsubReconnects, m.msgsIn, m.ingestQueue)
	}
	return m
}

// randHex returns n random bytes as hex. (UUIDv7 prefixes are timestamps, so they are NOT unique enough for ids.)
func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
