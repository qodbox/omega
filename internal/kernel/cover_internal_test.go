package kernel

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog"

	"omega/internal/validation"
)

func TestPaintObeysTheColourSwitch(t *testing.T) {
	previous := colourise
	t.Cleanup(func() { colourise = previous })

	colourise = false
	if got := paint(ansiRed, "erreur"); got != "erreur" {
		t.Errorf("sans couleur, paint = %q", got)
	}

	colourise = true
	got := paint(ansiRed, "erreur")
	if !strings.HasPrefix(got, ansiRed) || !strings.HasSuffix(got, ansiReset) {
		t.Errorf("avec couleur, paint = %q", got)
	}
	if !strings.Contains(got, "erreur") {
		t.Errorf("le texte a disparu: %q", got)
	}
}

func TestStatusColourFollowsTheClass(t *testing.T) {
	cases := map[int]string{
		200: ansiGreen,
		204: ansiGreen,
		301: ansiCyan,
		404: ansiYellow,
		422: ansiYellow,
		500: ansiRed,
		503: ansiRed,
	}

	for status, want := range cases {
		if got := statusColour(status); got != want {
			t.Errorf("statusColour(%d) = %q, attendu %q", status, got, want)
		}
	}
}

func TestHumanDurationPicksTheRightUnit(t *testing.T) {
	cases := []struct {
		elapsed time.Duration
		want    string
	}{
		{400 * time.Nanosecond, "400ns"},
		{2 * time.Microsecond, "2µs"},
		{1500 * time.Microsecond, "1.5ms"},
		{2500 * time.Millisecond, "2.50s"},
	}

	for _, item := range cases {
		if got := humanDuration(item.elapsed); got != item.want {
			t.Errorf("humanDuration(%v) = %q, attendu %q", item.elapsed, got, item.want)
		}
	}
}

func TestRequestLineTruncatesALongPath(t *testing.T) {
	previous := colourise
	t.Cleanup(func() { colourise = previous })
	colourise = false

	long := "/api/" + strings.Repeat("a", 200)
	line := requestLine("GET", long, 200, time.Millisecond)

	if strings.Contains(line, strings.Repeat("a", 200)) {
		t.Error("un chemin de 200 caracteres doit etre tronque")
	}
	if !strings.Contains(line, "…") {
		t.Errorf("la troncature doit se voir: %q", line)
	}
	if !strings.Contains(line, "200") || !strings.Contains(line, "GET") {
		t.Errorf("statut ou methode absent: %q", line)
	}
}

func TestRequestLineKeepsAShortPathWhole(t *testing.T) {
	previous := colourise
	t.Cleanup(func() { colourise = previous })
	colourise = false

	line := requestLine("DELETE", "/api/users/1", 204, 3*time.Second)

	if !strings.Contains(line, "/api/users/1") {
		t.Errorf("chemin absent: %q", line)
	}
	if strings.Contains(line, "…") {
		t.Errorf("chemin court tronque a tort: %q", line)
	}
	if !strings.Contains(line, "3.00s") {
		t.Errorf("duree absente: %q", line)
	}
}

func TestParseLevelFallsBackToInfo(t *testing.T) {
	cases := map[string]zerolog.Level{
		"debug":  zerolog.DebugLevel,
		"DEBUG":  zerolog.DebugLevel,
		"warn":   zerolog.WarnLevel,
		"error":  zerolog.ErrorLevel,
		"":       zerolog.InfoLevel,
		"bavard": zerolog.InfoLevel,
		"trace":  zerolog.TraceLevel,
	}

	for name, want := range cases {
		if got := parseLevel(name); got != want {
			t.Errorf("parseLevel(%q) = %v, attendu %v", name, got, want)
		}
	}
}

