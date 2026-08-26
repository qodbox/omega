package policy

import (
	"context"
	"reflect"
	"sort"
	"strings"
	"sync"
)

type Rule func(ctx context.Context, actor any, subject any) bool

type Before func(ctx context.Context, actor any, ability string) (bool, bool)

type Registry struct {
	mu       sync.RWMutex
	rules    map[string]Rule
	grants   map[string]map[string]bool
	wildcard map[string]bool
	before   []Before
}

func New() *Registry {
	return &Registry{
		rules:    map[string]Rule{},
		grants:   map[string]map[string]bool{},
		wildcard: map[string]bool{},
	}
}

func (r *Registry) Define(ability string, rule Rule) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rules[ability] = rule
	return r
}

func (r *Registry) Grant(role string, abilities ...string) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.grants[role] == nil {
		r.grants[role] = map[string]bool{}
	}
	for _, ability := range abilities {
		if ability == "*" {
			r.wildcard[role] = true
			continue
		}
		r.grants[role][ability] = true
	}
	return r
}

func (r *Registry) Before(hook Before) *Registry {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.before = append(r.before, hook)
	return r
}

func (r *Registry) Allows(ctx context.Context, actor any, ability string, subject ...any) bool {
	if actor == nil {
		return false
	}

	r.mu.RLock()
	hooks := append([]Before(nil), r.before...)
	rule, hasRule := r.rules[ability]
	r.mu.RUnlock()

	for _, hook := range hooks {
		if decided, allowed := hook(ctx, actor, ability); decided {
			return allowed
		}
	}

	if hasRule {
		var target any
		if len(subject) > 0 {
			target = subject[0]
		}
		return rule(ctx, actor, target)
	}

	return r.granted(actor, ability)
}

func (r *Registry) Denies(ctx context.Context, actor any, ability string, subject ...any) bool {
	return !r.Allows(ctx, actor, ability, subject...)
}

func (r *Registry) granted(actor any, ability string) bool {
	role := roleOf(actor)
	if role == "" {
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.wildcard[role] {
		return true
	}
	if r.grants[role][ability] {
		return true
	}

	resource, _, found := strings.Cut(ability, ".")
	return found && r.grants[role][resource+".*"]
}

func (r *Registry) Abilities(role string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.wildcard[role] {
		return []string{"*"}
	}

	abilities := make([]string, 0, len(r.grants[role]))
	for ability := range r.grants[role] {
		abilities = append(abilities, ability)
	}
	sort.Strings(abilities)
	return abilities
}

func (r *Registry) Roles() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := map[string]bool{}
	for role := range r.grants {
		seen[role] = true
	}
	for role := range r.wildcard {
		seen[role] = true
	}

	roles := make([]string, 0, len(seen))
	for role := range seen {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles
}

type carrier interface{ Role() string }

func roleOf(actor any) string {
	if typed, ok := actor.(carrier); ok {
		return typed.Role()
	}

	value := reflect.ValueOf(actor)
	for value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return ""
	}

	field := value.FieldByName("Role")
	if field.IsValid() && field.Kind() == reflect.String {
		return field.String()
	}
	return ""
}
