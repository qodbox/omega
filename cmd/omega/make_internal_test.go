package main

import (
	"path/filepath"

	"github.com/spf13/cobra"
	"strings"
	"testing"
)

func stubFor(t *testing.T, name string) string {
	t.Helper()

	raw, err := stubs.ReadFile("stubs/" + name + ".stub")
	if err != nil {
		t.Fatalf("stub %s: %v", name, err)
	}
	return string(raw)
}

func TestNoStubImportsARemovedLayerPackage(t *testing.T) {
	removed := []string{
		`"omega/app/services"`, `"omega/app/controllers"`,
		`"omega/app/requests"`, `"omega/app/presenters"`,
	}

	entries, err := stubs.ReadDir("stubs")
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		body := stubFor(t, strings.TrimSuffix(entry.Name(), ".stub"))
		for _, gone := range removed {
			if strings.Contains(body, gone) {
				t.Errorf("%s importe %s, qui n'existe plus", entry.Name(), gone)
			}
		}
	}
}

func TestDomainStubsDeclareTheirDirectoryPackage(t *testing.T) {
	for _, name := range []string{"controller", "controller_resource", "service", "request"} {
		body := stubFor(t, name)
		if !strings.HasPrefix(body, "package {{.Package}}\n") {
			first, _, _ := strings.Cut(body, "\n")
			t.Errorf("%s declare %q au lieu de package {{.Package}}", name, first)
		}
	}
}

func TestGeneratorsWriteIntoTheirLayer(t *testing.T) {
	n := names{Name: "Facture", Var: "facture", Snake: "facture", Plural: "factures"}

	expected := map[string]string{
		"controller": filepath.Join("app", "controllers", "facture", "controller.go"),
		"service":    filepath.Join("app", "services", "facture", "service.go"),
		"request":    filepath.Join("app", "requests", "facture", "requests.go"),
	}

	for _, gen := range generators {
		want, tracked := expected[gen.name]
		if !tracked {
			continue
		}
		if got := gen.path(n); got != want {
			t.Errorf("%s ecrit dans %s, want %s", gen.name, got, want)
		}
	}
}

func TestTheDomainPackageIsDerivedFromTheDestination(t *testing.T) {
	cases := map[string]string{
		filepath.Join("app", "services", "facture", "service.go"): "facture",
		filepath.Join("app", "models", "facture.go"):              "models",
		filepath.Join("app", "jobs", "envoi.go"):                  "jobs",
	}

	for dest, want := range cases {
		if got := filepath.Base(filepath.Dir(dest)); got != want {
			t.Errorf("%s -> paquet %q, want %q", dest, got, want)
		}
	}
}

func TestStandaloneStubsDoNotDependOnSiblings(t *testing.T) {
	siblings := []string{"*Service", "Service{", "Payload", "models.", "factories."}

	for _, name := range []string{"controller", "service", "request", "seed", "factory", "job", "listener", "middleware"} {
		body := stubFor(t, name)
		for _, symbol := range siblings {
			if strings.Contains(body, symbol) && !isOwnSymbol(name, symbol) {
				t.Errorf("%s.stub reference %q, qui n'existe pas quand la commande est lancee seule", name, symbol)
			}
		}
	}
}

func isOwnSymbol(stub, symbol string) bool {
	switch stub {
	case "service":
		return symbol == "*Service" || symbol == "Service{"
	case "request":
		return symbol == "Payload"
	}
	return false
}

func TestResourceStubsMayDependOnTheirSlice(t *testing.T) {
	for _, name := range []string{"controller_resource", "service_resource"} {
		if body := stubFor(t, name); !strings.Contains(body, "Payload") {
			t.Errorf("%s.stub devrait utiliser Payload, genere avec lui", name)
		}
	}
}

func TestModelExposesTheArtisanFlags(t *testing.T) {
	var model *cobra.Command
	for _, cmd := range makeCommands() {
		if strings.HasPrefix(cmd.Use, "make:model") {
			model = cmd
		}
	}
	if model == nil {
		t.Fatal("make:model introuvable")
	}

	for shorthand, name := range map[string]string{
		"m": "migration", "c": "controller", "s": "seeder",
		"f": "factory", "r": "resource", "a": "all",
	} {
		flag := model.Flags().ShorthandLookup(shorthand)
		if flag == nil {
			t.Errorf("-%s absent", shorthand)
			continue
		}
		if flag.Name != name {
			t.Errorf("-%s pointe sur %q, want %q", shorthand, flag.Name, name)
		}
	}

	if model.Flags().ShorthandLookup("f").Name == "force" {
		t.Error("-f doit designer --factory, pas --force")
	}
	if model.Flags().Lookup("force") == nil {
		t.Error("--force absent")
	}
}

func TestResourceCreatesAnApiResourceLikeArtisan(t *testing.T) {
	n := names{Name: "Post", Var: "post", Snake: "post", Plural: "posts"}

	var resource *generator
	for index := range generators {
		if generators[index].name == "resource" {
			resource = &generators[index]
		}
	}
	if resource == nil {
		t.Fatal("make:resource introuvable")
	}

	want := filepath.Join("app", "presenters", "post", "presenter.go")
	if got := resource.path(n); got != want {
		t.Fatalf("make:resource ecrit dans %s, want %s", got, want)
	}
	if resource.stub != "presenter" {
		t.Errorf("stub = %q, want presenter", resource.stub)
	}
}

func TestTheResourceControllerGoesThroughThePresenter(t *testing.T) {
	body := stubFor(t, "controller_resource")

	if !strings.Contains(body, "presenter.NewView") || !strings.Contains(body, "presenter.NewViews") {
		t.Error("le controleur de ressource renvoie des modeles bruts")
	}
	if strings.Contains(body, `"data": records}`) || strings.Contains(body, `"data": record}`) {
		t.Error("un modele brut atteint encore la reponse")
	}
}

func TestEveryArtisanCounterpartExists(t *testing.T) {
	expected := []string{
		"controller", "model", "seed", "factory", "service", "request",
		"job", "listener", "middleware", "resource",
		"policy", "rule", "event", "mail", "notification", "observer", "test",
	}

	present := map[string]bool{}
	for _, gen := range generators {
		present[gen.name] = true
	}

	for _, name := range expected {
		if !present[name] {
			t.Errorf("make:%s manquant", name)
		}
	}
}

func TestAliasesAreRegistered(t *testing.T) {
	names := map[string]bool{}
	for _, cmd := range makeCommands() {
		names[strings.Fields(cmd.Use)[0]] = true
	}

	for _, alias := range []string{"make:seeder", "make:presenter"} {
		if !names[alias] {
			t.Errorf("%s absent", alias)
		}
	}
}

func TestSelfRegisteringStubsUseInit(t *testing.T) {
	for _, name := range []string{"policy", "rule", "seed"} {
		if body := stubFor(t, name); !strings.Contains(body, "func init()") {
			t.Errorf("%s.stub ne s'enregistre pas tout seul", name)
		}
	}
}
