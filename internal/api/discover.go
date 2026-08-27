package api

import (
	"reflect"
	"strings"

	"github.com/jinzhu/inflection"
	"gorm.io/gorm"
)

func (r *Registry) Discover(db *gorm.DB) error {
	if db == nil {
		return nil
	}

	migrator := db.Migrator()
	tables, err := migrator.GetTables()
	if err != nil {
		return err
	}

	for _, table := range tables {
		if internalTable(table) || r.byTable(table) != nil {
			continue
		}

		columns, err := migrator.ColumnTypes(table)
		if err != nil {
			continue
		}

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
			if sensitive(name) {
				resource.Hidden[name] = true
			}

			openapi, format := columnType(column)
			resource.Fields = append(resource.Fields, Field{
				Name:   name,
				Column: name,
				Kind:   reflect.String,
				Type:   openapi,
				Format: format,

				ReadOnly: name == "id" || strings.HasSuffix(name, "_at"),
			})
		}

		r.mu.Lock()
		r.resources = append(r.resources, resource)
		r.mu.Unlock()
	}
	return nil
}

func internalTable(table string) bool {
	switch table {
	case "migrations", "schema_migrations", "refresh_tokens",
		"jobs", "failed_jobs", "sessions", "cache", "password_resets",
		"sqlite_sequence", "goose_db_version",
		// Billing state is written by Stripe's webhooks and by nothing else.
		// Published as resources these would let any authenticated caller POST
		// themselves a subscription, so they never reach the router.
		"billing_customers", "billing_subscriptions", "billing_events":
		return true
	}
	return strings.HasPrefix(table, "sqlite_") || strings.HasPrefix(table, "pg_")
}

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

func columnType(column gorm.ColumnType) (string, string) {
	name := strings.ToLower(column.DatabaseTypeName())

	switch {
	case strings.Contains(name, "bool"):
		return "boolean", ""
	case strings.Contains(name, "int"):

		return "integer", ""
	case strings.Contains(name, "float"), strings.Contains(name, "double"),
		strings.Contains(name, "real"), strings.Contains(name, "decimal"),
		strings.Contains(name, "numeric"):
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
