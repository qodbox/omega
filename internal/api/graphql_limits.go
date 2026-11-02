package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"
)

// GraphQLGuard bounds a GraphQL request before anything executes: one JSON
// object per request (no batch), the query in the body only, and the depth,
// field count, top-level fields (aliases included) and estimated cost of the
// query within the registry limits.
//
// The query is only read from the JSON body: graphql-go/handler would also
// execute ?query= from the URL, which IntrospectionOnly — reading the body —
// would not see.
func (r *Registry) GraphQLGuard() fiber.Handler {
	return func(c fiber.Ctx) error {
		for _, parameter := range []string{"query", "variables", "operationName"} {
			if c.Query(parameter) != "" {
				return graphqlRefusal(c, fiber.StatusBadRequest, "send the query in a JSON body, not in the URL")
			}
		}

		if kind := strings.ToLower(c.Get(fiber.HeaderContentType)); kind != "" && !strings.Contains(kind, "json") {
			return graphqlRefusal(c, fiber.StatusUnsupportedMediaType, "GraphQL requests must be application/json")
		}

		body := bytes.TrimSpace(c.Body())
		if len(body) > 0 && body[0] == '[' {
			return graphqlRefusal(c, fiber.StatusBadRequest, "batched GraphQL requests are not accepted")
		}

		var request struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		if err := json.Unmarshal(body, &request); err != nil {
			return graphqlRefusal(c, fiber.StatusBadRequest, "invalid JSON body")
		}

		if err := r.CheckQuery(request.Query, request.Variables); err != nil {
			return graphqlRefusal(c, fiber.StatusBadRequest, err.Error())
		}
		return c.Next()
	}
}

func graphqlRefusal(c fiber.Ctx, status int, message string) error {
	return c.Status(status).JSON(fiber.Map{"errors": []fiber.Map{{"message": message}}})
}

// CheckQuery measures a query against the registry limits. A query that does
// not parse passes: the executor reports the syntax error itself.
func (r *Registry) CheckQuery(query string, variables map[string]any) error {
	document, err := parser.Parse(parser.ParseParams{
		Source: source.NewSource(&source.Source{Body: []byte(query)}),
	})
	if err != nil {
		return nil
	}

	lists := map[string]bool{}
	for _, resource := range r.All() {
		if resource.Allows(ActionList) {
			lists[resource.Plural] = true
		}
	}

	measure := &measure{
		limits:    r.Limits(),
		fragments: map[string]*ast.FragmentDefinition{},
		lists:     lists,
		variables: variables,
	}
	for _, definition := range document.Definitions {
		if fragment, ok := definition.(*ast.FragmentDefinition); ok && fragment.Name != nil {
			measure.fragments[fragment.Name.Value] = fragment
		}
	}

	for _, definition := range document.Definitions {
		operation, ok := definition.(*ast.OperationDefinition)
		if !ok {
			continue
		}
		roots := measure.rootFields(operation.SelectionSet, map[string]bool{})
		if roots > measure.limits.MaxRootFields {
			return fmt.Errorf("too many top-level fields: %d, at most %d (aliases count)", roots, measure.limits.MaxRootFields)
		}
		if err := measure.walk(operation.SelectionSet, 1, 1, true, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

type measure struct {
	limits    Limits
	fragments map[string]*ast.FragmentDefinition
	lists     map[string]bool
	variables map[string]any

	fields     int
	complexity int
}

func (m *measure) rootFields(set *ast.SelectionSet, visiting map[string]bool) int {
	if set == nil {
		return 0
	}
	count := 0
	for _, selection := range set.Selections {
		switch typed := selection.(type) {
		case *ast.Field:
			count++
		case *ast.InlineFragment:
			count += m.rootFields(typed.SelectionSet, visiting)
		case *ast.FragmentSpread:
			if typed.Name == nil || visiting[typed.Name.Value] {
				continue
			}
			if fragment := m.fragments[typed.Name.Value]; fragment != nil {
				visiting[typed.Name.Value] = true
				count += m.rootFields(fragment.SelectionSet, visiting)
				delete(visiting, typed.Name.Value)
			}
		}
	}
	return count
}

// walk counts fields with fragments expanded, and aborts as soon as a limit is
// crossed, so a fragment bomb cannot make the measure itself expensive.
func (m *measure) walk(set *ast.SelectionSet, depth, multiplier int, root bool, visiting map[string]bool) error {
	if set == nil {
		return nil
	}
	if depth > m.limits.MaxDepth {
		return fmt.Errorf("query is nested deeper than %d levels", m.limits.MaxDepth)
	}

	for _, selection := range set.Selections {
		switch typed := selection.(type) {
		case *ast.Field:
			m.fields++
			m.complexity += multiplier
			if m.fields > m.limits.MaxFields {
				return fmt.Errorf("query selects more than %d fields", m.limits.MaxFields)
			}
			if m.complexity > m.limits.MaxComplexity {
				return fmt.Errorf("query is too expensive: estimated cost above %d", m.limits.MaxComplexity)
			}

			next := multiplier
			if root && typed.Name != nil && m.lists[typed.Name.Value] {
				next = multiplier * m.listSize(typed)
			}
			if err := m.walk(typed.SelectionSet, depth+1, next, false, visiting); err != nil {
				return err
			}

		case *ast.InlineFragment:
			if err := m.walk(typed.SelectionSet, depth, multiplier, root, visiting); err != nil {
				return err
			}

		case *ast.FragmentSpread:
			if typed.Name == nil {
				continue
			}
			name := typed.Name.Value
			if visiting[name] {
				return fmt.Errorf("fragment %s spreads itself", name)
			}
			fragment := m.fragments[name]
			if fragment == nil {
				continue
			}
			visiting[name] = true
			err := m.walk(fragment.SelectionSet, depth, multiplier, root, visiting)
			delete(visiting, name)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// listSize is the number of rows a list field may return: its limit argument,
// a literal or a variable, bounded like the resolver bounds it.
func (m *measure) listSize(field *ast.Field) int {
	size := defaultPerPage
	for _, argument := range field.Arguments {
		if argument.Name == nil || argument.Name.Value != "limit" {
			continue
		}
		switch value := argument.Value.(type) {
		case *ast.IntValue:
			if parsed, err := strconv.Atoi(value.Value); err == nil {
				size = parsed
			}
		case *ast.Variable:
			size = maxPerPage
			if value.Name != nil {
				if number, ok := m.variables[value.Name.Value].(float64); ok {
					size = int(number)
				}
			}
		}
	}
	return min(max(size, 1), maxPerPage)
}
