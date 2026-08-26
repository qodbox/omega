package integration

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestGraphQLLimitsAliasAmplification(t *testing.T) {
	s := boot(t)
	s.signUp("amplify@integration.test")

	aliases := make([]string, 0, 300)
	for i := 0; i < 300; i++ {
		aliases = append(aliases, fmt.Sprintf("a%d: users(limit: 200) { id email }", i))
	}
	query := "{ " + strings.Join(aliases, " ") + " }"

	started := time.Now()
	answer := s.post("/graphql", map[string]any{"query": query})
	elapsed := time.Since(started)

	if answer.status == http.StatusOK && elapsed > 2*time.Second {
		t.Fatalf("300 alias executes en %s sans limite d'amplification", elapsed)
	}
}

func TestGraphQLHidesSensitiveColumns(t *testing.T) {
	s := boot(t)
	s.signUp("gqlhidden@integration.test")

	answer := s.post("/graphql", map[string]any{"query": "{ users { password } }"})
	if answer.status == http.StatusOK && !strings.Contains(answer.raw, "error") {
		t.Fatalf("la colonne password est interrogeable en GraphQL: %s", truncate(answer.raw))
	}
}

func TestGraphQLRejectsAMalformedQuery(t *testing.T) {
	s := boot(t)
	s.signUp("gqlbad@integration.test")

	answer := s.post("/graphql", map[string]any{"query": "{ users { "})
	if answer.status >= http.StatusInternalServerError {
		t.Fatalf("requete malformee: status = %d, want < 500 — %s", answer.status, truncate(answer.raw))
	}
}
