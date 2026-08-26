package kernel

import (
	"strings"
	"testing"
	"time"
)

func TestStatusColourFollowsTheStatusClass(t *testing.T) {
	seen := map[string]bool{}
	for _, status := range []int{200, 301, 404, 500} {
		colour := statusColour(status)
		if colour == "" {
			t.Errorf("status %d: pas de couleur", status)
		}
		seen[colour] = true
	}
	if len(seen) < 3 {
		t.Errorf("les classes de statut partagent trop de couleurs: %v", seen)
	}
}

func TestHumanDurationScalesWithMagnitude(t *testing.T) {
	cases := []time.Duration{
		420 * time.Nanosecond,
		1500 * time.Microsecond,
		2 * time.Second,
		3 * time.Minute,
	}

	seen := map[string]bool{}
	for _, elapsed := range cases {
		rendered := humanDuration(elapsed)
		if rendered == "" {
			t.Fatalf("%s rendu vide", elapsed)
		}
		seen[rendered] = true
	}
	if len(seen) != len(cases) {
		t.Errorf("des durees distinctes rendent la meme chaine: %v", seen)
	}
}

func TestRequestLineCarriesMethodPathAndStatus(t *testing.T) {
	line := requestLine("PATCH", "/api/users/7", 422, 12*time.Millisecond)

	for _, part := range []string{"PATCH", "/api/users/7", "422"} {
		if !strings.Contains(line, part) {
			t.Errorf("la ligne ne contient pas %q: %s", part, line)
		}
	}
}

func TestPaintWrapsAndCanBeEmpty(t *testing.T) {
	if got := paint("", "texte"); !strings.Contains(got, "texte") {
		t.Errorf("paint sans couleur perd le texte: %q", got)
	}
	if got := paint(statusColour(200), "ok"); !strings.Contains(got, "ok") {
		t.Errorf("paint perd le texte: %q", got)
	}
}

func TestBannerMentionsItsInputs(t *testing.T) {
	rendered := banner("Omega", "1.0.0", "local", "sqlite", "0.0.0.0:3000")

	for _, part := range []string{"Omega", "1.0.0", "local", "sqlite", "127.0.0.1:3000"} {
		if !strings.Contains(rendered, part) {
			t.Errorf("le banner ne contient pas %q:\n%s", part, rendered)
		}
	}
}

func TestConsoleLevelMapsEveryLevel(t *testing.T) {
	if got := consoleLevel("info"); got != " " {
		t.Errorf("info = %q, want un espace", got)
	}
	if got := consoleLevel("warn"); !strings.Contains(got, "warn") {
		t.Errorf("warn = %q", got)
	}
	if got := consoleLevel("error"); !strings.Contains(got, "error") {
		t.Errorf("error = %q", got)
	}
	if got := consoleLevel("inconnu"); got != "inconnu" {
		t.Errorf("un niveau inconnu doit passer tel quel: %q", got)
	}
}

func TestWriteConsoleDoesNotPanic(t *testing.T) {
	writeConsole("")
}
