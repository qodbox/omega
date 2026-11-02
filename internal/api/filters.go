package api

import (
	"strings"

	"gorm.io/gorm"
)

// reservedParameters are the query string keys that are not filters.
var reservedParameters = map[string]bool{"page": true, "per_page": true, "sort": true, "count": true}

type operator struct {
	suffix string
	clause string
}

// Longest suffixes first, so __gte is never read as __gt.
var operators = []operator{
	{"__contains", ""},
	{"__starts", ""},
	{"__like", ""},
	{"__ends", ""},
	{"__gte", " >= ?"},
	{"__lte", " <= ?"},
	{"__gt", " > ?"},
	{"__lt", " < ?"},
	{"__ne", " <> ?"},
	{"__in", ""},
}

type filter struct {
	column string
	op     string
	value  string
}

func parseFilter(name, value string) filter {
	for _, candidate := range operators {
		if strings.HasSuffix(name, candidate.suffix) {
			return filter{column: strings.TrimSuffix(name, candidate.suffix), op: candidate.suffix, value: value}
		}
	}
	return filter{column: name, value: value}
}

// applyFilters turns the query string into WHERE clauses. Every column must be
// a visible field of the resource; the number of filters, the size of __in and
// the shape of LIKE patterns are bounded by the registry limits.
func applyFilters(query *gorm.DB, resource *Resource, limits Limits, parameters [][2]string) (*gorm.DB, error) {
	count := 0
	for _, parameter := range parameters {
		if reservedParameters[parameter[0]] {
			continue
		}
		count++
		if count > limits.MaxFilters {
			return nil, invalid("too many filters: at most %d per request", limits.MaxFilters)
		}

		f := parseFilter(parameter[0], parameter[1])
		if !resource.filterable(f.column) {
			return nil, invalid("unknown filter %s", f.column)
		}
		column := quote(query, columnOf(resource, f.column))

		switch f.op {
		case "__in":
			values := strings.Split(f.value, ",")
			if len(values) > limits.MaxInValues {
				return nil, invalid("%s__in takes at most %d values", f.column, limits.MaxInValues)
			}
			query = query.Where(column+" IN ?", values)

		case "__like", "__contains", "__starts", "__ends":
			pattern, err := likePattern(f, limits)
			if err != nil {
				return nil, err
			}
			query = query.Where(column+" LIKE ? ESCAPE "+escapeLiteral(query), pattern)

		case "":
			query = query.Where(column+" = ?", f.value)

		default:
			for _, candidate := range operators {
				if candidate.suffix == f.op {
					query = query.Where(column+candidate.clause, f.value)
				}
			}
		}
	}
	return query, nil
}

func columnOf(resource *Resource, name string) string {
	if field, ok := resource.field(name); ok {
		return field.Column
	}
	return name
}

// likePattern builds the pattern of a LIKE filter, with the backslash as escape
// character everywhere.
//
//   - __contains, __starts and __ends take the value literally: %, _ and \ are
//     escaped, then the wildcards the operator implies are added.
//   - __like lets the client write % and _ as wildcards and \% or \_ for the
//     literal characters; a lone trailing backslash, which PostgreSQL rejects
//     and SQLite reads as "no match", is escaped. The number of wildcards is
//     capped: "%a%b%c%..." is a cheap way to make LIKE backtrack.
func likePattern(f filter, limits Limits) (string, error) {
	if len([]rune(f.value)) > limits.MaxPatternLength {
		return "", invalid("%s%s: the pattern is longer than %d characters", f.column, f.op, limits.MaxPatternLength)
	}

	switch f.op {
	case "__contains":
		return "%" + escapeLike(f.value) + "%", nil
	case "__starts":
		return escapeLike(f.value) + "%", nil
	case "__ends":
		return "%" + escapeLike(f.value), nil
	}

	var out strings.Builder
	wildcards := 0
	runes := []rune(f.value)
	for index := 0; index < len(runes); index++ {
		current := runes[index]
		switch current {
		case '\\':
			if index+1 < len(runes) && (runes[index+1] == '%' || runes[index+1] == '_' || runes[index+1] == '\\') {
				out.WriteRune('\\')
				out.WriteRune(runes[index+1])
				index++
				continue
			}
			out.WriteString(`\\`)
		case '%', '_':
			wildcards++
			out.WriteRune(current)
		default:
			out.WriteRune(current)
		}
	}
	if wildcards > limits.MaxWildcards {
		return "", invalid("%s__like: at most %d wildcards", f.column, limits.MaxWildcards)
	}
	return out.String(), nil
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func escapeLike(value string) string { return likeEscaper.Replace(value) }

// escapeLiteral is the SQL literal of a single backslash: MySQL reads a
// backslash inside a string literal as an escape, the others do not.
func escapeLiteral(query *gorm.DB) string {
	if dialect(query) == "mysql" {
		return `'\\'`
	}
	return `'\'`
}
