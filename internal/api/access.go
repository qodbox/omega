package api

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	"gorm.io/gorm"
)

// The failures every entry point (REST and GraphQL) shares; each maps them to
// its own transport.
var (
	errUnauthenticated = errors.New("authentication required")
	errNotFound        = errors.New("not found")
)

type forbiddenError struct{ ability string }

func (e forbiddenError) Error() string { return "you may not " + e.ability }

type invalidError struct{ message string }

func (e invalidError) Error() string { return e.message }

func invalid(format string, args ...any) error {
	return invalidError{message: fmt.Sprintf(format, args...)}
}

// ActorID extracts the identifier an Owner column is compared with.
type ActorID func(actor any) any

// IdentifyWith overrides how the id of an actor is read. By default it is the
// ID field (or ID() method) of the actor, then its Key() string.
func (r *Registry) IdentifyWith(identify ActorID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.identify = identify
}

// access is what an operation knows about its caller once checked.
type access struct {
	actor any
	gated bool

	// holdsAny: the actor holds <plural>.any — every row, guarded columns,
	// and the choice of the owner on create.
	holdsAny bool

	// unscoped: the resource is not owned, or the actor holds <plural>.any.
	unscoped bool
	ownerID  any
}

// begin authenticates the caller and, for the actions that have no row (list
// and create), asks the policies. Row actions are asked once the row is
// loaded, with the row as subject — see authorizeRow.
//
// strict refuses everything when no gate is configured: GraphQL has always
// done so. REST without a gate keeps serving as it did, unless the resource is
// owned, which cannot be honoured without knowing the actor.
func (r *Registry) begin(ctx, carrier context.Context, resource *Resource, action string, strict bool) (*access, error) {
	r.mu.RLock()
	gate, actorFrom, identify := r.gate, r.actorFrom, r.identify
	r.mu.RUnlock()

	ability := resource.Plural + "." + action
	if !resource.Allows(action) {
		return nil, forbiddenError{ability: ability}
	}

	if gate == nil || actorFrom == nil {
		if strict {
			return nil, fmt.Errorf("graphql: no policy gate is configured, refusing %s", ability)
		}
		if resource.Owner != "" {
			return nil, forbiddenError{ability: ability}
		}
		return &access{unscoped: true}, nil
	}

	actor := actorFrom(carrier)
	if actor == nil {
		return nil, errUnauthenticated
	}

	granted := &access{actor: actor, gated: true, unscoped: true}

	if action == ActionList || action == ActionCreate {
		if !gate(ctx, actor, ability) {
			return nil, forbiddenError{ability: ability}
		}
	}

	granted.holdsAny = gate(ctx, actor, resource.Plural+"."+AbilityAny)
	if resource.Owner != "" && !granted.holdsAny {
		granted.unscoped = false
		granted.ownerID = actorIdentity(actor, identify)
		if granted.ownerID == nil {
			return nil, forbiddenError{ability: ability}
		}
	}
	return granted, nil
}

// privileged reports an actor that may write guarded columns and choose the
// owner of a row.
func (a *access) privileged() bool { return a.gated && a.holdsAny }

// authorizeRow asks the policies about a loaded row. A registered model is
// handed to the rule as the model itself (*models.User...), a discovered table
// as its column map.
func (r *Registry) authorizeRow(ctx context.Context, resource *Resource, granted *access, action string, row map[string]any) error {
	if !granted.gated {
		return nil
	}
	r.mu.RLock()
	gate := r.gate
	r.mu.RUnlock()

	ability := resource.Plural + "." + action
	if !gate(ctx, granted.actor, ability, r.subject(ctx, resource, row)) {
		return forbiddenError{ability: ability}
	}
	return nil
}

func (r *Registry) subject(ctx context.Context, resource *Resource, row map[string]any) any {
	if resource.Model == nil {
		return row
	}
	typ := reflect.TypeOf(resource.Model)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return row
	}

	model := reflect.New(typ).Interface()
	if err := r.db.WithContext(ctx).Table(tableOf(r.db, resource)).
		Where("id = ?", row["id"]).Take(model).Error; err != nil {
		return row
	}
	return model
}

// scope opens the table of a resource with the rows the caller may reach: not
// soft-deleted, and owned by the actor unless the resource is unscoped for them.
func (r *Registry) scope(ctx context.Context, resource *Resource, granted *access) *gorm.DB {
	query := r.db.WithContext(ctx).Table(tableOf(r.db, resource))
	if resource.softDeletes() {
		query = query.Where(deletedAt + " IS NULL")
	}
	if !granted.unscoped {
		query = query.Where(quote(query, resource.Owner)+" = ?", granted.ownerID)
	}
	return query
}

// readable selects the columns of a row, re-reading exact numerics as text so
// a driver that scans NUMERIC into float64 (pgx) cannot round an amount.
// SQLite is left alone: its NUMERIC values are INTEGER, REAL holding at most 15
// significant digits (see exactDigits), or TEXT, and CAST would print a REAL
// in exponent form; decimalText renders the float exactly instead.
func readable(query *gorm.DB, resource *Resource) *gorm.DB {
	if dialect(query) == "sqlite" {
		return query
	}
	casts := []string{}
	for _, field := range resource.Fields {
		if !field.Decimal || resource.Hidden[field.Name] {
			continue
		}
		column := quote(query, field.Column)
		casts = append(casts, "CAST("+column+" AS "+textType(query)+") AS "+column)
	}
	if len(casts) == 0 {
		return query
	}
	return query.Select("*, " + strings.Join(casts, ", "))
}

func textType(query *gorm.DB) string {
	if dialect(query) == "mysql" {
		return "CHAR"
	}
	return "TEXT"
}

func dialect(query *gorm.DB) string {
	if query == nil || query.Dialector == nil {
		return ""
	}
	return query.Dialector.Name()
}

// exactDigits is the number of significant digits a decimal column keeps
// exactly; 0 means the column's own precision is the only bound.
func (r *Registry) exactDigits() int {
	if dialect(r.db) == "sqlite" {
		return 15
	}
	return 0
}

func quote(query *gorm.DB, column string) string {
	if query.Statement == nil || query.Dialector == nil {
		return column
	}
	return query.Statement.Quote(column)
}

// load reads one row the caller may reach.
func (r *Registry) load(ctx context.Context, resource *Resource, granted *access, id any) (map[string]any, error) {
	row := map[string]any{}
	err := readable(r.scope(ctx, resource, granted), resource).Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	return row, nil
}

func actorIdentity(actor any, identify ActorID) any {
	if identify != nil {
		return identify(actor)
	}
	if typed, ok := actor.(interface{ ID() any }); ok {
		return typed.ID()
	}

	value := reflect.ValueOf(actor)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Struct {
		if field := value.FieldByName("ID"); field.IsValid() && field.CanInterface() {
			if field.IsZero() {
				return nil
			}
			return field.Interface()
		}
	}

	if typed, ok := actor.(interface{ Key() string }); ok && typed.Key() != "" {
		return typed.Key()
	}
	return nil
}
