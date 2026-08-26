package unit

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/rs/zerolog"
	"gorm.io/gorm"

	"omega/internal/database"
)

var dbkCovLog = zerolog.New(io.Discard)

var dbkCovErrSabotage = errors.New("dbkcov: instruction sabotee")

var dbkCovSeederFails bool

type dbkCovNote struct {
	ID    uint `gorm:"primaryKey"`
	Title string
}

func dbkCovOpen(t *testing.T, conn database.Connection) *gorm.DB {
	t.Helper()

	if conn.MaxOpenConns == 0 {
		conn.MaxOpenConns = 1
	}
	db, err := database.Open(conn, dbkCovLog, "silent", 0)
	if err != nil {
		t.Fatalf("Open %s: %v", conn.Driver, err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}

func dbkCovSqlite(t *testing.T) *gorm.DB {
	t.Helper()
	return dbkCovOpen(t, database.Connection{
		Driver: "sqlite",
		DSN:    filepath.Join(t.TempDir(), "cover.db"),
	})
}

func dbkCovBreakExec(t *testing.T, db *gorm.DB, needle string) {
	t.Helper()

	err := db.Callback().Raw().Before("gorm:raw").Register("dbkcov:break", func(tx *gorm.DB) {
		if strings.Contains(tx.Statement.SQL.String(), needle) {
			tx.AddError(dbkCovErrSabotage)
		}
	})
	if err != nil {
		t.Fatalf("enregistrement du callback: %v", err)
	}
}

func TestDbkCovNewManagerFallsBackToSqlite(t *testing.T) {
	manager := database.NewManager(database.Config{}, dbkCovLog)

	if got := manager.DefaultName(); got != "sqlite" {
		t.Fatalf("DefaultName = %q, attendu sqlite", got)
	}
}

func TestDbkCovConnectionUsesTheDefaultWhenUnnamed(t *testing.T) {
	manager := database.NewManager(database.Config{
		Default:  "principale",
		LogLevel: "silent",
		Connections: map[string]database.Connection{
			"principale": {
				Driver:          "sqlite",
				DSN:             filepath.Join(t.TempDir(), "principale.db"),
				MaxOpenConns:    1,
				MaxIdleConns:    1,
				ConnMaxLifetime: time.Minute,
			},
		},
	}, dbkCovLog)

	unnamed, err := manager.Connection("")
	if err != nil {
		t.Fatalf("Connection(\"\"): %v", err)
	}

	named, err := manager.Connection("principale")
	if err != nil {
		t.Fatalf("Connection(principale): %v", err)
	}
	if unnamed != named {
		t.Fatal("la connexion par defaut n'est pas la connexion nommee")
	}

	if err := manager.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatalf("Close a vide: %v", err)
	}
}

func TestDbkCovConnectionReportsAnUnknownDriver(t *testing.T) {
	manager := database.NewManager(database.Config{
		Default:     "pigeon",
		Connections: map[string]database.Connection{"pigeon": {Driver: "pigeon-voyageur"}},
	}, dbkCovLog)

	_, err := manager.Default()
	if err == nil || !strings.Contains(err.Error(), "pigeon-voyageur") {
		t.Fatalf("driver inconnu accepte: %v", err)
	}
	if !strings.Contains(err.Error(), "pigeon") {
		t.Errorf("l'erreur ne nomme pas la connexion: %v", err)
	}

	if _, err := manager.Connection("absente"); err == nil {
		t.Error("une connexion inexistante a ete acceptee")
	}
}

func TestDbkCovOpenRejectsUnreachableTargets(t *testing.T) {
	cases := []struct {
		name string
		conn database.Connection
	}{
		{"sqlite-repertoire-absent", database.Connection{
			Driver: "sqlite", DSN: filepath.Join(t.TempDir(), "absent", "cover.db"),
		}},
		{"postgres-dsn", database.Connection{
			Driver: "postgres",
			DSN:    "host=dbkcov.invalid port=5432 user=u password=p dbname=d sslmode=disable",
		}},
		{"postgres-champs", database.Connection{
			Driver: "postgresql", Host: "dbkcov.invalid", Database: "d", Username: "u",
		}},
		{"mysql-dsn", database.Connection{
			Driver: "mysql", DSN: "u:p@tcp(dbkcov.invalid:3306)/d",
		}},
		{"mysql-champs", database.Connection{
			Driver: "mariadb", Host: "dbkcov.invalid", Database: "d", Username: "u",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, err := database.Open(tc.conn, dbkCovLog, "silent", 0)
			if err == nil {
				if sqlDB, dbErr := db.DB(); dbErr == nil {
					_ = sqlDB.Close()
				}
				t.Fatalf("%s: une cible injoignable a ete acceptee", tc.name)
			}
		})
	}
}

func TestDbkCovSqliteFallsBackToOmegaDb(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	db := dbkCovOpen(t, database.Connection{Driver: "sqlite3"})
	if err := db.Exec("CREATE TABLE dbk_cov_probe (id INTEGER)").Error; err != nil {
		t.Fatalf("creation de table: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "omega.db")); err != nil {
		t.Fatalf("omega.db attendu dans le repertoire courant: %v", err)
	}
}

func TestDbkCovGormLoggerTracesSuccessfulQueriesAtInfoLevel(t *testing.T) {
	db, err := database.Open(database.Connection{
		Driver:       "sqlite",
		DSN:          filepath.Join(t.TempDir(), "trace.db"),
		MaxOpenConns: 1,
	}, dbkCovLog, "info", time.Minute)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, err := db.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	if err := db.Exec("CREATE TABLE dbk_cov_trace (id INTEGER)").Error; err != nil {
		t.Fatalf("creation de table: %v", err)
	}

	var count int64
	if err := db.Table("dbk_cov_trace").Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("count = %d, attendu 0", count)
	}
}

func TestDbkCovFactoryReportsCreateFailures(t *testing.T) {
	db := dbkCovSqlite(t)

	factory := database.NewFactory(func(f *gofakeit.Faker) dbkCovNote {
		return dbkCovNote{Title: f.Word()}
	})

	if _, err := factory.Create(db); err == nil {
		t.Error("Create sans table a reussi")
	}
	if _, err := factory.CreateMany(db, 2); err == nil {
		t.Error("CreateMany sans table a reussi")
	}

	empty, err := factory.CreateMany(db, 0)
	if err != nil {
		t.Fatalf("CreateMany(0): %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("CreateMany(0) = %d lignes", len(empty))
	}
}

func TestDbkCovMigrationHelpersReportABrokenMigrationsTable(t *testing.T) {
	db := dbkCovSqlite(t)

	if err := db.Exec("CREATE TABLE migrations (colonne_inattendue TEXT)").Error; err != nil {
		t.Fatalf("creation de la table migrations: %v", err)
	}

	if _, err := database.Applied(db); err == nil {
		t.Error("Applied a lu une table migrations sans colonne id")
	}
	if _, err := database.StatusList(db); err == nil {
		t.Error("StatusList a accepte une table migrations cassee")
	}
	if _, err := database.Pending(db); err == nil {
		t.Error("Pending a accepte une table migrations cassee")
	}
	if err := database.Reset(db); err == nil {
		t.Error("Reset a accepte une table migrations cassee")
	}
}

func TestDbkCovResetStopsOnAnUnrollbackableMigration(t *testing.T) {
	db := dbkCovSqlite(t)

	if err := db.Exec("CREATE TABLE migrations (id VARCHAR(255) PRIMARY KEY)").Error; err != nil {
		t.Fatalf("creation de la table migrations: %v", err)
	}
	if err := db.Exec("INSERT INTO migrations (id) VALUES ('dbkcov-inconnue')").Error; err != nil {
		t.Fatalf("insertion: %v", err)
	}

	applied, err := database.Applied(db)
	if err != nil {
		t.Fatalf("Applied: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("Applied = %v, attendu une entree", applied)
	}

	if err := database.Reset(db); err == nil {
		t.Fatal("Reset a pretendu annuler une migration inconnue")
	}
}

func TestDbkCovDropAllTablesReportsAClosedConnection(t *testing.T) {
	db := dbkCovSqlite(t)

	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("fermeture: %v", err)
	}

	if err := database.DropAllTables(db); err == nil {
		t.Error("DropAllTables a accepte une connexion fermee")
	}
	if err := database.Fresh(db); err == nil {
		t.Error("Fresh a accepte une connexion fermee")
	}
}

func TestDbkCovDropAllTablesAcceptsAnEmptyDatabase(t *testing.T) {
	db := dbkCovSqlite(t)

	if err := database.DropAllTables(db); err != nil {
		t.Fatalf("DropAllTables sur une base vide: %v", err)
	}
}

func TestDbkCovDropAllTablesReportsAFailedForeignKeySuspension(t *testing.T) {
	db := dbkCovSqlite(t)

	if err := db.Exec("CREATE TABLE dbk_cov_rows (id INTEGER)").Error; err != nil {
		t.Fatalf("creation de table: %v", err)
	}
	dbkCovBreakExec(t, db, "PRAGMA foreign_keys = OFF")

	if err := database.DropAllTables(db); !errors.Is(err, dbkCovErrSabotage) {
		t.Fatalf("DropAllTables = %v, attendu %v", err, dbkCovErrSabotage)
	}
}

func TestDbkCovDropAllTablesReportsAFailedDrop(t *testing.T) {
	db := dbkCovSqlite(t)

	if err := db.Exec("CREATE TABLE dbk_cov_rows (id INTEGER)").Error; err != nil {
		t.Fatalf("creation de table: %v", err)
	}
	dbkCovBreakExec(t, db, "DROP TABLE")

	err := database.DropAllTables(db)
	if !errors.Is(err, dbkCovErrSabotage) {
		t.Fatalf("DropAllTables = %v, attendu %v", err, dbkCovErrSabotage)
	}
	if !strings.Contains(err.Error(), "dbk_cov_rows") {
		t.Errorf("l'erreur ne nomme pas la table: %v", err)
	}
}

func TestDbkCovSeedersAreOrderedReplacedAndRun(t *testing.T) {
	db := dbkCovSqlite(t)

	runs := map[string]int{}
	database.RegisterSeeder(database.SeederFunc{
		SeederName: "dbkCovDernier",
		Fn:         func(*gorm.DB) error { runs["dernier"]++; return nil },
	}, 900)
	database.RegisterSeeder(database.SeederFunc{
		SeederName: "dbkCovPremier",
		Fn:         func(*gorm.DB) error { runs["remplace"]++; return nil },
	}, 1)
	database.RegisterSeeder(database.SeederFunc{
		SeederName: "dbkCovPremier",
		Fn: func(*gorm.DB) error {
			runs["premier"]++
			if dbkCovSeederFails {
				return dbkCovErrSabotage
			}
			return nil
		},
	}, 1)

	positions := map[string]int{}
	for i, seeder := range database.Seeders() {
		positions[seeder.Name()] = i
	}
	if positions["dbkCovPremier"] >= positions["dbkCovDernier"] {
		t.Fatalf("ordre des seeders incorrect: %v", positions)
	}

	if err := database.Seed(db); err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if runs["remplace"] != 0 {
		t.Errorf("le seeder remplace a tourne %d fois", runs["remplace"])
	}
	if runs["premier"] != 1 || runs["dernier"] != 1 {
		t.Fatalf("executions = %v", runs)
	}

	dbkCovSeederFails = true
	t.Cleanup(func() { dbkCovSeederFails = false })

	if err := database.Seed(db); !errors.Is(err, dbkCovErrSabotage) {
		t.Fatalf("Seed complet = %v, attendu %v", err, dbkCovErrSabotage)
	}
	if err := database.Seed(db, "dbkCovPremier"); !errors.Is(err, dbkCovErrSabotage) {
		t.Fatalf("Seed nomme = %v, attendu %v", err, dbkCovErrSabotage)
	}
}