func TestRequestIDIsEmptyWhenNothingSetIt(t *testing.T) {
	app := fiber.New()

	var seen string
	app.Get("/", func(c *fiber.Ctx) error {
		seen = requestID(c)
		return c.SendStatus(fiber.StatusOK)
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/", nil), -1); err != nil {
		t.Fatal(err)
	}
	if seen != "" {
		t.Errorf("sans identifiant pose, requestID = %q", seen)
	}
}

func TestRequestIDReadsWhatTheMiddlewareStored(t *testing.T) {
	app := fiber.New()

	var seen string
	app.Get("/", func(c *fiber.Ctx) error {
		c.Locals(RequestIDKey, "abc-123")
		seen = requestID(c)
		return c.SendStatus(fiber.StatusOK)
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/", nil), -1); err != nil {
		t.Fatal(err)
	}
	if seen != "abc-123" {
		t.Errorf("requestID = %q", seen)
	}
}

func TestRateKeyFallsBackToTheAddress(t *testing.T) {
	app := fiber.New()

	var seen string
	app.Get("/", func(c *fiber.Ctx) error {
		seen = rateKey(c)
		return c.SendStatus(fiber.StatusOK)
	})

	if _, err := app.Test(httptest.NewRequest("GET", "/", nil), -1); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(seen, "ip:") {
		t.Errorf("sans jeton, rateKey = %q, attendu un prefixe ip:", seen)
	}
}

func TestRateKeySeparatesTwoTokens(t *testing.T) {
	app := fiber.New()

	var seen string
	app.Get("/", func(c *fiber.Ctx) error {
		seen = rateKey(c)
		return c.SendStatus(fiber.StatusOK)
	})

	first := httptest.NewRequest("GET", "/", nil)
	first.Header.Set("Authorization", "Bearer entete.charge.signature-un-0123456789")
	if _, err := app.Test(first, -1); err != nil {
		t.Fatal(err)
	}
	one := seen

	second := httptest.NewRequest("GET", "/", nil)
	second.Header.Set("Authorization", "Bearer entete.charge.signature-deux-9876543210")
	if _, err := app.Test(second, -1); err != nil {
		t.Fatal(err)
	}

	if !strings.HasPrefix(one, "token:") {
		t.Errorf("avec jeton, rateKey = %q, attendu un prefixe token:", one)
	}
	if one == seen {
		t.Error("deux jetons differents doivent donner deux cles differentes")
	}
}

func covConfig(t *testing.T, body string) *Config {
	t.Helper()

	covIsolateEnv(t)

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	return cfg
}

func covIsolateEnv(t *testing.T) {
	t.Helper()

	covChdir(t, t.TempDir())

	for _, name := range []string{"APP_PORT", "APP_HOST", "APP_PORT_ATTEMPTS"} {
		restore, had := os.LookupEnv(name)
		if err := os.Unsetenv(name); err != nil {
			t.Fatal(err)
		}
		if had {
			t.Cleanup(func() { _ = os.Setenv(name, restore) })
		}
	}
}

func TestAddrComputesFromConfigBeforeListening(t *testing.T) {
	app := &App{Cfg: covConfig(t, "host: 127.0.0.1\nport: 4321\n")}

	if got := app.Addr(); got != "127.0.0.1:4321" {
		t.Errorf("Addr = %q", got)
	}
}

func TestAddrPrefersTheBoundAddress(t *testing.T) {
	app := &App{Cfg: covConfig(t, "host: 127.0.0.1\nport: 4321\n"), addr: "127.0.0.1:9999"}

	if got := app.Addr(); got != "127.0.0.1:9999" {
		t.Errorf("une fois lie, Addr doit rendre l'adresse reelle, obtenu %q", got)
	}
}

func TestDriverNameSaysWhenThereIsNoDatabase(t *testing.T) {
	app := &App{Cfg: covConfig(t, "port: 3000\n")}

	if got := app.driverName(); got != "no database" {
		t.Errorf("driverName = %q", got)
	}
}

func TestListenMovesToTheNextFreePort(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()

	busy := taken.Addr().(*net.TCPAddr).Port
	app := &App{
		Cfg: covConfig(t, fmt.Sprintf("host: 127.0.0.1\nport: %d\nport_attempts: 5\n", busy)),
		Log: zerolog.Nop(),
	}

	listener, err := app.listen()
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = listener.Close() }()

	if got := listener.Addr().(*net.TCPAddr).Port; got == busy {
		t.Errorf("le port %d etait pris, listen l'a repris quand meme", busy)
	}
}

func TestListenGivesUpWhenEveryPortIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = taken.Close() }()

	busy := taken.Addr().(*net.TCPAddr).Port
	app := &App{
		Cfg: covConfig(t, fmt.Sprintf("host: 127.0.0.1\nport: %d\nport_attempts: 1\n", busy)),
		Log: zerolog.Nop(),
	}

	if _, err := app.listen(); err == nil {
		t.Fatal("un seul essai sur un port pris doit echouer")
	} else if !strings.Contains(err.Error(), "taken") {
		t.Errorf("message inattendu: %v", err)
	}
}

