package kernel

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	v   *viper.Viper
	dir string
}

var envRef = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)(?::-([^}]*))?\}`)

func LoadConfig(dir string) (*Config, error) {
	_ = godotenv.Load()

	v := viper.New()
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	patterns := []string{"*.yaml", "*.yml"}
	var files []string
	for _, p := range patterns {
		matches, err := filepath.Glob(filepath.Join(dir, p))
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", p, err)
		}
		files = append(files, matches...)
	}

	for _, file := range files {
		raw, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}

		sub := viper.New()
		sub.SetConfigType("yaml")
		if err := sub.ReadConfig(bytes.NewReader(ExpandEnv(raw))); err != nil {
			return nil, fmt.Errorf("parse %s: %w", file, err)
		}

		name := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
		if err := v.MergeConfigMap(map[string]any{name: sub.AllSettings()}); err != nil {
			return nil, fmt.Errorf("merge %s: %w", file, err)
		}
	}

	return &Config{v: v, dir: dir}, nil
}

func ExpandEnv(raw []byte) []byte {
	return envRef.ReplaceAllFunc(raw, func(match []byte) []byte {
		groups := envRef.FindSubmatch(match)
		if val, ok := os.LookupEnv(string(groups[1])); ok && val != "" {
			return []byte(val)
		}
		return groups[2]
	})
}

func (c *Config) Dir() string                       { return c.dir }
func (c *Config) Viper() *viper.Viper               { return c.v }
func (c *Config) Has(key string) bool               { return c.v.IsSet(key) }
func (c *Config) Get(key string) any                { return c.v.Get(key) }
func (c *Config) String(key string) string          { return c.v.GetString(key) }
func (c *Config) Int(key string) int                { return c.v.GetInt(key) }
func (c *Config) Bool(key string) bool              { return c.v.GetBool(key) }
func (c *Config) Duration(key string) time.Duration { return c.v.GetDuration(key) }
func (c *Config) StringSlice(key string) []string   { return c.v.GetStringSlice(key) }
func (c *Config) Map(key string) map[string]any     { return c.v.GetStringMap(key) }

func (c *Config) StringOr(key, def string) string {
	if val := c.v.GetString(key); val != "" {
		return val
	}
	return def
}

func (c *Config) IntOr(key string, def int) int {
	if !c.v.IsSet(key) {
		return def
	}
	return c.v.GetInt(key)
}

func (c *Config) BoolOr(key string, def bool) bool {
	if !c.v.IsSet(key) {
		return def
	}
	return c.v.GetBool(key)
}

func (c *Config) Unmarshal(key string, out any) error {
	return c.v.UnmarshalKey(key, out)
}

func (c *Config) Env() string { return c.StringOr("app.env", "local") }

func (c *Config) IsLocal() bool {
	env := c.Env()
	return env == "local" || env == "development" || env == "dev"
}

func (c *Config) Debug() bool { return c.BoolOr("app.debug", c.IsLocal()) }
