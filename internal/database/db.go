package database

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type Connection struct {
	Driver   string `mapstructure:"driver"`
	DSN      string `mapstructure:"dsn"`
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Database string `mapstructure:"database"`
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`
	SSLMode  string `mapstructure:"sslmode"`
	Charset  string `mapstructure:"charset"`
	TimeZone string `mapstructure:"timezone"`

	MaxOpenConns    int           `mapstructure:"max_open_conns"`
	MaxIdleConns    int           `mapstructure:"max_idle_conns"`
	ConnMaxLifetime time.Duration `mapstructure:"conn_max_lifetime"`
}

type Config struct {
	Default       string                `mapstructure:"default"`
	Connections   map[string]Connection `mapstructure:"connections"`
	LogLevel      string                `mapstructure:"log_level"`
	SlowThreshold time.Duration         `mapstructure:"slow_threshold"`
}

type Manager struct {
	cfg Config
	log zerolog.Logger

	mu    sync.Mutex
	conns map[string]*gorm.DB
}

func NewManager(cfg Config, log zerolog.Logger) *Manager {
	if cfg.Default == "" {
		cfg.Default = "sqlite"
	}
	return &Manager{cfg: cfg, log: log, conns: make(map[string]*gorm.DB)}
}

func (m *Manager) Default() (*gorm.DB, error) { return m.Connection(m.cfg.Default) }

func (m *Manager) DefaultName() string { return m.cfg.Default }

func (m *Manager) Connection(name string) (*gorm.DB, error) {
	if name == "" {
		name = m.cfg.Default
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if db, ok := m.conns[name]; ok {
		return db, nil
	}

	conn, ok := m.cfg.Connections[name]
	if !ok {
		return nil, fmt.Errorf("database: no connection named %q", name)
	}

	db, err := Open(conn, m.log, m.cfg.LogLevel, m.cfg.SlowThreshold)
	if err != nil {
		return nil, fmt.Errorf("database: connection %q: %w", name, err)
	}

	m.conns[name] = db
	m.log.Debug().Str("connection", name).Str("driver", conn.Driver).Msg("database connected")
	return db, nil
}

func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	for name, db := range m.conns {
		sqlDB, err := db.DB()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		if err := sqlDB.Close(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", name, err))
		}
	}
	m.conns = make(map[string]*gorm.DB)

	if len(errs) > 0 {
		return fmt.Errorf("database: close: %s", strings.Join(errs, "; "))
	}
	return nil
}

func Open(conn Connection, log zerolog.Logger, logLevel string, slow time.Duration) (*gorm.DB, error) {
	dialector, err := dialector(conn)
	if err != nil {
		return nil, err
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		Logger:                                   newGormLogger(log, logLevel, slow),
		DisableForeignKeyConstraintWhenMigrating: false,
		PrepareStmt:                              true,
		SkipDefaultTransaction:                   true,
	})
	if err != nil {
		return nil, err
	}

	if err := applyPool(db, conn); err != nil {
		return nil, err
	}
	return db, nil
}

// Chaque driver s'inscrit ici depuis son propre fichier, derriere une
// build tag: a SQLite-only binary does not embed pgx.
var dialectors = map[string]func(Connection) gorm.Dialector{}

func registerDialector(build func(Connection) gorm.Dialector, aliases ...string) {
	for _, alias := range aliases {
		dialectors[alias] = build
	}
}

func availableDrivers() []string {
	seen := map[string]bool{}
	names := []string{}
	for alias := range dialectors {
		canonical := alias
		switch alias {
		case "sqlite3":
			canonical = "sqlite"
		case "postgresql", "pgsql":
			canonical = "postgres"
		case "mariadb":
			canonical = "mysql"
		}
		if !seen[canonical] {
			seen[canonical] = true
			names = append(names, canonical)
		}
	}
	sort.Strings(names)
	return names
}

func dialector(conn Connection) (gorm.Dialector, error) {
	build, ok := dialectors[strings.ToLower(conn.Driver)]
	if !ok {
		return nil, fmt.Errorf("unsupported driver %q (want %s)",
			conn.Driver, strings.Join(availableDrivers(), ", "))
	}
	return build(conn), nil
}

func sqliteDSN(conn Connection) string {
	if conn.DSN != "" {
		return conn.DSN
	}
	if conn.Database != "" {
		return conn.Database
	}
	return "omega.db"
}

func postgresDSN(conn Connection) string {
	if conn.DSN != "" {
		return conn.DSN
	}
	if conn.Port == 0 {
		conn.Port = 5432
	}
	if conn.SSLMode == "" {
		conn.SSLMode = "disable"
	}
	if conn.TimeZone == "" {
		conn.TimeZone = "UTC"
	}
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s TimeZone=%s",
		conn.Host, conn.Port, conn.Username, conn.Password, conn.Database, conn.SSLMode, conn.TimeZone)
}

func mysqlDSN(conn Connection) string {
	if conn.DSN != "" {
		return conn.DSN
	}
	if conn.Port == 0 {
		conn.Port = 3306
	}
	if conn.Charset == "" {
		conn.Charset = "utf8mb4"
	}
	if conn.TimeZone == "" {
		conn.TimeZone = "Local"
	}
	return fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=%s&parseTime=True&loc=%s",
		conn.Username, conn.Password, conn.Host, conn.Port, conn.Database, conn.Charset, conn.TimeZone)
}

func applyPool(db *gorm.DB, conn Connection) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	if conn.MaxOpenConns > 0 {
		sqlDB.SetMaxOpenConns(conn.MaxOpenConns)
	}
	if conn.MaxIdleConns > 0 {
		sqlDB.SetMaxIdleConns(conn.MaxIdleConns)
	}
	if conn.ConnMaxLifetime > 0 {
		sqlDB.SetConnMaxLifetime(conn.ConnMaxLifetime)
	}
	return nil
}
