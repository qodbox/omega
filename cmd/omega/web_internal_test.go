package main

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

func TestEveryStackShipsAWorkingSkeleton(t *testing.T) {
	for _, target := range stacks {
		t.Run(target.name, func(t *testing.T) {
			root := "webkit/" + target.name

			files := map[string]bool{}
			err := fs.WalkDir(webkit, root, func(path string, entry fs.DirEntry, err error) error {
				if err != nil || entry.IsDir() {
					return err
				}
				files[strings.TrimPrefix(path, root+"/")] = true
				return nil
			})
			if err != nil {
				t.Fatalf("gabarit illisible: %v", err)
			}

			for _, needed := range []string{"package.json", "components.json"} {
				if !files[needed] {
					t.Errorf("%s manquant", needed)
				}
			}
			if len(files) < 20 {
				t.Errorf("%d fichier(s) seulement — le gabarit ne porte pas l'application complete", len(files))
			}
		})
	}
}

func TestEveryStackShipsTheDashboardAndTheDocs(t *testing.T) {
	pages := map[string][]string{
		"nuxt": {
			"app/pages/index.vue", "app/pages/login.vue", "app/pages/dashboard.vue",
			"app/pages/users/index.vue", "app/pages/docs/[[slug]].vue",
		},
		"react": {
			"src/pages/home.tsx", "src/pages/login.tsx", "src/pages/dashboard.tsx",
			"src/pages/users.tsx", "src/pages/docs.tsx",
		},
		"next": {
			"app/(site)/page.tsx", "app/(site)/login/page.tsx", "app/(site)/docs/[[...slug]]/page.tsx",
			"app/(app)/dashboard/page.tsx", "app/(app)/users/page.tsx",
			"app/layout.tsx", "app/(site)/layout.tsx", "app/(app)/layout.tsx",
		},
	}

	for _, target := range stacks {
		expected, known := pages[target.name]
		if !known {
			t.Errorf("%s: aucune liste de pages attendue", target.name)
			continue
		}
		for _, page := range expected {
			if _, err := webkit.ReadFile("webkit/" + target.name + "/" + page); err != nil {
				t.Errorf("%s: %s manquante", target.name, page)
			}
		}
	}
}

func TestSharedContentIsNotDuplicatedPerStack(t *testing.T) {
	for _, name := range []string{"tokens.css", "api-types.ts", "docs.ts", "locales/fr.json"} {
		if _, err := webkit.ReadFile("webkit/shared/" + name); err != nil {
			t.Errorf("partage manquant: %s", name)
		}
	}

	for _, target := range stacks {
		for source, destination := range target.shared {
			if _, err := webkit.ReadFile("webkit/shared/" + source); err != nil {
				t.Errorf("%s pointe sur un partage inexistant: %s", target.name, source)
			}
			if _, err := webkit.ReadFile("webkit/" + target.name + "/" + filepath.ToSlash(destination)); err == nil {
				t.Errorf("%s duplique %s au lieu de le partager", target.name, destination)
			}
		}
	}
}

func TestEveryStackConfiguresShadcn(t *testing.T) {
	for _, target := range stacks {
		raw, err := webkit.ReadFile("webkit/" + target.name + "/components.json")
		if err != nil {
			t.Errorf("%s: components.json manquant", target.name)
			continue
		}

		var config struct {
			Tailwind struct {
				CSS string `json:"css"`
			} `json:"tailwind"`
			Aliases map[string]string `json:"aliases"`
		}
		if err := json.Unmarshal(raw, &config); err != nil {
			t.Errorf("%s: components.json illisible: %v", target.name, err)
			continue
		}
		if config.Tailwind.CSS == "" {
			t.Errorf("%s: components.json ne designe pas la feuille de styles", target.name)
		}
		if config.Aliases["ui"] == "" {
			t.Errorf("%s: components.json ne designe pas le dossier des composants", target.name)
		}
		if len(target.components) == 0 {
			t.Errorf("%s: aucune liste de composants shadcn", target.name)
		}
	}
}

func TestEveryStackSharesTheSameApiClient(t *testing.T) {
	if _, err := webkit.ReadFile("webkit/shared/api.ts"); err != nil {
		t.Fatalf("client partage absent: %v", err)
	}

	for _, target := range stacks {
		destination, mapped := target.shared["api.ts"]
		if !mapped {
			t.Errorf("%s n'utilise pas le client partage", target.name)
			continue
		}

		// A per-stack copy would let the sign-in flows drift apart.
		if _, err := webkit.ReadFile("webkit/" + target.name + "/" + filepath.ToSlash(destination)); err == nil {
			t.Errorf("%s embarque son propre %s", target.name, destination)
		}
	}
}

