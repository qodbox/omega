package unit

import (
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"omega/internal/cache"
	"omega/internal/router"
	"omega/internal/storage"
	"omega/internal/validation"
)

func namedRouter(path, name string) *router.Router {
	app := fiber.New()
	app.Get(path, func(c *fiber.Ctx) error { return nil }).Name(name)
	return router.New(app)
}

func TestRouterURLEscapesAPathParameter(t *testing.T) {
	r := namedRouter("/users/:id", "users.show")

	built, err := r.URL("users.show", map[string]any{"id": "a/b"})
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if strings.Count(built, "/") != 2 {
		t.Fatalf("un parametre a injecte un segment: %q", built)
	}
}

func TestRouterURLRejectsATraversingParameter(t *testing.T) {
	r := namedRouter("/files/:name", "files.show")

	built, err := r.URL("files.show", map[string]any{"name": "../../etc/passwd"})
	if err != nil {
		return
	}
	if path.Clean(built) != built || !strings.HasPrefix(built, "/files/") {
		t.Fatalf("URL generee traversante: %q resout en %q", built, path.Clean(built))
	}
}

func TestRouterURLDropsAnOptionalMiddleSegment(t *testing.T) {
	r := namedRouter("/docs/:lang?/page", "docs.page")

	built, err := r.URL("docs.page", map[string]any{})
	if err != nil {
		t.Fatalf("URL: %v", err)
	}
	if strings.Contains(built, "//") {
		t.Fatalf("segment optionnel absent laisse un double slash: %q", built)
	}
}

func TestStorageDoesNotFollowASymlinkOutOfRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("interdit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("liens symboliques indisponibles: %v", err)
	}

	disk, err := storage.New(root, "/files")
	if err != nil {
		t.Fatal(err)
	}

	content, err := disk.Get("link/secret.txt")
	if err == nil {
		t.Fatalf("lecture hors racine via un lien symbolique: %q", content)
	}
}

func TestStorageDoesNotWriteThroughASymlinkOutOfRoot(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	outside := filepath.Join(base, "outside")

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("liens symboliques indisponibles: %v", err)
	}

	disk, err := storage.New(root, "/files")
	if err != nil {
		t.Fatal(err)
	}

	if err := disk.PutBytes("link/planted.txt", []byte("x")); err == nil {
		if _, statErr := os.Stat(filepath.Join(outside, "planted.txt")); statErr == nil {
			t.Fatal("ecriture hors racine via un lien symbolique")
		}
	}
}

func TestRememberBuildsOnceUnderConcurrency(t *testing.T) {
	store := cache.New()

	var builds atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = cache.Remember(store, "clef", time.Minute, func() (string, error) {
				builds.Add(1)
				time.Sleep(5 * time.Millisecond)
				return "valeur", nil
			})
		}()
	}

	close(start)
	wg.Wait()

	if n := builds.Load(); n != 1 {
		t.Fatalf("Remember a construit %d fois au lieu d'une (stampede)", n)
	}
}

type inner struct {
	Name string `json:"name" validate:"required"`
}

type outer struct {
	User    inner `json:"user"`
	Company inner `json:"company"`
}

func TestValidationKeepsNestedFieldsApart(t *testing.T) {
	err := validation.Struct(&outer{})

	var failed *validation.Error
	if !errors.As(err, &failed) {
		t.Fatalf("attendu *validation.Error, obtenu %v", err)
	}
	if len(failed.Fields) != 2 {
		t.Fatalf("deux champs imbriques homonymes fusionnes en %d entree(s): %v", len(failed.Fields), failed.Fields)
	}
}
