package database

import (
	"strings"
	"testing"
)

func TestDialectorAcceptsEverySupportedDriver(t *testing.T) {
	cases := map[string]string{
		"sqlite":     "sqlite",
		"sqlite3":    "sqlite",
		"SQLite":     "sqlite",
		"postgres":   "postgres",
		"postgresql": "postgres",
		"pgsql":      "postgres",
		"POSTGRES":   "postgres",
		"mysql":      "mysql",
		"mariadb":    "mysql",
		"MariaDB":    "mysql",
	}

	for driver, want := range cases {
		got, err := dialector(Connection{Driver: driver, Database: "omega", Host: "127.0.0.1", Port: 5432})
		if err != nil {
			t.Errorf("%s: %v", driver, err)
			continue
		}
		if got.Name() != want {
			t.Errorf("%s: dialecte = %q, want %q", driver, got.Name(), want)
		}
	}
}

func TestDialectorRefusesAnUnsupportedDriver(t *testing.T) {
	for _, driver := range []string{"sqlserver", "mssql", "oracle", "cockroach", ""} {
		_, err := dialector(Connection{Driver: driver})
		if err == nil {
			t.Errorf("%q accepte alors qu'il n'est pas supporte", driver)
			continue
		}
		for _, expected := range []string{"sqlite", "postgres", "mysql"} {
			if !strings.Contains(err.Error(), expected) {
				t.Errorf("%q: l'erreur ne nomme pas %q: %v", driver, expected, err)
			}
		}
	}
}
