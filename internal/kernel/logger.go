package kernel

import (
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

func NewLogger(cfg *Config) zerolog.Logger {
	level := parseLevel(cfg.StringOr("app.log_level", defaultLevel(cfg)))
	zerolog.SetGlobalLevel(level)
	zerolog.TimeFieldFormat = time.RFC3339

	var out io.Writer = os.Stdout
	if cfg.IsLocal() {
		out = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: "15:04:05",
			NoColor:    !colourise,

			FieldsExclude: []string{
				"app", "request_id", "ip", "htmx", "component",
				"method", "path", "status", "duration",
			},

			FormatLevel: consoleLevel,
		}
	}

	logger := zerolog.New(out).Level(level).With().Timestamp().Logger()

	if name := cfg.String("app.name"); name != "" && !cfg.IsLocal() {
		logger = logger.With().Str("app", name).Logger()
	}
	return logger
}

func defaultLevel(cfg *Config) string {
	if cfg.IsLocal() {
		return "debug"
	}
	return "info"
}

func parseLevel(name string) zerolog.Level {
	level, err := zerolog.ParseLevel(strings.ToLower(name))
	if err != nil || level == zerolog.NoLevel {
		return zerolog.InfoLevel
	}
	return level
}
