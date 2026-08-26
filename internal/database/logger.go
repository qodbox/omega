package database

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

type gormLogger struct {
	log   zerolog.Logger
	level gormlogger.LogLevel
	slow  time.Duration
}

func newGormLogger(log zerolog.Logger, level string, slow time.Duration) gormlogger.Interface {
	if slow <= 0 {
		slow = 200 * time.Millisecond
	}
	return &gormLogger{
		log:   log.With().Str("component", "gorm").Logger(),
		level: parseGormLevel(level),
		slow:  slow,
	}
}

func parseGormLevel(name string) gormlogger.LogLevel {
	switch strings.ToLower(name) {
	case "silent", "off", "none":
		return gormlogger.Silent
	case "error":
		return gormlogger.Error
	case "info", "debug", "all":
		return gormlogger.Info
	default:
		return gormlogger.Warn
	}
}

func (l *gormLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	clone := *l
	clone.level = level
	return &clone
}

func (l *gormLogger) Info(_ context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Info {
		l.log.Info().Msgf(msg, data...)
	}
}

func (l *gormLogger) Warn(_ context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Warn {
		l.log.Warn().Msgf(msg, data...)
	}
}

func (l *gormLogger) Error(_ context.Context, msg string, data ...any) {
	if l.level >= gormlogger.Error {
		l.log.Error().Msgf(msg, data...)
	}
}

func (l *gormLogger) Trace(_ context.Context, begin time.Time, fc func() (string, int64), err error) {
	if l.level <= gormlogger.Silent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	switch {
	case err != nil && !errors.Is(err, gorm.ErrRecordNotFound) && l.level >= gormlogger.Error:
		l.log.Error().Err(err).Dur("elapsed", elapsed).Int64("rows", rows).Str("sql", sql).Msg("query failed")
	case elapsed > l.slow && l.level >= gormlogger.Warn:
		l.log.Warn().Dur("elapsed", elapsed).Int64("rows", rows).Str("sql", sql).Msg("slow query")
	case l.level >= gormlogger.Info:
		l.log.Debug().Dur("elapsed", elapsed).Int64("rows", rows).Str("sql", sql).Msg("query")
	}
}
