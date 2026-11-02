package api

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/jinzhu/inflection"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// DiscoveryMode decides which tables Discover publishes.
type DiscoveryMode string

const (
	// DiscoverAll publishes every table that is not internal: the historical
	// behaviour, convenient while prototyping.
	DiscoverAll DiscoveryMode = "all"
	// DiscoverNone publishes nothing; only api.Register'ed resources are served.
	DiscoverNone DiscoveryMode = "none"
	// DiscoverAllowlist publishes only the tables named in Discovery.Tables.
	DiscoverAllowlist DiscoveryMode = "allowlist"
)

// Discovery configures DiscoverWith.
type Discovery struct {
	Mode   DiscoveryMode
	Tables []string
}

// ResolveDiscovery turns the configured values into a Discovery. An empty mode
// means "all", except in production, where it means "none": a forgotten
// setting must not publish the whole schema.
func ResolveDiscovery(env, mode string, tables any) (Discovery, error) {
	discovery := Discovery{Tables: SplitList(tables)}

	switch DiscoveryMode(strings.ToLower(strings.TrimSpace(mode))) {
	case "":
		discovery.Mode = DiscoverAll
		if strings.EqualFold(env, "production") || strings.EqualFold(env, "prod") {
			discovery.Mode = DiscoverNone
		}
	case DiscoverAll:
		discovery.Mode = DiscoverAll
	case DiscoverNone:
		discovery.Mode = DiscoverNone
	case DiscoverAllowlist:
		discovery.Mode = DiscoverAllowlist
	default:
		return discovery, fmt.Errorf("api: unknown discovery mode %q (want all, none or allowlist)", mode)
	}
	return discovery, nil
}

