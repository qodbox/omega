package unit

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"omega/internal/policy"
)

type covActorWithMethod struct{ role string }

func (a covActorWithMethod) Role() string { return a.role }

type covActorWithField struct{ Role string }

type covActorWithoutRole struct{ Name string }

type covActorWrongKind struct{ Role int }

func covRegistry() *policy.Registry {
	r := policy.New()
	r.Grant("admin", "*")
	r.Grant("editor", "posts.*", "comments.delete")
	r.Grant("reader", "posts.read")
	return r
}

func TestCovPolicyReadsTheRoleFromAMethod(t *testing.T) {
	r := covRegistry()

	if !r.Allows(context.Background(), covActorWithMethod{role: "reader"}, "posts.read") {
		t.Error("le role venant d'une methode Role() doit etre reconnu")
	}
}

func TestCovPolicyReadsTheRoleFromAField(t *testing.T) {
	r := covRegistry()

	if !r.Allows(context.Background(), covActorWithField{Role: "reader"}, "posts.read") {
		t.Error("le role venant d'un champ Role doit etre reconnu")
	}
	if !r.Allows(context.Background(), &covActorWithField{Role: "reader"}, "posts.read") {
		t.Error("un pointeur vers l'acteur doit fonctionner aussi")
	}
}

func TestCovPolicyRefusesWhatCarriesNoRole(t *testing.T) {
	r := covRegistry()
	ctx := context.Background()

	cases := map[string]any{
		"sans champ Role":        covActorWithoutRole{Name: "x"},
		"champ Role non textuel": covActorWrongKind{Role: 3},
		"pointeur nul":           (*covActorWithField)(nil),
		"valeur simple":          "reader",
		"entier":                 42,
	}

	for name, actor := range cases {
		if r.Allows(ctx, actor, "posts.read") {
			t.Errorf("%s: l'acces aurait du etre refuse", name)
		}
	}
}

func TestCovPolicyHonoursTheWildcardRole(t *testing.T) {
	r := covRegistry()
	ctx := context.Background()

	for _, ability := range []string{"posts.read", "users.delete", "n_importe.quoi"} {
		if !r.Allows(ctx, covActorWithField{Role: "admin"}, ability) {
			t.Errorf("admin refuse sur %q", ability)
		}
	}
}

func TestCovPolicyHonoursAResourceWildcard(t *testing.T) {
	r := covRegistry()
	ctx := context.Background()
	editor := covActorWithField{Role: "editor"}

	for _, ability := range []string{"posts.read", "posts.write", "posts.delete"} {
		if !r.Allows(ctx, editor, ability) {
			t.Errorf("editor refuse sur %q alors que posts.* est accorde", ability)
		}
	}
	if !r.Allows(ctx, editor, "comments.delete") {
		t.Error("editor refuse sur une capacite accordee nommement")
	}
	if r.Allows(ctx, editor, "comments.write") {
		t.Error("editor accepte sur une capacite jamais accordee")
	}
}

func TestCovPolicyRefusesAnAbilityWithoutResource(t *testing.T) {
	r := covRegistry()

	if r.Allows(context.Background(), covActorWithField{Role: "editor"}, "publier") {
		t.Error("une capacite sans point ne doit pas passer par le joker de ressource")
	}
}

func TestCovAbilitiesListsWhatARoleHolds(t *testing.T) {
	r := covRegistry()

	if got := r.Abilities("admin"); len(got) != 1 || got[0] != "*" {
		t.Errorf("Abilities(admin) = %v, attendu [*]", got)
	}

	got := r.Abilities("editor")
	if len(got) != 2 || got[0] != "comments.delete" || got[1] != "posts.*" {
		t.Errorf("Abilities(editor) = %v, attendu une liste triee", got)
	}

	if got := r.Abilities("inconnu"); len(got) != 0 {
		t.Errorf("Abilities(inconnu) = %v, attendu vide", got)
	}
}

func TestCovRolesListsEveryRoleOnce(t *testing.T) {
	r := covRegistry()

	got := r.Roles()
	want := []string{"admin", "editor", "reader"}

	if len(got) != len(want) {
		t.Fatalf("Roles = %v, attendu %v", got, want)
	}
	for i, role := range want {
		if got[i] != role {
			t.Errorf("Roles = %v, attendu %v (trie)", got, want)
		}
	}
}

func TestCovDeniesMirrorsAllows(t *testing.T) {
	r := covRegistry()
	ctx := context.Background()
	actor := covActorWithField{Role: "reader"}

	if r.Denies(ctx, actor, "posts.read") {
		t.Error("Denies doit etre l'inverse d'Allows")
	}
	if !r.Denies(ctx, actor, "posts.delete") {
		t.Error("Denies doit refuser ce qu'Allows refuse")
	}
}

func TestCovMiddlewareGuardsTheRoute(t *testing.T) {
	r := covRegistry()

	cases := []struct {
		name   string
		actor  any
		status int
	}{
		{"sans acteur", nil, fiber.StatusUnauthorized},
		{"role insuffisant", covActorWithField{Role: "reader"}, fiber.StatusForbidden},
		{"role suffisant", covActorWithField{Role: "admin"}, fiber.StatusOK},
	}

	for _, item := range cases {
		guard := r.Middleware(func(*fiber.Ctx) any { return item.actor })

		app := fiber.New()
		app.Get("/", guard("posts.delete"), func(c *fiber.Ctx) error {
			if c.Locals(policy.Local) == nil {
				t.Errorf("%s: l'acteur doit etre depose dans le contexte", item.name)
			}
			return c.SendStatus(fiber.StatusOK)
		})

		response, err := app.Test(httptest.NewRequest("GET", "/", nil), -1)
		if err != nil {
			t.Fatalf("%s: %v", item.name, err)
		}
		_ = response.Body.Close()

		if response.StatusCode != item.status {
			t.Errorf("%s: statut = %d, attendu %d", item.name, response.StatusCode, item.status)
		}
	}
}
