package integration

import (
	"net/http"
	"strings"
	"testing"
)

func graphql(t *testing.T, s *server, query string) response {
	t.Helper()
	return s.post("/graphql", map[string]any{"query": query})
}

func TestDiscoveredTablesHaveNoGraphQLMutation(t *testing.T) {
	s := bootWithNotes(t)
	s.signUpAdmin("gqlro@integration.test")

	answer := graphql(t, s, `mutation { create_note(title: "Interdite") { id } }`)
	if !strings.Contains(answer.raw, "error") {
		t.Fatalf("une table decouverte est modifiable en GraphQL: %s", truncate(answer.raw))
	}

	listed := graphql(t, s, `{ notes { id } }`)
	if listed.status != http.StatusOK || strings.Contains(listed.raw, "Cannot query") {
		t.Fatalf("la lecture GraphQL doit rester ouverte: %s", truncate(listed.raw))
	}
}

func TestRegisteredResourcesKeepTheirMutations(t *testing.T) {
	s := boot(t)
	s.signUpAdmin("gqlrw@integration.test")

	answer := graphql(t, s, `mutation { update_user(id: "1", name: "Renomme") { id name } }`)
	if strings.Contains(answer.raw, "Cannot query field") {
		t.Fatalf("une ressource enregistree a perdu ses mutations: %s", truncate(answer.raw))
	}
}

func TestGraphQLMutationsRefuseHiddenFields(t *testing.T) {
	s := boot(t)
	s.signUpAdmin("gqlhidden2@integration.test")

	answer := graphql(t, s, `mutation { update_user(id: "1", password: "vole") { id } }`)
	if !strings.Contains(answer.raw, "error") {
		t.Fatalf("un champ cache est modifiable en GraphQL: %s", truncate(answer.raw))
	}
}