func TestListenTreatsZeroAttemptsAsOne(t *testing.T) {
	app := &App{
		Cfg: covConfig(t, "host: 127.0.0.1\nport: 0\nport_attempts: 0\n"),
		Log: zerolog.Nop(),
	}

	listener, err := app.listen()
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	_ = listener.Close()
}

func TestListenReportsAnAddressItCannotUse(t *testing.T) {
	app := &App{
		Cfg: covConfig(t, "host: 203.0.113.255\nport: 4321\nport_attempts: 3\n"),
		Log: zerolog.Nop(),
	}

	if _, err := app.listen(); err == nil {
		t.Fatal("une adresse non locale doit echouer")
	}
}

func TestRunRefusesAnAppWithoutHttp(t *testing.T) {
	app := &App{Cfg: covConfig(t, "port: 0\n"), Log: zerolog.Nop()}

	err := app.Run()
	if err == nil {
		t.Fatal("Run sans Fiber doit echouer")
	}
	if !strings.Contains(err.Error(), "without HTTP") {
		t.Errorf("message inattendu: %v", err)
	}
}

func TestFindRootRecognisesEitherMarker(t *testing.T) {
	for _, marker := range markers {
		dir := t.TempDir()
		full := filepath.Join(dir, marker)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}

		deep := filepath.Join(dir, "app", "controllers", "user")
		if err := os.MkdirAll(deep, 0o755); err != nil {
			t.Fatal(err)
		}

		root, found := FindRoot(deep)
		if !found {
			t.Errorf("%s: racine non trouvee depuis un sous-dossier", marker)
			continue
		}
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		if expected, err := filepath.EvalSymlinks(dir); err == nil {
			dir = expected
		}
		if root != dir {
			t.Errorf("%s: racine = %q, attendu %q", marker, root, dir)
		}
	}
}

func TestFindRootStopsAtTheFilesystemTop(t *testing.T) {
	if _, found := FindRoot(t.TempDir()); found {
		t.Error("un dossier vide ne doit pas passer pour une racine")
	}
}

func TestEnterRootStaysPutAtTheRoot(t *testing.T) {
	dir := covProject(t)
	covChdir(t, dir)

	root, moved, err := EnterRoot()
	if err != nil {
		t.Fatalf("EnterRoot: %v", err)
	}
	if moved {
		t.Error("deja a la racine, EnterRoot ne doit pas se deplacer")
	}
	if !covSamePath(t, root, dir) {
		t.Errorf("racine = %q, attendu %q", root, dir)
	}
}

func TestEnterRootClimbsFromASubdirectory(t *testing.T) {
	dir := covProject(t)
	deep := filepath.Join(dir, "app", "models")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	covChdir(t, deep)

	root, moved, err := EnterRoot()
	if err != nil {
		t.Fatalf("EnterRoot: %v", err)
	}
	if !moved {
		t.Error("EnterRoot devait remonter")
	}
	if !covSamePath(t, root, dir) {
		t.Errorf("racine = %q, attendu %q", root, dir)
	}

	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if !covSamePath(t, working, dir) {
		t.Errorf("EnterRoot doit avoir change de dossier, on est dans %q", working)
	}
}

func TestEnterRootRefusesOutsideAProject(t *testing.T) {
	covChdir(t, t.TempDir())

	_, moved, err := EnterRoot()
	if !errors.Is(err, ErrNoProject) {
		t.Fatalf("erreur = %v, attendu ErrNoProject", err)
	}
	if moved {
		t.Error("aucun deplacement ne doit avoir lieu")
	}
}

func covProject(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	full := filepath.Join(dir, "config", "app.yaml")
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("port: 3000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func covChdir(t *testing.T, dir string) {
	t.Helper()

	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })

	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
}

func covSamePath(t *testing.T, left, right string) bool {
	t.Helper()

	resolve := func(path string) string {
		if out, err := filepath.EvalSymlinks(path); err == nil {
			return out
		}
		return path
	}
	return resolve(left) == resolve(right)
}

