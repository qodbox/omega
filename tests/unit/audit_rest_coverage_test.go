package unit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"omega/internal/database"
	"omega/internal/mcp"
	"omega/internal/storage"
)

func TestStoragePutStreamsAReader(t *testing.T) {
	disk, err := storage.New(t.TempDir(), "/files")
	if err != nil {
		t.Fatal(err)
	}

	written, err := disk.Put("dossier/fichier.txt", strings.NewReader("contenu diffuse"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if written != int64(len("contenu diffuse")) {
		t.Errorf("octets ecrits = %d", written)
	}

	back, err := disk.Get("dossier/fichier.txt")
	if err != nil || string(back) != "contenu diffuse" {
		t.Fatalf("relecture: %q, %v", back, err)
	}

	if _, err := disk.Put("../../evade.txt", strings.NewReader("x")); err != nil {
		t.Errorf("un nom traversant doit etre normalise, pas rejete: %v", err)
	}
}

func TestMigrationsReportWhatIsPending(t *testing.T) {
	db := noteDB(t)

	pending, err := database.Pending(db)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if pending != len(database.Migrations()) {
		t.Errorf("Pending = %d, want %d", pending, len(database.Migrations()))
	}
}

func TestMcpModelsDescribesAPackage(t *testing.T) {
	dir := t.TempDir()
	source := `package models

import "time"

type Facture struct {
	ID     uint
	Total  float64
	Emise  time.Time
}

func (f *Facture) Payee() bool { return false }
func (f Facture) TableName() string { return "factures" }
`
	if err := os.WriteFile(filepath.Join(dir, "facture.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	described, err := mcp.Models(dir)
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	for _, part := range []string{"Facture", "Total", "Payee", "TableName"} {
		if !strings.Contains(described, part) {
			t.Errorf("la description omet %q:\n%s", part, described)
		}
	}
}

func TestMcpModelsReportsAMissingDirectory(t *testing.T) {
	if _, err := mcp.Models(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Error("un dossier inexistant a ete accepte")
	}
}

func TestMcpSchemaBuildsAToolContract(t *testing.T) {
	schema := mcp.Schema(map[string]string{"nom": "le nom", "limite": "le maximum"}, "nom")

	if schema["type"] != "object" {
		t.Errorf("type = %v", schema["type"])
	}
	properties, _ := schema["properties"].(map[string]any)
	if len(properties) != 2 {
		t.Errorf("properties = %v", properties)
	}
	required, _ := schema["required"].([]string)
	if len(required) != 1 || required[0] != "nom" {
		t.Errorf("required = %v", required)
	}
}

func TestMcpModelsRendersEveryTypeShape(t *testing.T) {
	dir := t.TempDir()
	source := `package models

import "time"

type Commande struct {
	ID        uint
	Reference *string
	Lignes    []Ligne
	Etiquettes map[string]int
	Creee     time.Time
	Pointeurs []*Ligne
	Etrange   chan int
}

type Ligne struct {
	Libelle string
}

func (c *Commande) Total() float64 { return 0 }
`
	if err := os.WriteFile(filepath.Join(dir, "commande.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	described, err := mcp.Models(dir)
	if err != nil {
		t.Fatalf("Models: %v", err)
	}

	for _, part := range []string{"*string", "[]Ligne", "map[string]int", "time.Time", "[]*Ligne", "?"} {
		if !strings.Contains(described, part) {
			t.Errorf("la description omet %q:\n%s", part, described)
		}
	}
}

func TestMcpSearchHonoursItsLimitAndMisses(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("package p\n// aiguille\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	limited, err := mcp.Search(dir, "aiguille", 1)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if strings.Count(limited, ".go") > 2 {
		t.Errorf("la limite n'est pas respectee:\n%s", limited)
	}

	empty, err := mcp.Search(dir, "introuvable-nulle-part", 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if strings.Contains(empty, ".go:") {
		t.Errorf("un terme absent renvoie des correspondances:\n%s", empty)
	}
}

func TestMcpReadRejectsADirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sous"), 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := mcp.Read(dir, "sous"); err == nil {
		t.Error("lire un dossier a reussi")
	}
	if _, err := mcp.Read(dir, "absent.txt"); err == nil {
		t.Error("lire un fichier absent a reussi")
	}
}
