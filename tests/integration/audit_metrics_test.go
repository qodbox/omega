package integration

import (
	"fmt"
	"net/http"
	"testing"
)

func TestUnknownPathsDoNotCreateMetricBuckets(t *testing.T) {
	s := boot(t)

	before := metricRouteCount(t, s)

	for i := 0; i < 40; i++ {
		s.get(fmt.Sprintf("/api/does-not-exist-%d", i))
	}

	after := metricRouteCount(t, s)

	if after-before > 5 {
		t.Fatalf("40 URL inconnues ont cree %d compteurs de metriques (avant %d, apres %d): cardinalite non bornee",
			after-before, before, after)
	}
}

func metricRouteCount(t *testing.T, s *server) int {
	t.Helper()

	answer := s.get("/api/stats")
	if answer.status != http.StatusOK {
		t.Skipf("/api/stats indisponible: status = %d", answer.status)
	}

	routes, ok := answer.body["routes"].(map[string]any)
	if !ok {
		t.Skipf("pas de section routes dans /api/stats: %s", truncate(answer.raw))
	}
	return len(routes)
}