func TestErrorHandlerShapesEachFailure(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{"validation", &validation.Error{Fields: map[string]string{"email": "obligatoire"}}, fiber.StatusUnprocessableEntity, "Validation failed."},
		{"fiber", fiber.NewError(fiber.StatusNotFound, "Introuvable."), fiber.StatusNotFound, "Introuvable."},
		{"nu", errors.New("fuite interne"), fiber.StatusInternalServerError, "An internal error occurred."},
	}

	for _, item := range cases {
		app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(zerolog.Nop(), false, false)})
		app.Get("/", func(c *fiber.Ctx) error { return item.err })

		response, err := app.Test(httptest.NewRequest("GET", "/", nil), -1)
		if err != nil {
			t.Fatalf("%s: %v", item.name, err)
		}
		body, err := io.ReadAll(response.Body)
		_ = response.Body.Close()
		if err != nil {
			t.Fatal(err)
		}

		if response.StatusCode != item.status {
			t.Errorf("%s: statut = %d, attendu %d", item.name, response.StatusCode, item.status)
		}
		if !strings.Contains(string(body), item.body) {
			t.Errorf("%s: corps = %s", item.name, body)
		}
	}
}

func TestErrorHandlerHidesInternalsUnlessDebugging(t *testing.T) {
	secret := "connexion refusee vers 10.0.0.1"

	quiet := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(zerolog.Nop(), false, false)})
	quiet.Get("/", func(c *fiber.Ctx) error { return errors.New(secret) })

	response, err := quiet.Test(httptest.NewRequest("GET", "/", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(response.Body)
	_ = response.Body.Close()

	if strings.Contains(string(body), secret) {
		t.Errorf("le detail interne a fuite: %s", body)
	}

	loud := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(zerolog.Nop(), true, false)})
	loud.Get("/", func(c *fiber.Ctx) error { return errors.New(secret) })

	response, err = loud.Test(httptest.NewRequest("GET", "/", nil), -1)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(response.Body)
	_ = response.Body.Close()

	if !strings.Contains(string(body), secret) {
		t.Errorf("en debug, le detail doit apparaitre: %s", body)
	}
}

func TestLoadConfigRefusesBrokenYaml(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "app.yaml"), []byte("port: [non\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := LoadConfig(dir); err == nil {
		t.Fatal("un YAML invalide doit echouer")
	}
}

func TestLoadConfigAcceptsAnEmptyDirectory(t *testing.T) {
	covIsolateEnv(t)

	cfg, err := LoadConfig(t.TempDir())
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := cfg.IntOr("app.port", 3000); got != 3000 {
		t.Errorf("sans fichier, la valeur par defaut doit tenir, obtenu %d", got)
	}
}

func TestWithConfigDirKeepsTheDefaultWhenGivenNothing(t *testing.T) {
	b := bootConfig{configDir: "config"}

	WithConfigDir("")(&b)
	if b.configDir != "config" {
		t.Errorf("un dossier vide a ecrase le defaut: %q", b.configDir)
	}

	WithConfigDir("ailleurs")(&b)
	if b.configDir != "ailleurs" {
		t.Errorf("configDir = %q", b.configDir)
	}
}

func TestWithoutChdirLeavesTheProcessWhereItIs(t *testing.T) {
	dir := covProject(t)
	deep := filepath.Join(dir, "app", "models")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	covChdir(t, deep)

	root, moved, err := enterRootIf(false)
	if err != nil {
		t.Fatalf("enterRootIf: %v", err)
	}
	if moved {
		t.Error("WithoutChdir ne doit pas deplacer le processus")
	}
	if !covSamePath(t, root, deep) {
		t.Errorf("racine annoncee = %q, attendu %q", root, deep)
	}

	working, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if !covSamePath(t, working, deep) {
		t.Errorf("le processus a bouge vers %q", working)
	}
}

func TestWithoutChdirStillRefusesOutsideAProject(t *testing.T) {
	covChdir(t, t.TempDir())

	if _, _, err := enterRootIf(false); !errors.Is(err, ErrNoProject) {
		t.Fatalf("erreur = %v, attendu ErrNoProject", err)
	}
}

func TestBootMovesByDefault(t *testing.T) {
	b := bootConfig{withChdir: true}

	WithoutChdir()(&b)
	if b.withChdir {
		t.Error("WithoutChdir doit desactiver le deplacement")
	}
}
