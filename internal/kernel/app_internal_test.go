package kernel

import (
	"net"
	"strings"
	"testing"
)

func TestBootOptionsChangeTheConfiguration(t *testing.T) {
	b := bootConfig{withHTTP: true, withDB: true}

	WithConfigDir("/ailleurs")(&b)
	WithoutHTTP()(&b)
	WithoutDatabase()(&b)

	if b.configDir != "/ailleurs" {
		t.Errorf("configDir = %q", b.configDir)
	}
	if b.withHTTP {
		t.Error("WithoutHTTP n'a rien change")
	}
	if b.withDB {
		t.Error("WithoutDatabase n'a rien change")
	}
}

func TestAddressInUseRecognisesABusyPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	_, err = net.Listen("tcp", listener.Addr().String())
	if err == nil {
		t.Skip("le port se laisse reouvrir sur cette plateforme")
	}
	if !addressInUse(err) {
		t.Fatalf("addressInUse ne reconnait pas %v", err)
	}

	if addressInUse(nil) {
		t.Error("addressInUse(nil) est vrai")
	}
	if addressInUse(net.UnknownNetworkError("autre chose")) {
		t.Error("une erreur sans rapport est prise pour un port occupe")
	}
}

func TestDriverNameFallsBackWithoutADatabase(t *testing.T) {
	app := &App{Cfg: &Config{}}

	if got := app.driverName(); !strings.Contains(got, "no database") {
		t.Errorf("driverName = %q", got)
	}
}

func bootForTest(t *testing.T, opts ...Option) *App {
	t.Helper()

	t.Setenv("APP_ENV", "testing")
	t.Setenv("LOG_LEVEL", "error")
	t.Setenv("AUTH_SECRET", "an-internal-secret-long-enough-to-sign")
	t.Setenv("DB_SQLITE_PATH", t.TempDir()+"/test.db")
	t.Setenv("APP_PORT", "0")

	app, err := Boot(append([]Option{WithConfigDir("../../config"), WithoutDatabase()}, opts...)...)
	if err != nil {
		t.Skipf("boot indisponible depuis le paquet: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown() })
	return app
}

func TestListenPicksAFreePortAndFallsBack(t *testing.T) {
	app := bootForTest(t)

	first, err := app.listen()
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer first.Close()

	if app.Addr() == "" {
		t.Error("Addr() est vide")
	}
	if app.wantedPort() != 0 {
		t.Errorf("wantedPort = %d, want 0", app.wantedPort())
	}

	second, err := app.listen()
	if err != nil {
		t.Fatalf("second listen: %v", err)
	}
	defer second.Close()

	if first.Addr().String() == second.Addr().String() {
		t.Error("deux ecoutes ont recu la meme adresse")
	}
}

func TestRunRefusesAnAppWithoutHTTP(t *testing.T) {
	app := bootForTest(t, WithoutHTTP())

	if err := app.Run(); err == nil {
		t.Fatal("Run a accepte une application sans HTTP")
	}
	if app.driverName() == "" {
		t.Error("driverName est vide")
	}
	if app.Container() == nil {
		t.Error("Container() est nil")
	}
}
