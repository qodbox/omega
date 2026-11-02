package api

// Limits bounds what one request may ask of the generated endpoints. A zero
// field takes its default.
type Limits struct {
	// REST: filters in one query string, values in one __in, characters in one
	// LIKE pattern, and wildcards in one __like pattern.
	MaxFilters       int
	MaxInValues      int
	MaxPatternLength int
	MaxWildcards     int

	// GraphQL: nesting depth, fields once fragments are expanded, top-level
	// fields (aliases included), and estimated cost — a list counts its limit
	// times the fields it selects.
	MaxDepth      int
	MaxFields     int
	MaxRootFields int
	MaxComplexity int
}

// DefaultLimits are generous for a client and tight for an attacker. The
// GraphQL depth and field count leave room for GraphiQL's introspection query.
var DefaultLimits = Limits{
	MaxFilters:       10,
	MaxInValues:      100,
	MaxPatternLength: 128,
	MaxWildcards:     8,

	MaxDepth:      15,
	MaxFields:     500,
	MaxRootFields: 10,
	MaxComplexity: 10000,
}

func (l Limits) withDefaults() Limits {
	pick := func(value, fallback int) int {
		if value > 0 {
			return value
		}
		return fallback
	}
	return Limits{
		MaxFilters:       pick(l.MaxFilters, DefaultLimits.MaxFilters),
		MaxInValues:      pick(l.MaxInValues, DefaultLimits.MaxInValues),
		MaxPatternLength: pick(l.MaxPatternLength, DefaultLimits.MaxPatternLength),
		MaxWildcards:     pick(l.MaxWildcards, DefaultLimits.MaxWildcards),
		MaxDepth:         pick(l.MaxDepth, DefaultLimits.MaxDepth),
		MaxFields:        pick(l.MaxFields, DefaultLimits.MaxFields),
		MaxRootFields:    pick(l.MaxRootFields, DefaultLimits.MaxRootFields),
		MaxComplexity:    pick(l.MaxComplexity, DefaultLimits.MaxComplexity),
	}
}

// SetLimits replaces the limits of the registry; zero fields keep the default.
func (r *Registry) SetLimits(limits Limits) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.limits = limits.withDefaults()
}

// Limits returns the limits in force.
func (r *Registry) Limits() Limits {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.limits.withDefaults()
}
