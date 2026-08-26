package database

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

const migrationTable = "migrations"

var (
	migrationsMu sync.RWMutex
	migrations   []*gormigrate.Migration
)

func Register(migration *gormigrate.Migration) {
	migrationsMu.Lock()
	defer migrationsMu.Unlock()

	for _, existing := range migrations {
		if existing.ID == migration.ID {
			panic(fmt.Sprintf("database: duplicate migration id %q", migration.ID))
		}
	}
	migrations = append(migrations, migration)
}

func Migrations() []*gormigrate.Migration {
	migrationsMu.RLock()
	defer migrationsMu.RUnlock()

	ordered := make([]*gormigrate.Migration, len(migrations))
	copy(ordered, migrations)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].ID < ordered[j].ID })
	return ordered
}

func Migrator(db *gorm.DB) *gormigrate.Gormigrate {
	options := *gormigrate.DefaultOptions
	options.TableName = migrationTable
	options.UseTransaction = true

	return gormigrate.New(db, &options, Migrations())
}

func Up(db *gorm.DB) error {
	if len(Migrations()) == 0 {
		return nil
	}
	return Migrator(db).Migrate()
}

func RollbackLast(db *gorm.DB) error {
	return Migrator(db).RollbackLast()
}

func Reset(db *gorm.DB) error {
	migrator := Migrator(db)
	for {
		applied, err := Applied(db)
		if err != nil {
			return err
		}
		if len(applied) == 0 {
			return nil
		}
		if err := migrator.RollbackLast(); err != nil {
			return err
		}
	}
}

func Fresh(db *gorm.DB) error {
	if err := DropAllTables(db); err != nil {
		return err
	}
	return Up(db)
}

type Status struct {
	ID      string
	Applied bool
}

func Applied(db *gorm.DB) ([]string, error) {
	if !db.Migrator().HasTable(migrationTable) {
		return nil, nil
	}

	var ids []string
	if err := db.Table(migrationTable).Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("read %s: %w", migrationTable, err)
	}
	return ids, nil
}

func StatusList(db *gorm.DB) ([]Status, error) {
	applied, err := Applied(db)
	if err != nil {
		return nil, err
	}

	done := make(map[string]bool, len(applied))
	for _, id := range applied {
		done[id] = true
	}

	all := Migrations()
	statuses := make([]Status, 0, len(all))
	for _, migration := range all {
		statuses = append(statuses, Status{ID: migration.ID, Applied: done[migration.ID]})
	}
	return statuses, nil
}

func Pending(db *gorm.DB) (int, error) {
	statuses, err := StatusList(db)
	if err != nil {
		return 0, err
	}

	count := 0
	for _, status := range statuses {
		if !status.Applied {
			count++
		}
	}
	return count, nil
}

func DropAllTables(db *gorm.DB) error {
	tables, err := db.Migrator().GetTables()
	if err != nil {
		return fmt.Errorf("list tables: %w", err)
	}
	if len(tables) == 0 {
		return nil
	}

	restore, err := suspendForeignKeys(db)
	if err != nil {
		return err
	}
	defer restore()

	for _, table := range tables {
		if isSystemTable(table) {
			continue
		}
		if err := db.Migrator().DropTable(table); err != nil {
			return fmt.Errorf("drop table %s: %w", table, err)
		}
	}
	return nil
}

var systemTablePrefixes = []string{"sqlite_", "pg_", "sql_"}

func isSystemTable(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range systemTablePrefixes {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

func suspendForeignKeys(db *gorm.DB) (func(), error) {
	noop := func() {}

	switch strings.ToLower(db.Dialector.Name()) {
	case "sqlite":
		if err := db.Exec("PRAGMA foreign_keys = OFF").Error; err != nil {
			return noop, err
		}
		return func() { db.Exec("PRAGMA foreign_keys = ON") }, nil
	case "mysql":
		if err := db.Exec("SET FOREIGN_KEY_CHECKS = 0").Error; err != nil {
			return noop, err
		}
		return func() { db.Exec("SET FOREIGN_KEY_CHECKS = 1") }, nil
	default:

		return noop, nil
	}
}
