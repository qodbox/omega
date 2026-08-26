package api

import (
	"reflect"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

type Field struct {
	Name     string
	Column   string
	Kind     reflect.Kind
	Type     string
	Format   string
	ReadOnly bool
}

type Resource struct {
	Plural   string
	Singular string

	Table  string
	Model  any
	Fields []Field

	Hidden map[string]bool

	Discovered bool
	AllowWrite bool
}

func (r *Resource) writable() bool { return !r.Discovered || r.AllowWrite }

type Registry struct {
	mu        sync.RWMutex
	db        *gorm.DB
	resources []*Resource

	extra map[string]PathItem
}

func (r *Registry) Document(path string, item PathItem) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.extra == nil {
		r.extra = map[string]PathItem{}
	}
	r.extra[path] = item
}

func NewRegistry(db *gorm.DB) *Registry { return &Registry{db: db} }

func (r *Registry) DB() *gorm.DB { return r.db }

func (r *Registry) All() []*Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]*Resource(nil), r.resources...)
}

func (r *Registry) AllowWrites(plurals ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, plural := range plurals {
		for _, resource := range r.resources {
			if resource.Plural == plural {
				resource.AllowWrite = true
			}
		}
	}
}

func (r *Registry) Find(plural string) *Resource {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, resource := range r.resources {
		if resource.Plural == plural {
			return resource
		}
	}
	return nil
}

func Register[T any](r *Registry, plural, singular string, hidden ...string) *Resource {
	var zero T
	resource := &Resource{
		Plural:   plural,
		Singular: singular,
		Model:    &zero,
		Hidden:   map[string]bool{},
		Fields:   describe(reflect.TypeOf(zero)),
	}
	resource.Table = tableOf(r.db, resource)
	for _, name := range hidden {
		resource.Hidden[name] = true
	}

	r.mu.Lock()
	r.resources = append(r.resources, resource)
	r.mu.Unlock()
	return resource
}

func describe(typ reflect.Type) []Field {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil
	}

	fields := make([]Field, 0, typ.NumField())
	for index := 0; index < typ.NumField(); index++ {
		structField := typ.Field(index)
		if !structField.IsExported() {
			continue
		}

		name, options, _ := strings.Cut(structField.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = toSnake(structField.Name)
		}

		kind := structField.Type.Kind()
		if kind == reflect.Pointer {
			kind = structField.Type.Elem().Kind()
		}

		openapiType, format := openapiType(structField.Type)
		fields = append(fields, Field{
			Name:     name,
			Column:   toSnake(structField.Name),
			Kind:     kind,
			Type:     openapiType,
			Format:   format,
			ReadOnly: name == "id" || strings.HasSuffix(name, "_at") || strings.Contains(options, "readonly"),
		})
	}
	return fields
}

func openapiType(typ reflect.Type) (string, string) {
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch typ.String() {
	case "time.Time":
		return "string", "date-time"
	case "gorm.DeletedAt":
		return "string", "date-time"
	}
	switch typ.Kind() {
	case reflect.Bool:
		return "boolean", ""
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "integer", ""
	case reflect.Float32, reflect.Float64:
		return "number", ""
	case reflect.Slice, reflect.Array:
		return "array", ""
	case reflect.Map, reflect.Struct:
		return "object", ""
	default:
		return "string", ""
	}
}

func toSnake(name string) string {
	var out strings.Builder
	runes := []rune(name)
	for index, r := range runes {
		if r >= 'A' && r <= 'Z' {
			if index > 0 && (runes[index-1] < 'A' || runes[index-1] > 'Z') {
				out.WriteRune('_')
			}
			out.WriteRune(r + 32)
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

const (
	createdAt = "created_at"
	updatedAt = "updated_at"
	deletedAt = "deleted_at"
)

func (r *Resource) visible(row map[string]any) map[string]any {
	for name := range r.Hidden {
		delete(row, name)
	}

	delete(row, deletedAt)
	return row
}

func (r *Resource) serialise(row map[string]any) map[string]any {
	r.visible(row)

	for _, field := range r.Fields {
		value, ok := row[field.Name]
		if !ok || value == nil {
			continue
		}

		if raw, isBytes := value.([]byte); isBytes {
			row[field.Name] = string(raw)
			continue
		}

		if moment, isTime := value.(time.Time); isTime {
			row[field.Name] = moment.Format(time.RFC3339Nano)
			continue
		}

		if field.Type == "boolean" {
			if number, isInt := value.(int64); isInt {
				row[field.Name] = number != 0
			}
		}
	}
	return row
}

func (r *Resource) has(name string) bool {
	for _, field := range r.Fields {
		if field.Name == name || field.Column == name {
			return true
		}
	}
	return false
}

func (r *Resource) filterable(name string) bool {
	if r.Hidden[name] || name == deletedAt {
		return false
	}
	return r.has(name)
}

func (r *Resource) hasColumn(column string) bool {
	for _, field := range r.Fields {
		if field.Column == column {
			return true
		}
	}
	return false
}

func (r *Resource) softDeletes() bool { return r.hasColumn(deletedAt) }