// SplitList reads a list from configuration: a YAML sequence, or a comma
// separated string coming from an environment variable.
func SplitList(value any) []string {
	var raw []string
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		raw = strings.Split(typed, ",")
	case []string:
		raw = typed
	case []any:
		for _, item := range typed {
			raw = append(raw, fmt.Sprint(item))
		}
	default:
		raw = strings.Split(fmt.Sprint(typed), ",")
	}

	out := make([]string, 0, len(raw))
	for _, item := range raw {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// Discover publishes every non-internal table, read-only. It is
// DiscoverWith(db, Discovery{Mode: DiscoverAll}).
func (r *Registry) Discover(db *gorm.DB) error {
	_, err := r.DiscoverWith(db, Discovery{Mode: DiscoverAll})
	return err
}

// DiscoverWith publishes the tables the mode selects and returns their names.
// Each one is logged at warn level: a table reaching the API by discovery is
// something an operator should see at every boot.
func (r *Registry) DiscoverWith(db *gorm.DB, discovery Discovery) ([]string, error) {
	if db == nil || discovery.Mode == DiscoverNone {
		return nil, nil
	}

	allowed := map[string]bool{}
	for _, table := range discovery.Tables {
		allowed[table] = true
	}

	migrator := db.Migrator()
	tables, err := migrator.GetTables()
	if err != nil {
		return nil, err
	}

	present := map[string]bool{}
	published := []string{}
	for _, table := range tables {
		present[table] = true
		if discovery.Mode == DiscoverAllowlist && !allowed[table] {
			continue
		}
		if internalTable(table) {
			if discovery.Mode == DiscoverAllowlist {
				log.Warn().Str("table", table).Msg("api: an internal table is never published, even when allow-listed")
			}
			continue
		}
		if r.byTable(table) != nil {
			continue
		}

		columns, err := migrator.ColumnTypes(table)
		if err != nil {
			continue
		}

		resource := discovered(table, columns, db.Dialector.Name())

		r.mu.Lock()
		r.resources = append(r.resources, resource)
		r.mu.Unlock()

		published = append(published, table)
		log.Warn().Str("table", table).Str("mode", string(discovery.Mode)).
			Msg("api: discovery published a table (read-only, sensitive columns hidden)")
	}

	if discovery.Mode == DiscoverAllowlist {
		for _, table := range discovery.Tables {
			if !present[table] {
				log.Warn().Str("table", table).Msg("api: an allow-listed table does not exist")
			}
		}
	}
	return published, nil
}

func discovered(table string, columns []gorm.ColumnType, dialect string) *Resource {
	resource := &Resource{
		Discovered: true,
		Table:      table,
		Plural:     table,
		Singular:   inflection.Singular(table),
		Hidden:     map[string]bool{},
		Fields:     make([]Field, 0, len(columns)),
	}
	if resource.Singular == resource.Plural {
		resource.Singular = resource.Plural + "_item"
	}

	for _, column := range columns {
		name := column.Name()
		openapi, format := columnType(column)
		if format == formatDecimal && ambiguousNumeric(column, dialect) {
			openapi, format = "number", ""
		}
		field := Field{
			Name:     name,
			Column:   name,
			Kind:     reflect.String,
			Type:     openapi,
			Format:   format,
			ReadOnly: name == "id" || strings.HasSuffix(name, "_at"),
		}

		if format == formatDecimal {
			field.Decimal = true
			field.Precision, field.Scale = decimalSize(column)
		}
		if openapi == "string" && format == "" {
			if length, ok := column.Length(); ok && length > 0 && length < maxReportedLength {
				field.Size = int(length)
			}
		}
		resource.Fields = append(resource.Fields, field)
	}

	hideSensitive(resource, true)
	return resource
}

// ambiguousNumeric spots SQLite's bare NUMERIC: gorm declares booleans that
// way there, so without a declared precision the column is not taken for an
// exact decimal.
func ambiguousNumeric(column gorm.ColumnType, dialect string) bool {
	if dialect != "sqlite" {
		return false
	}
	declared, _ := column.ColumnType()
	return !strings.Contains(strings.ToLower(declared+column.DatabaseTypeName()), "decimal") &&
		!strings.Contains(declared, "(")
}

// Some drivers report an unbounded TEXT as math.MaxInt64; anything this large
// is not a size worth enforcing.
const maxReportedLength = 1 << 24

var sizePattern = regexp.MustCompile(`\(\s*(\d+)\s*(?:,\s*(\d+)\s*)?\)`)

func decimalSize(column gorm.ColumnType) (int, int) {
	if precision, scale, ok := column.DecimalSize(); ok && precision > 0 {
		return int(precision), int(scale)
	}
	declared, _ := column.ColumnType()
	if declared == "" {
		declared = column.DatabaseTypeName()
	}
	return parseSize(declared)
}

func parseSize(declared string) (int, int) {
	match := sizePattern.FindStringSubmatch(declared)
	if match == nil {
		return 0, 0
	}
	precision, _ := strconv.Atoi(match[1])
	scale, _ := strconv.Atoi(match[2])
	return precision, scale
}

func internalTable(table string) bool {
	switch table {
	case "migrations", "schema_migrations", "refresh_tokens",
		"jobs", "failed_jobs", "sessions", "cache", "password_resets",
		"sqlite_sequence", "goose_db_version",
		// Written by the authentication layer alone: identifiers and client
		// addresses of failed logins.
		"login_attempts",
		// Billing state is written by Stripe's webhooks and by nothing else.
		// Published as resources these would let any authenticated caller POST
		// themselves a subscription, so they never reach the router.
		"billing_customers", "billing_subscriptions", "billing_events":
		return true
	}
	return strings.HasPrefix(table, "sqlite_") || strings.HasPrefix(table, "pg_")
}

// Secrets: never served, whatever the resource.
var sensitiveNames = map[string]bool{
	"password": true, "password_hash": true, "secret": true, "token": true,
	"api_key": true, "private_key": true, "salt": true, "signature": true,
	"otp": true, "pin": true, "cvv": true, "iban": true, "ssn": true,
	"credit_card": true, "card_number": true, "mfa_secret": true,
	"recovery_code": true, "session_id": true, "remember_token": true,
	"payload": true, "nonce": true,
}

var sensitiveSuffixes = []string{
	"_password", "_secret", "_token", "_hash", "_key", "_salt",
	"_signature", "_otp", "_pin", "_cvv", "_iban", "_ssn",
	"_card", "_code", "_credential", "_payload",
}

// Personal data: hidden by default on every resource, lifted with Visible.
var personalNames = map[string]bool{
	"msisdn": true, "ip": true, "ip_address": true,
	"date_of_birth": true, "dob": true, "birth_date": true, "birthdate": true,
	"national_id": true, "tax_id": true,
}

var personalPrefixes = []string{"phone", "mobile", "address", "passport", "document", "kyc"}

var personalSuffixes = []string{"_phone", "_mobile", "_msisdn", "_address", "_ip", "_dob", "_national_id"}

// Emails are hidden on discovered tables only: a registered model is a
// deliberate act (the users resource serves its email on purpose), a table
// that merely exists is not.
func discoveredOnly(column string) bool {
	return column == "email" || strings.HasSuffix(column, "_email")
}

func sensitive(column string) bool {
	if sensitiveNames[column] {
		return true
	}
	for _, suffix := range sensitiveSuffixes {
		if strings.HasSuffix(column, suffix) {
			return true
		}
	}
	return false
}

func personal(column string) bool {
	if personalNames[column] {
		return true
	}
	for _, prefix := range personalPrefixes {
		if strings.HasPrefix(column, prefix) {
			return true
		}
	}
	for _, suffix := range personalSuffixes {
		if strings.HasSuffix(column, suffix) {
			return true
		}
	}
	return false
}

func hideSensitive(resource *Resource, discovered bool) {
	for _, field := range resource.Fields {
		for _, name := range []string{field.Name, field.Column} {
			lower := strings.ToLower(name)
			if sensitive(lower) || personal(lower) || (discovered && discoveredOnly(lower)) {
				resource.Hidden[field.Name] = true
			}
		}
	}
}

const formatDecimal = "decimal"

func columnType(column gorm.ColumnType) (string, string) {
	name := strings.ToLower(column.DatabaseTypeName())

	switch {
	case strings.Contains(name, "bool"):
		return "boolean", ""
	case strings.Contains(name, "int"):
		return "integer", ""
	case strings.Contains(name, "decimal"), strings.Contains(name, "numeric"),
		strings.Contains(name, "money"):
		return "string", formatDecimal
	case strings.Contains(name, "float"), strings.Contains(name, "double"),
		strings.Contains(name, "real"):
		return "number", ""
	case strings.Contains(name, "timestamp"), strings.Contains(name, "datetime"):
		return "string", "date-time"
	case name == "date":
		return "string", "date"
	case strings.Contains(name, "time"):
		return "string", "time"
	case strings.Contains(name, "json"):
		return "object", ""
	case strings.Contains(name, "blob"), strings.Contains(name, "bytea"):
		return "string", "byte"
	case strings.Contains(name, "uuid"):
		return "string", "uuid"
	default:
		return "string", ""
	}
}

func (r *Registry) byTable(table string) *Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, resource := range r.resources {
		if resource.Table == table || resource.Plural == table {
			return resource
		}
	}
	return nil
}
