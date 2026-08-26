package unit

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/graphql-go/graphql"

	"omega/internal/api"
)

func notesSchema(t *testing.T) graphql.Schema {
	t.Helper()

	db := noteDB(t)
	registry := api.NewRegistry(db)
	if err := registry.Discover(db); err != nil {
		t.Fatalf("discover: %v", err)
	}
	registry.AllowWrites("notes")

	schema, err := registry.Schema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return schema
}

func run(t *testing.T, schema graphql.Schema, query string) *graphql.Result {
	t.Helper()

	return graphql.Do(graphql.Params{
		Schema:        schema,
		RequestString: query,
		Context:       context.Background(),
	})
}

func TestGraphQLCreatesAndDeletesARow(t *testing.T) {
	schema := notesSchema(t)

	created := run(t, schema, `mutation { create_note(title: "Depuis GraphQL", slug: "gql") { id title } }`)
	if len(created.Errors) > 0 {
		t.Fatalf("create_note: %v", created.Errors)
	}

	data, _ := created.Data.(map[string]any)
	note, _ := data["create_note"].(map[string]any)
	if note == nil || note["title"] != "Depuis GraphQL" {
		t.Fatalf("la ressource creee est vide: %v", data)
	}

	id := fmt.Sprint(note["id"])
	if id == "" || id == "<nil>" {
		t.Fatalf("pas d'id renvoye: %v", note)
	}

	removed := run(t, schema, `mutation { delete_note(id: "`+id+`") }`)
	if len(removed.Errors) > 0 {
		t.Fatalf("delete_note: %v", removed.Errors)
	}
	if deleted, _ := removed.Data.(map[string]any); deleted["delete_note"] != true {
		t.Fatalf("delete_note = %v, want true", deleted["delete_note"])
	}

	after := run(t, schema, `{ notes_count }`)
	if counted, _ := after.Data.(map[string]any); counted["notes_count"] != 1 {
		t.Fatalf("notes_count = %v, want 1 (la ligne de depart)", counted["notes_count"])
	}
}

func TestGraphQLDeletingAMissingRowReportsFalse(t *testing.T) {
	schema := notesSchema(t)

	removed := run(t, schema, `mutation { delete_note(id: "99999") }`)
	if len(removed.Errors) > 0 {
		t.Fatalf("delete_note: %v", removed.Errors)
	}
	if data, _ := removed.Data.(map[string]any); data["delete_note"] != false {
		t.Fatalf("delete_note = %v, want false", data["delete_note"])
	}
}

func TestGraphQLReadsOneAndPaginates(t *testing.T) {
	schema := notesSchema(t)

	one := run(t, schema, `{ note(id: "1") { id title } }`)
	if len(one.Errors) > 0 {
		t.Fatalf("note: %v", one.Errors)
	}

	listed := run(t, schema, `{ notes(limit: 1, offset: 0, sort: "-id") { id } }`)
	if len(listed.Errors) > 0 {
		t.Fatalf("notes: %v", listed.Errors)
	}
}

type mixedRow struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	Actif     bool      `json:"actif"`
	Quantite  int       `json:"quantite"`
	Prix      float64   `json:"prix"`
	Libelle   string    `json:"libelle"`
	CreatedAt time.Time `json:"created_at"`
}

func TestGraphQLRendersEveryScalarShape(t *testing.T) {
	db := noteDB(t)
	if err := db.AutoMigrate(&mixedRow{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&mixedRow{Actif: true, Quantite: 7, Prix: 12.5, Libelle: "essai"}).Error; err != nil {
		t.Fatal(err)
	}

	registry := api.NewRegistry(db)
	if err := registry.Discover(db); err != nil {
		t.Fatalf("discover: %v", err)
	}
	schema, err := registry.Schema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}

	result := run(t, schema, `{ mixed_rows { id actif quantite prix libelle created_at } }`)
	if len(result.Errors) > 0 {
		t.Fatalf("requete: %v", result.Errors)
	}

	data, _ := result.Data.(map[string]any)
	rows, _ := data["mixed_rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("%d ligne(s)", len(rows))
	}

	row, _ := rows[0].(map[string]any)
	for _, field := range []string{"id", "actif", "quantite", "prix", "libelle", "created_at"} {
		if row[field] == nil {
			t.Errorf("%s est nil: %v", field, row)
		}
	}
}

func TestGraphQLSingularNamesAreCapitalised(t *testing.T) {
	schema := notesSchema(t)

	introspection := run(t, schema, `{ __type(name: "Note") { name } }`)
	if len(introspection.Errors) > 0 {
		t.Fatalf("introspection: %v", introspection.Errors)
	}

	data, _ := introspection.Data.(map[string]any)
	typed, _ := data["__type"].(map[string]any)
	if typed == nil || typed["name"] != "Note" {
		t.Fatalf("le type singulier n'est pas capitalise: %v", data)
	}
}

func TestSchemaHoldsWithoutAnyWritableResource(t *testing.T) {
	db := noteDB(t)
	registry := api.NewRegistry(db)
	if err := registry.Discover(db); err != nil {
		t.Fatalf("discover: %v", err)
	}

	schema, err := registry.Schema()
	if err != nil {
		t.Fatalf("un schema sans mutation doit se construire: %v", err)
	}

	listed := run(t, schema, `{ notes { id } }`)
	if len(listed.Errors) > 0 {
		t.Fatalf("lecture: %v", listed.Errors)
	}

	refused := run(t, schema, `mutation { create_note(title: "x") { id } }`)
	if len(refused.Errors) == 0 {
		t.Fatal("une mutation a ete acceptee alors qu'aucune n'est publiee")
	}
}
