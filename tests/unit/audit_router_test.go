package unit

import (
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/gofiber/fiber/v2"
	"github.com/valyala/fasthttp"
	"gorm.io/gorm"

	"omega/internal/router"
)

type note struct {
	ID    uint `gorm:"primarykey"`
	Title string
	Slug  string
}

func noteDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	if err := db.AutoMigrate(&note{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	if err := db.Create(&note{Title: "Premiere", Slug: "premiere"}).Error; err != nil {
		t.Fatalf("insertion: %v", err)
	}
	return db
}

func statusOf(t *testing.T, app *fiber.App, path string) int {
	t.Helper()

	response, err := app.Test(httptest.NewRequest("GET", path, nil), 2000)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return response.StatusCode
}

func TestModelBindsAnExistingRow(t *testing.T) {
	db := noteDB(t)
	app := fiber.New()
	app.Get("/notes/:id", router.Model[note](db), func(c *fiber.Ctx) error {
		bound, ok := router.Bound[note](c)
		if !ok {
			return fiber.NewError(fiber.StatusInternalServerError, "rien de lie")
		}
		return c.JSON(bound)
	})

	if status := statusOf(t, app, "/notes/1"); status != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
}

func TestModelAnswers404OnAMissingRow(t *testing.T) {
	db := noteDB(t)
	app := fiber.New()
	app.Get("/notes/:id", router.Model[note](db), func(c *fiber.Ctx) error { return nil })

	if status := statusOf(t, app, "/notes/999"); status != fiber.StatusNotFound {
		t.Fatalf("status = %d, want 404", status)
	}
}

func TestModelBindsOnACustomColumn(t *testing.T) {
	db := noteDB(t)
	app := fiber.New()
	app.Get("/notes/:slug", router.Model[note](db, router.BindOptions{Param: "slug", Column: "slug"}),
		func(c *fiber.Ctx) error {
			bound := router.MustBound[note](c)
			return c.SendString(bound.Title)
		})

	if status := statusOf(t, app, "/notes/premiere"); status != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if status := statusOf(t, app, "/notes/absente"); status != fiber.StatusNotFound {
		t.Fatalf("slug inconnu: status = %d, want 404", status)
	}
}

func TestModelHonoursAScope(t *testing.T) {
	db := noteDB(t)
	app := fiber.New()
	app.Get("/notes/:id", router.Model[note](db, router.BindOptions{
		Scope: func(c *fiber.Ctx, tx *gorm.DB) *gorm.DB { return tx.Where("slug = ?", "impossible") },
	}), func(c *fiber.Ctx) error { return nil })

	if status := statusOf(t, app, "/notes/1"); status != fiber.StatusNotFound {
		t.Fatalf("le scope n'a pas ete applique: status = %d, want 404", status)
	}
}

func TestBoundReportsAbsenceInsteadOfPanicking(t *testing.T) {
	app := fiber.New()
	app.Get("/rien", func(c *fiber.Ctx) error {
		if _, ok := router.Bound[note](c); ok {
			return fiber.NewError(fiber.StatusInternalServerError, "un modele est apparu")
		}
		return c.SendStatus(fiber.StatusNoContent)
	})

	if status := statusOf(t, app, "/rien"); status != fiber.StatusNoContent {
		t.Fatalf("status = %d, want 204", status)
	}
}

func TestMustBoundPanicsWhenNothingIsBound(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("MustBound n'a pas panique sans modele lie")
		}
	}()

	app := fiber.New()
	c := app.AcquireCtx(&fasthttp.RequestCtx{})
	defer app.ReleaseCtx(c)

	_ = router.MustBound[note](c)
}
