package observability

import (
	"io"
	"sync"
)

// Collector appends its own series, already in the Prometheus text format, to
// the /metrics output. The queue registers one for its counters and depth.
type Collector func(w io.Writer)

var (
	collectorsMu sync.RWMutex
	collectors   = map[*Metrics][]Collector{}
)

// Collect registers a collector rendered at the end of every scrape.
func (m *Metrics) Collect(collector Collector) {
	collectorsMu.Lock()
	defer collectorsMu.Unlock()
	collectors[m] = append(collectors[m], collector)
}

func (m *Metrics) renderCollectors(w io.Writer) {
	collectorsMu.RLock()
	registered := append([]Collector(nil), collectors[m]...)
	collectorsMu.RUnlock()

	for _, collector := range registered {
		collector(w)
	}
}
