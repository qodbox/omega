package api

import (
	"reflect"
	"strconv"
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

	// Size is the maximum length of a text column, when the driver or the gorm
	// tag reports one; 0 means unbounded.
	Size int

	// Decimal marks an exact numeric column (DECIMAL, NUMERIC). Its values travel
	// as strings, in and out, so no amount ever goes through a float64.
	Decimal   bool
	Precision int
	Scale     int
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

	// Actions, when set, lists the actions served; nil serves list and view,
	// plus the writes when the resource is writable.
	Actions map[string]bool

	// Owner names the column holding the id of the owning actor. Empty means the
	// rows are not owned.
	Owner string

	// Guarded columns are only written by actors holding <plural>.any.
	Guarded map[string]bool

	explicitHidden map[string]bool
}

func (r *Resource) writable() bool { return !r.Discovered || r.AllowWrite }

type Registry struct {
	mu        sync.RWMutex
	db        *gorm.DB
	resources []*Resource

	// Set through Protect. The GraphQL resolvers consult these; the REST routes
	// are guarded by the Authorize middleware instead.
	gate      Gate
	actorFrom ActorFrom
	identify  ActorID

	limits Limits

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
			if resource.Plural != plural {
				continue
			}
			resource.AllowWrite = true
			if resource.Actions != nil {
				for _, action := range []string{ActionCreate, ActionUpdate, ActionDelete} {
					resource.Actions[action] = true
				}
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
		if structField.Anonymous && structField.Tag.Get("json") == "" {
			embedded := structField.Type
			if embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if embedded.Kind() == reflect.Struct && embedded.String() != "time.Time" {
				fields = append(fields, describe(embedded)...)
				continue
			}
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

// visible strips what must not leave the server: hidden fields under their name
// and their column, deleted_at, and — for a registered model — every column the
// model does not describe (a json:"-" field still comes back from SELECT *).
func (r *Resource) visible(row map[string]any) map[string]any {
	for name := range r.Hidden {
		delete(row, name)
	}
	described := make(map[string]bool, len(r.Fields)*2)
	for _, field := range r.Fields {
		if r.Hidden[field.Name] {
			delete(row, field.Column)
			continue
		}
		described[field.Name] = true
		described[field.Column] = true
	}
	if !r.Discovered && len(r.Fields) > 0 {
		for column := range row {
			if !described[column] {
				delete(row, column)
			}
		}
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

		if field.Decimal {
			row[field.Name] = decimalText(value)
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

// decimalText renders an exact numeric as a string. Drivers hand it back as
// text (PostgreSQL, the CAST in readable), as an integer, or — SQLite storing a
// value it could keep losslessly — as a float whose shortest form is exact.
func decimalText(value any) any {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return value
	}
}

// annotate completes the fields of a registered model with what its gorm tags
// say: the size of text columns and the exact numeric columns.
func annotate(r *Registry, resource *Resource) {
	if r.db == nil || resource.Model == nil {
		return
	}
	statement := &gorm.Statement{DB: r.db}
	if err := statement.Parse(resource.Model); err != nil || statement.Schema == nil {
		return
	}

	for index := range resource.Fields {
		field := &resource.Fields[index]
		parsed := statement.Schema.LookUpField(field.Column)
		if parsed == nil {
			continue
		}

		declared := strings.ToLower(parsed.TagSettings["TYPE"])
		goType := parsed.FieldType.String()
		if strings.Contains(declared, "decimal") || strings.Contains(declared, "numeric") ||
			strings.HasSuffix(goType, ".Decimal") {
			field.Decimal = true
			field.Type, field.Format = "string", formatDecimal
			field.Precision, field.Scale = parsed.Precision, parsed.Scale
			if field.Precision == 0 {
				field.Precision, field.Scale = parseSize(declared)
			}
			continue
		}

		if field.Kind == reflect.String && parsed.Size > 0 {
			field.Size = parsed.Size
		}
	}
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
	if r.Hidden[name] || name == deletedAt || r.hiddenColumn(name) {
		return false
	}
	return r.has(name)
}

// hiddenColumn reports a name that is the column of a hidden field, so a
// json name differing from the column cannot reach it through the back door.
func (r *Resource) hiddenColumn(name string) bool {
	for _, field := range r.Fields {
		if field.Column == name && r.Hidden[field.Name] {
			return true
		}
	}
	return false
}

func (r *Resource) field(name string) (Field, bool) {
	for _, field := range r.Fields {
		if field.Name == name || field.Column == name {
			return field, true
		}
	}
	return Field{}, false
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
