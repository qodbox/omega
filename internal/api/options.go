package api

import (
	"fmt"
	"reflect"
)

// The actions a resource can serve. They double as the second half of the
// ability the policies are asked about: "<plural>.<action>".
const (
	ActionList   = "list"
	ActionView   = "view"
	ActionCreate = "create"
	ActionUpdate = "update"
	ActionDelete = "delete"
)

// AbilityAny is the suffix of the ability that lifts the ownership scope of a
// resource: an actor holding "<plural>.any" sees and writes every row.
const AbilityAny = "any"

var everyAction = []string{ActionList, ActionView, ActionCreate, ActionUpdate, ActionDelete}

// Option shapes a resource at registration. Options are applied in order, at
// boot, before the routes and the GraphQL schema are built.
type Option func(*Resource)

// Hidden keeps columns out of every response, filter, sort and write.
func Hidden(columns ...string) Option {
	return func(resource *Resource) {
		for _, column := range columns {
			resource.Hidden[column] = true
		}
	}
}

// Visible lifts the default hiding of columns whose name looks sensitive
// (phone, address, kyc_*...). It never un-hides a column Hidden named.
func Visible(columns ...string) Option {
	return func(resource *Resource) {
		for _, column := range columns {
			if resource.explicitHidden[column] {
				continue
			}
			delete(resource.Hidden, column)
		}
	}
}

// ReadOnly serves the list and the show actions only.
func ReadOnly() Option { return Actions(ActionList, ActionView) }

// Actions restricts the resource to the actions named. Routes and GraphQL
// fields are only built for those.
func Actions(actions ...string) Option {
	return func(resource *Resource) {
		resource.Actions = map[string]bool{}
		for _, action := range actions {
			resource.Actions[action] = true
		}
	}
}

// Owner scopes every read and write to the rows whose column holds the id of
// the actor, unless the actor holds "<plural>.any". A create stamps the column
// with the actor id, whatever the client sent.
func Owner(column string) Option {
	return func(resource *Resource) { resource.Owner = column }
}

// Guarded columns are shown but only written by actors holding
// "<plural>.any": a role, a verification date, a status a member must not set
// on their own row.
func Guarded(columns ...string) Option {
	return func(resource *Resource) {
		if resource.Guarded == nil {
			resource.Guarded = map[string]bool{}
		}
		for _, column := range columns {
			resource.Guarded[column] = true
		}
	}
}

// RegisterWith publishes a model with options:
//
//	api.RegisterWith[models.Order](registry, "orders", "order",
//		api.Owner("user_id"), api.Actions(api.ActionList, api.ActionView, api.ActionCreate))
func RegisterWith[T any](r *Registry, plural, singular string, options ...Option) *Resource {
	var zero T
	resource := &Resource{
		Plural:   plural,
		Singular: singular,
		Model:    &zero,
		Hidden:   map[string]bool{},
		Fields:   describe(reflect.TypeOf(zero)),
	}
	resource.Table = tableOf(r.db, resource)
	annotate(r, resource)
	hideSensitive(resource, false)
	resource.With(options...)

	r.mu.Lock()
	r.resources = append(r.resources, resource)
	r.mu.Unlock()
	return resource
}

// Register publishes a model with full CRUD; the trailing names are hidden
// columns. It is RegisterWith(..., Hidden(hidden...)).
func Register[T any](r *Registry, plural, singular string, hidden ...string) *Resource {
	return RegisterWith[T](r, plural, singular, Hidden(hidden...))
}

// With applies options to a resource already registered or discovered. It is
// meant for boot time, before Mount and Schema.
func (r *Resource) With(options ...Option) *Resource {
	if r.Hidden == nil {
		r.Hidden = map[string]bool{}
	}
	for _, option := range options {
		if option == nil {
			continue
		}
		snapshot := make(map[string]bool, len(r.Hidden))
		for column := range r.Hidden {
			snapshot[column] = true
		}
		option(r)
		r.recordExplicit(snapshot)
	}
	return r
}

// recordExplicit remembers the columns an option hid, so Visible cannot later
// expose what the author asked to hide.
func (r *Resource) recordExplicit(before map[string]bool) {
	for column := range r.Hidden {
		if before[column] {
			continue
		}
		if r.explicitHidden == nil {
			r.explicitHidden = map[string]bool{}
		}
		r.explicitHidden[column] = true
	}
}

// Configure applies options to the resource published under plural, typically
// a discovered table: registry.Configure("orders", api.Owner("user_id")).
func (r *Registry) Configure(plural string, options ...Option) error {
	resource := r.Find(plural)
	if resource == nil {
		return fmt.Errorf("api: no resource %q to configure", plural)
	}
	resource.With(options...)
	return nil
}

// Allows reports whether the resource serves an action.
func (r *Resource) Allows(action string) bool {
	if r.Actions != nil {
		return r.Actions[action]
	}
	switch action {
	case ActionList, ActionView:
		return true
	case ActionCreate, ActionUpdate, ActionDelete:
		return r.writable()
	}
	return false
}

func (r *Resource) writableColumn(field Field, privileged bool) bool {
	if field.ReadOnly || r.Hidden[field.Name] || r.Hidden[field.Column] || field.Name == deletedAt {
		return false
	}
	if r.Guarded[field.Name] || r.Guarded[field.Column] {
		return privileged
	}
	return true
}
