package unit

import (
	"os"
	"path/filepath"
	"testing"

	"omega/internal/kernel"
)

func TestExpandEnv(t *testing.T) {
	t.Setenv("OMEGA_SET", "value")
	t.Setenv("OMEGA_EMPTY", "")

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"substitutes a set variable", "a: ${OMEGA_SET}", "a: value"},
		{"falls back when unset", "a: ${OMEGA_MISSING:-fallback}", "a: fallback"},
		{"falls back when empty", "a: ${OMEGA_EMPTY:-fallback}", "a: fallback"},
		{"unset without fallback yields empty", "a: ${OMEGA_MISSING}", "a: "},
		{"a set variable beats the fallback", "a: ${OMEGA_SET:-fallback}", "a: value"},
		{"leaves other text alone", "a: plain $notavar", "a: plain $notavar"},
		{"handles several on one line", "${OMEGA_SET}/${OMEGA_MISSING:-x}", "value/x"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := string(kernel.ExpandEnv([]byte(tc.in))); got != tc.want {
				t.Errorf("ExpandEnv(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestLoadConfigNamespacesByFile(t *testing.T) {
	t.Setenv("TEST_PORT", "8080")
	t.Setenv("APP_ENV", "local")

	dir := t.TempDir()
	write(t, filepath.Join(dir, "app.yaml"), "name: Demo\nport: ${TEST_PORT:-3000}\nenv: local\n")
	write(t, filepath.Join(dir, "database.yaml"), "default: sqlite\nconnections:\n  sqlite:\n    driver: sqlite\n")

	cfg, err := kernel.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}

	if got := cfg.String("app.name"); got != "Demo" {
		t.Errorf("app.name = %q, want Demo", got)
	}
	if got := cfg.Int("app.port"); got != 8080 {
		t.Errorf("app.port = %d, want 8080 (the env var should win)", got)
	}
	if got := cfg.String("database.connections.sqlite.driver"); got != "sqlite" {
		t.Errorf("database.connections.sqlite.driver = %q, want sqlite", got)
	}
	if !cfg.IsLocal() {
		t.Error("env local should report IsLocal")
	}
	if got := cfg.StringOr("app.missing", "fallback"); got != "fallback" {
		t.Errorf("StringOr = %q, want fallback", got)
	}
}

func TestLoadConfigUsesYamlDefaults(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "app.yaml"), "port: ${UNSET_OMEGA_PORT:-3000}\n")

	cfg, err := kernel.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Int("app.port"); got != 3000 {
		t.Errorf("app.port = %d, want the 3000 fallback", got)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
