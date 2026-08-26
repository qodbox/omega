package mcp

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func exchange(t *testing.T, tools []Tool, lines ...string) []response {
	t.Helper()

	var out bytes.Buffer
	if err := New("test", tools).Serve(strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}

	answers := []response{}
	for _, raw := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if raw == "" {
			continue
		}
		var answer response
		if err := json.Unmarshal([]byte(raw), &answer); err != nil {
			t.Fatalf("reponse illisible %q: %v", raw, err)
		}
		answers = append(answers, answer)
	}
	return answers
}

func TestServeAnswersInitializeAndToolsList(t *testing.T) {
	tools := []Tool{{Name: "outil", Description: "un outil", Run: func(map[string]any) (string, error) { return "ok", nil }}}

	answers := exchange(t, tools,
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping"}`,
	)

	if len(answers) != 3 {
		t.Fatalf("%d reponse(s), want 3", len(answers))
	}
	for i, answer := range answers {
		if answer.Error != nil {
			t.Errorf("reponse %d en erreur: %+v", i, answer.Error)
		}
		if answer.JSONRPC != "2.0" {
			t.Errorf("reponse %d: jsonrpc = %q", i, answer.JSONRPC)
		}
	}
}

func TestServeIgnoresNotificationsAndGarbage(t *testing.T) {
	answers := exchange(t, nil,
		`pas du json`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":9,"method":"ping"}`,
	)

	if len(answers) != 1 {
		t.Fatalf("%d reponse(s), want 1 — une notification ou une ligne invalide a ete repondue", len(answers))
	}
}

func TestServeRejectsAnUnknownMethodAndTool(t *testing.T) {
	answers := exchange(t, nil,
		`{"jsonrpc":"2.0","id":1,"method":"inconnue"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"absent"}}`,
	)

	for i, answer := range answers {
		if answer.Error == nil {
			t.Errorf("reponse %d: attendu une erreur, obtenu %+v", i, answer.Result)
		}
	}
}

func TestToolFailureIsReportedAsContentNotTransportError(t *testing.T) {
	tools := []Tool{{Name: "casse", Run: func(map[string]any) (string, error) { return "", errors.New("boum") }}}

	answers := exchange(t, tools, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"casse"}}`)

	if len(answers) != 1 || answers[0].Error != nil {
		t.Fatalf("l'echec d'un outil doit rester une reponse valide: %+v", answers)
	}
	encoded, _ := json.Marshal(answers[0].Result)
	if !strings.Contains(string(encoded), "isError") || !strings.Contains(string(encoded), "boum") {
		t.Fatalf("l'erreur de l'outil n'est pas remontee: %s", encoded)
	}
}

func TestReadStaysInsideTheProject(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dedans.txt"), []byte("visible"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := Read(root, "dedans.txt"); err != nil || got != "visible" {
		t.Fatalf("lecture legitime: %q, %v", got, err)
	}

	for _, name := range []string{"../secret.txt", "../../etc/passwd", "/etc/passwd"} {
		if _, err := Read(root, name); err == nil {
			t.Errorf("%q a ete lu hors du projet", name)
		}
	}
}

func TestReadDoesNotFollowASymlinkOutOfTheProject(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "projet")
	outside := filepath.Join(base, "dehors")

	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("interdit"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "lien")); err != nil {
		t.Skipf("liens symboliques indisponibles: %v", err)
	}

	if content, err := Read(root, "lien/secret.txt"); err == nil {
		t.Fatalf("lecture hors projet via un lien symbolique: %q", content)
	}
}

func TestOverviewAndSearchProduceContent(t *testing.T) {
	if Overview("1.2.3") == "" {
		t.Error("Overview est vide")
	}
	if !strings.Contains(Overview("1.2.3"), "1.2.3") {
		t.Error("Overview ne mentionne pas la version")
	}

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n// aiguille\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	found, err := Search(root, "aiguille", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(found, "a.go") {
		t.Errorf("le fichier correspondant n'est pas dans le resultat: %s", found)
	}
}
