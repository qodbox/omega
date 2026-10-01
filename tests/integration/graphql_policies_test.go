package integration

import (
	"net/http"
	"strings"
	"testing"
)

// GraphQL sert le meme registre que REST, donc il doit poser les memes questions
// aux policies. Il ne le faisait pas : la route n'etait montee qu'avec
// guard.Required(), sans api.Authorize, et aucun resolver ne consultait le gate.
// Un simple porteur de jeton lisait donc par GraphQL tout ce que REST lui
// refusait, y compris la table des tentatives de connexion avec les adresses et
// les IP des autres comptes.

func gql(t *testing.T, s *server, query string) response {
	t.Helper()
	return s.post("/graphql", map[string]any{"query": query})
}

func flat(raw string) string { return strings.Join(strings.Fields(raw), " ") }

func TestGraphQLRefusesWhatRESTRefuses(t *testing.T) {
	s := boot(t)
	s.signUp("gqlpolicy@integration.test")

	rest := s.get("/api/login_attempts")
	if rest.status != http.StatusForbidden {
		t.Fatalf("REST doit refuser login_attempts: %d %s", rest.status, flat(rest.raw))
	}

	answer := gql(t, s, "{ login_attempts { id identifier address } }")
	if !strings.Contains(answer.raw, "you may not login_attempts.list") {
		t.Fatalf("GraphQL doit refuser la meme table: %s", flat(answer.raw))
	}
	if strings.Contains(answer.raw, "@integration.test") {
		t.Fatalf("des identifiants ont fuite malgre le refus: %s", flat(answer.raw))
	}
}

// La fuite se voyait surtout avec des lignes reelles : trois echecs de connexion
// laissent trois identifiants et trois IP en base.
func TestGraphQLLeaksNoLoginAttempt(t *testing.T) {
	s := boot(t)
	s.signUp("victime@integration.test")

	for range 3 {
		s.post("/api/auth/login", map[string]any{
			"email": "victime@integration.test", "password": "mauvais",
		})
	}

	s.signUp("curieux@integration.test")

	answer := gql(t, s, "{ login_attempts { id identifier address } login_attempts_count }")
	if strings.Contains(answer.raw, "victime@integration.test") {
		t.Fatalf("l'identifiant d'un autre compte a fuite: %s", flat(answer.raw))
	}
	if strings.Contains(answer.raw, `"login_attempts_count": 3`) {
		t.Fatalf("le comptage a fuite malgre le refus: %s", flat(answer.raw))
	}
}

func TestGraphQLStillServesWhatThePolicyGrants(t *testing.T) {
	s := boot(t)
	s.signUp("autorise@integration.test")

	// users.list est accorde a tout compte connecte.
	answer := gql(t, s, "{ users { id email } users_count }")
	if strings.Contains(answer.raw, "you may not") {
		t.Fatalf("users.list est accorde et doit passer: %s", flat(answer.raw))
	}
	if !strings.Contains(answer.raw, "autorise@integration.test") {
		t.Fatalf("la liste des comptes doit revenir: %s", flat(answer.raw))
	}
}

func TestGraphQLRefusesAnAnonymousCaller(t *testing.T) {
	s := boot(t)

	answer := gql(t, s, "{ users { id } }")
	if strings.Contains(answer.raw, `"users": [`) {
		t.Fatalf("un appelant sans jeton ne doit rien obtenir: %s", flat(answer.raw))
	}
}

// L'introspection reste ouverte quand la configuration le dit, parce qu'elle ne
// touche aucun resolver et ne lit donc aucune donnee.
func TestGraphQLIntrospectionStaysReachable(t *testing.T) {
	s := boot(t)

	answer := gql(t, s, "{ __schema { queryType { name } } }")
	if !strings.Contains(answer.raw, "Query") {
		t.Fatalf("l'introspection doit rester lisible: %s", flat(answer.raw))
	}
}
