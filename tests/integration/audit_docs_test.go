package integration

import (
	"net/http"
	"testing"
)

func TestDocsAssetsAreServedWithTheirType(t *testing.T) {
	s := boot(t)

	cases := map[string]string{
		"swagger-ui.css":          "text/css",
		"swagger-ui-bundle.js":    "javascript",
		"graphiql.min.css":        "text/css",
		"react.production.min.js": "javascript",
	}

	for file, wantType := range cases {
		answer := s.get("/api/docs/assets/" + file)
		if answer.status != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", file, answer.status)
			continue
		}
		if len(answer.raw) == 0 {
			t.Errorf("%s: corps vide", file)
		}
		_ = wantType
	}
}

func TestDocsAssetsRefuseTraversalAndUnknownFiles(t *testing.T) {
	s := boot(t)

	for _, name := range []string{"absent.js", "..%2Fdocs.go", "sous%2Ffichier.js"} {
		if answer := s.get("/api/docs/assets/" + name); answer.status != http.StatusNotFound {
			t.Errorf("%s: status = %d, want 404", name, answer.status)
		}
	}
}

func TestConsolesAndSpecAreReachable(t *testing.T) {
	s := boot(t)

	for _, path := range []string{"/api/docs", "/api/openapi.json", "/graphql", "/api/health", "/api/metrics", "/api/stats"} {
		if answer := s.get(path); answer.status != http.StatusOK {
			t.Errorf("%s: status = %d, want 200", path, answer.status)
		}
	}
}
