package httpapi

import "github.com/prometheus/client_golang/prometheus"

// PromTelemetry records anonymous client samples as Prometheus histograms (docs/design/06 §2.9).
type PromTelemetry struct{ age, ack prometheus.Histogram }

func NewPromTelemetry(reg prometheus.Registerer) *PromTelemetry {
	t := &PromTelemetry{
		age: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "product_location_age_seconds",
			Help: "Age of a position when displayed on a client.", Buckets: []float64{1, 2, 5, 10, 20, 30, 60, 120, 300}}),
		ack: prometheus.NewHistogram(prometheus.HistogramOpts{Name: "product_ack_latency_seconds",
			Help: "Client-observed time from sending a point to its ACK.", Buckets: []float64{.1, .25, .5, 1, 2, 5, 10, 30}}),
	}
	if reg != nil {
		reg.MustRegister(t.age, t.ack)
	}
	return t
}

func (t *PromTelemetry) Observe(locationAgeS, ackLatencyS []float64) {
	for _, v := range locationAgeS {
		if v >= 0 && v < 86400 {
			t.age.Observe(v)
		}
	}
	for _, v := range ackLatencyS {
		if v >= 0 && v < 3600 {
			t.ack.Observe(v)
		}
	}
}
