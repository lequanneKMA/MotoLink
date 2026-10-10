// Package metrics owns the Prometheus registry and the metrics HTTP handler.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry wraps a private registry so tests and multiple instances do not
// collide on the global default.
type Registry struct {
	Reg *prometheus.Registry
}

// New returns a registry with the Go runtime and process collectors installed
// (go_goroutines, memstats, process_cpu_seconds_total; docs/design/13 §5.2).
func New() *Registry {
	r := prometheus.NewRegistry()
	r.MustRegister(collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	return &Registry{Reg: r}
}

// Handler serves /metrics.
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.Reg, promhttp.HandlerOpts{})
}