func TestTheSharedClientCarriesTheWholeAuthFlow(t *testing.T) {
	raw, err := webkit.ReadFile("webkit/shared/api.ts")
	if err != nil {
		t.Fatal(err)
	}

	body := string(raw)
	for _, piece := range []string{
		"/auth/login", "/auth/register", "/auth/refresh", "/auth/logout", "/auth/me",
		"omega.tokens", "Bearer ", "subscribe", "ApiError",
	} {
		if !strings.Contains(body, piece) {
			t.Errorf("le client partage n'implemente pas %q", piece)
		}
	}
}

func TestEveryStackShipsACollapsibleSidebar(t *testing.T) {
	// The file to read, then the markers proving the collapse is wired.
	sidebars := map[string]struct {
		file    string
		markers []string
	}{
		"nuxt": {
			"app/components/AppSidebar.vue",
			[]string{"useSidebar", "collapsed", "w-rail"},
		},
		"react": {
			"src/components/app-sidebar.tsx",
			[]string{`collapsible="icon"`},
		},
		"next": {
			"components/app-sidebar.tsx",
			[]string{`collapsible="icon"`},
		},
	}

	for _, target := range stacks {
		spec, known := sidebars[target.name]
		if !known {
			t.Errorf("%s: aucune sidebar attendue declaree", target.name)
			continue
		}

		raw, err := webkit.ReadFile("webkit/" + target.name + "/" + spec.file)
		if err != nil {
			t.Errorf("%s: %s introuvable", target.name, spec.file)
			continue
		}
		for _, marker := range spec.markers {
			if !strings.Contains(string(raw), marker) {
				t.Errorf("%s: la sidebar n'est pas repliable (%q absent)", target.name, marker)
			}
		}
	}
}

func TestEveryStackMountsItsSidebar(t *testing.T) {
	// A collapsible sidebar nothing renders is worth nothing.
	mounts := map[string][]string{
		"nuxt":  {"app/layouts/dashboard.vue"},
		"react": {"src/App.tsx"},
		"next":  {"app/(app)/layout.tsx"},
	}

	for _, target := range stacks {
		for _, file := range mounts[target.name] {
			raw, err := webkit.ReadFile("webkit/" + target.name + "/" + file)
			if err != nil {
				t.Errorf("%s: %s introuvable", target.name, file)
				continue
			}
			body := string(raw)
			if !strings.Contains(body, "AppSidebar") {
				t.Errorf("%s: %s ne monte pas la sidebar", target.name, file)
			}
			if !strings.Contains(body, "SidebarTrigger") && !strings.Contains(body, "toggle") {
				t.Errorf("%s: %s n'offre aucun declencheur de repli", target.name, file)
			}
		}
	}
}

func TestEveryStackRetargetsTheApiWithOmegaUrl(t *testing.T) {
	// The port is frozen at scaffolding time; OMEGA_URL must be able to
	// replace it without reopening a configuration file.
	configs := map[string]string{
		"nuxt":  "nuxt.config.ts",
		"react": "vite.config.ts",
		"next":  "next.config.ts",
	}

	for _, target := range stacks {
		raw, err := webkit.ReadFile("webkit/" + target.name + "/" + configs[target.name])
		if err != nil {
			t.Errorf("%s: %s introuvable", target.name, configs[target.name])
			continue
		}

		body := string(raw)
		if !strings.Contains(body, "OMEGA_URL") {
			t.Errorf("%s ne lit pas OMEGA_URL", target.name)
		}
		// Without this exact port, the real-port substitution never applies.
		if !strings.Contains(body, defaultAPI) {
			t.Errorf("%s ne contient pas %s, la substitution du port ne prendra pas",
				target.name, defaultAPI)
		}
	}
}

func TestEveryStackPinsItsDevPort(t *testing.T) {
	// Sliding onto the API port would loop the proxy back on itself.
	for _, target := range stacks {
		files := map[string]string{
			"nuxt":  "nuxt.config.ts",
			"react": "vite.config.ts",
			"next":  "package.json",
		}

		raw, err := webkit.ReadFile("webkit/" + target.name + "/" + files[target.name])
		if err != nil {
			t.Errorf("%s: %s introuvable", target.name, files[target.name])
			continue
		}
		if !strings.Contains(string(raw), "WEB_PORT") {
			t.Errorf("%s ne permet pas de choisir son port de dev", target.name)
		}
	}
}
