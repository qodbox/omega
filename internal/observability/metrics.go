package observability

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gofiber/fiber/v2"
)

type route struct {
	requests atomic.Int64
	errors   atomic.Int64
	nanos    atomic.Int64
}

type Metrics struct {
	mu       sync.RWMutex
	routes   map[string]*route
	started  time.Time
	inFlight atomic.Int64
}

func New() *Metrics {
	return &Metrics{routes: map[string]*route{}, started: time.Now()}
}

func (m *Metrics) bucket(key string) *route {
	m.mu.RLock()
	found, ok := m.routes[key]
	m.mu.RUnlock()
	if ok {
		return found
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if found, ok := m.routes[key]; ok {
		return found
	}
	fresh := &route{}
	m.routes[key] = fresh
	return fresh
}

func (m *Metrics) Middleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		started := time.Now()
		m.inFlight.Add(1)

		err := c.Next()

		m.inFlight.Add(-1)
		status := c.Response().StatusCode()
		if err != nil {
			status = fiber.StatusInternalServerError
		}

		pattern := c.Route().Path
		if pattern == "" {
			pattern = c.Path()
		}

		bucket := m.bucket(c.Method() + " " + pattern)
		bucket.requests.Add(1)
		bucket.nanos.Add(time.Since(started).Nanoseconds())
		if status >= 500 {
			bucket.errors.Add(1)
		}
		return err
	}
}

func (m *Metrics) Handler() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Type("txt", "utf-8")
		return c.SendString(m.Render())
	}
}

func (m *Metrics) Render() string {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)

	var out strings.Builder

	write := func(name, help, kind string, value string, labels ...string) {
		fmt.Fprintf(&out, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, kind)
		if len(labels) == 0 {
			fmt.Fprintf(&out, "%s %s\n", name, value)
		}
	}

	write("omega_uptime_seconds", "Seconds since the process started", "gauge",
		strconv.FormatFloat(time.Since(m.started).Seconds(), 'f', 0, 64))
	write("omega_goroutines", "Goroutines currently running", "gauge",
		strconv.Itoa(runtime.NumGoroutine()))
	write("omega_memory_bytes", "Heap memory in use", "gauge",
		strconv.FormatUint(memory.HeapAlloc, 10))
	write("omega_gc_total", "Completed garbage collections", "counter",
		strconv.FormatUint(uint64(memory.NumGC), 10))
	write("omega_requests_in_flight", "Requests being served right now", "gauge",
		strconv.FormatInt(m.inFlight.Load(), 10))

	m.mu.RLock()
	keys := make([]string, 0, len(m.routes))
	for key := range m.routes {
		keys = append(keys, key)
	}
	m.mu.RUnlock()
	sort.Strings(keys)

	out.WriteString("# HELP omega_requests_total Requests served per route\n# TYPE omega_requests_total counter\n")
	for _, key := range keys {
		method, path, _ := strings.Cut(key, " ")
		bucket := m.bucket(key)
		fmt.Fprintf(&out, "omega_requests_total{method=%q,route=%q} %d\n", method, path, bucket.requests.Load())
	}

	out.WriteString("# HELP omega_errors_total Responses with a 5xx status\n# TYPE omega_errors_total counter\n")
	for _, key := range keys {
		method, path, _ := strings.Cut(key, " ")
		bucket := m.bucket(key)
		fmt.Fprintf(&out, "omega_errors_total{method=%q,route=%q} %d\n", method, path, bucket.errors.Load())
	}

	out.WriteString("# HELP omega_request_seconds_total Time spent serving a route\n# TYPE omega_request_seconds_total counter\n")
	for _, key := range keys {
		method, path, _ := strings.Cut(key, " ")
		bucket := m.bucket(key)
		seconds := float64(bucket.nanos.Load()) / float64(time.Second)
		fmt.Fprintf(&out, "omega_request_seconds_total{method=%q,route=%q} %s\n",
			method, path, strconv.FormatFloat(seconds, 'f', 6, 64))
	}

	return out.String()
}

func (m *Metrics) Snapshot() map[string]any {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)

	m.mu.RLock()
	routes := make(map[string]any, len(m.routes))
	for key, bucket := range m.routes {
		requests := bucket.requests.Load()
		average := 0.0
		if requests > 0 {
			average = float64(bucket.nanos.Load()) / float64(requests) / float64(time.Millisecond)
		}
		routes[key] = map[string]any{
			"requests":   requests,
			"errors":     bucket.errors.Load(),
			"average_ms": average,
		}
	}
	m.mu.RUnlock()

	return map[string]any{
		"uptime_seconds": int(time.Since(m.started).Seconds()),
		"goroutines":     runtime.NumGoroutine(),
		"memory_bytes":   memory.HeapAlloc,
		"in_flight":      m.inFlight.Load(),
		"routes":         routes,
	}
}
