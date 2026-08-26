package database

import (
	"strings"
	"testing"
)

func TestDefaultBuildCarriesEveryDriver(t *testing.T) {
	for _, alias := range []string{
		"sqlite", "sqlite3",
		"postgres", "postgresql", "pgsql",
		"mysql", "mariadb",
	} {
		if _, ok := dialectors[alias]; !ok {
			t.Errorf("%q absent du build par defaut", alias)
		}
	}
}

func TestAvailableDriversCollapsesAliases(t *testing.T) {
	names := availableDrivers()

	if len(names) != 3 {
		t.Fatalf("%d driver(s) annonces: %v", len(names), names)
	}
	// Sorted, so the error message stays stable.
	if strings.Join(names, ",") != "mysql,postgres,sqlite" {
		t.Errorf("liste = %v", names)
	}
}

func TestAnUnknownDriverNamesWhatIsAvailable(t *testing.T) {
	_, err := dialector(Connection{Driver: "oracle"})
	if err == nil {
		t.Fatal("un driver inconnu a ete accepte")
	}

	for _, expected := range availableDrivers() {
		if !strings.Contains(err.Error(), expected) {
			t.Errorf("l'erreur ne nomme pas %q: %v", expected, err)
		}
	}
}
