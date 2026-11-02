package queue

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"
)

// WriteMetrics writes the queue counters of this process and the depth of
// the jobs table in the Prometheus text format. The depth is read from the
// database, so every process (API, worker) reports the same numbers; the
// counters are per process.
func (q *Queue) WriteMetrics(w io.Writer) {
	stats := q.Stats()
	counters := []struct {
		name, help string
		value      int64
	}{
		{"omega_queue_jobs_processed_total", "Jobs completed by this process", stats.Processed},
		{"omega_queue_jobs_failed_total", "Jobs that gave up in this process", stats.Failed},
		{"omega_queue_jobs_retried_total", "Failed attempts scheduled again by this process", stats.Retried},
		{"omega_queue_jobs_interrupted_total", "Jobs released by a shutdown of this process", stats.Interrupted},
		{"omega_queue_jobs_reclaimed_total", "Jobs taken back from dead workers by this process", stats.Reclaimed},
		{"omega_queue_jobs_lost_total", "Runs whose claim was taken by another worker", stats.Lost},
		{"omega_queue_store_errors_total", "Job statuses this process could not record", stats.StoreErrors},
	}
	for _, counter := range counters {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n",
			counter.name, counter.help, counter.name, counter.name, counter.value)
	}

	if q.store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	counts, err := q.store.Counts(ctx)
	if err != nil {
		return
	}

	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)

	fmt.Fprint(w, "# HELP omega_queue_jobs Jobs in the table per status\n# TYPE omega_queue_jobs gauge\n")
	for _, status := range statuses {
		fmt.Fprintf(w, "omega_queue_jobs{status=%q} %d\n", status, counts[status])
	}
}
