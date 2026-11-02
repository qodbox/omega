package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"
)

// Schema builds the GraphQL schema of the registry. Every resolver goes through
// the same checks as the REST handlers — begin, scope, fetch — so the two
// transports answer the same question the same way: abilities, ownership,
// hidden columns, write validation.
func (r *Registry) Schema() (graphql.Schema, error) {
	queries := graphql.Fields{}
	mutations := graphql.Fields{}

	for _, resource := range r.All() {
		resource := resource
		object := graphql.NewObject(graphql.ObjectConfig{
			Name:   upperFirst(resource.Singular),
			Fields: r.objectFields(resource),
		})

		if resource.Allows(ActionList) {
			queries[resource.Plural] = &graphql.Field{
				Type: graphql.NewList(object),
				Args: graphql.FieldConfigArgument{
					"limit":  &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: defaultPerPage},
					"offset": &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 0},
					"sort":   &graphql.ArgumentConfig{Type: graphql.String},
				},
				Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveList(resource, p) },
			}
			queries[resource.Plural+"_count"] = &graphql.Field{
				Type:    graphql.Int,
				Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveCount(resource, p) },
			}
		}

		if resource.Allows(ActionView) {
			queries[resource.Singular] = &graphql.Field{
				Type:    object,
				Args:    graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}},
				Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveOne(resource, p) },
			}
		}

		input := graphql.FieldConfigArgument{}
		for _, field := range resource.Fields {
			if field.ReadOnly || resource.Hidden[field.Name] || field.Name == deletedAt {
				continue
			}
			input[field.Name] = &graphql.ArgumentConfig{Type: scalarOf(field)}
		}

		if resource.Allows(ActionCreate) {
			mutations["create_"+resource.Singular] = &graphql.Field{
				Type: object, Args: input,
				Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveCreate(resource, p) },
			}
		}
		if resource.Allows(ActionUpdate) {
			mutations["update_"+resource.Singular] = &graphql.Field{
				Type:    object,
				Args:    withID(input),
				Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveUpdate(resource, p) },
			}
		}
		if resource.Allows(ActionDelete) {
			mutations["delete_"+resource.Singular] = &graphql.Field{
				Type:    graphql.Boolean,
				Args:    graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}},
				Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveDelete(resource, p) },
			}
		}
	}

	config := graphql.SchemaConfig{
		Query: graphql.NewObject(graphql.ObjectConfig{Name: "Query", Fields: queries}),
	}
	if len(mutations) > 0 {
		config.Mutation = graphql.NewObject(graphql.ObjectConfig{Name: "Mutation", Fields: mutations})
	}
	return graphql.NewSchema(config)
}

// graphqlBegin is restBegin for the resolvers: the same checks, strict when no
// gate is configured, and the error texts GraphQL clients already know.
func (r *Registry) graphqlBegin(ctx context.Context, resource *Resource, action string) (*access, error) {
	granted, err := r.begin(ctx, ctx, resource, action, true)
	if errors.Is(err, errUnauthenticated) {
		return nil, errors.New("graphql: authentication required")
	}
	return granted, err
}

func (r *Registry) resolveList(resource *Resource, p graphql.ResolveParams) (any, error) {
	granted, err := r.graphqlBegin(p.Context, resource, ActionList)
	if err != nil {
		return nil, err
	}

	sort, _ := p.Args["sort"].(string)
	query, err := sorted(r.scope(p.Context, resource, granted), resource, sort)
	if err != nil {
		return nil, err
	}

	limit, _ := p.Args["limit"].(int)
	offset, _ := p.Args["offset"].(int)

	rows := []map[string]any{}
	if err := readable(query, resource).Limit(min(max(limit, 1), maxPerPage)).Offset(max(offset, 0)).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		resource.serialise(row)
	}
	return rows, nil
}

func (r *Registry) resolveCount(resource *Resource, p graphql.ResolveParams) (any, error) {
	granted, err := r.graphqlBegin(p.Context, resource, ActionList)
	if err != nil {
		return nil, err
	}
	var total int64
	err = r.scope(p.Context, resource, granted).Count(&total).Error
	return int(total), err
}

func (r *Registry) resolveOne(resource *Resource, p graphql.ResolveParams) (any, error) {
	granted, err := r.graphqlBegin(p.Context, resource, ActionView)
	if err != nil {
		return nil, err
	}

	row, err := r.fetch(p.Context, resource, granted, ActionView, p.Args["id"])
	if errors.Is(err, errNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return resource.serialise(row), nil
}

func (r *Registry) resolveCreate(resource *Resource, p graphql.ResolveParams) (any, error) {
	granted, err := r.graphqlBegin(p.Context, resource, ActionCreate)
	if err != nil {
		return nil, err
	}

	raw := arguments(resource, p, false)
	if len(raw) == 0 {
		return nil, fmt.Errorf("nothing to create")
	}
	payload, err := resource.payload(p.Context, granted, raw, false, r.exactDigits())
	if err != nil {
		return nil, err
	}

	if err := r.insert(p.Context, resource, payload); err != nil {
		return nil, err
	}

	row, err := r.load(p.Context, resource, granted, insertedID(payload))
	if err != nil {
		return nil, err
	}
	return resource.serialise(row), nil
}

func (r *Registry) resolveUpdate(resource *Resource, p graphql.ResolveParams) (any, error) {
	granted, err := r.graphqlBegin(p.Context, resource, ActionUpdate)
	if err != nil {
		return nil, err
	}

	payload, err := resource.payload(p.Context, granted, arguments(resource, p, true), true, r.exactDigits())
	if err != nil {
		return nil, err
	}

	id := p.Args["id"]
	if _, err := r.fetch(p.Context, resource, granted, ActionUpdate, id); err != nil {
		if errors.Is(err, errNotFound) {
			return nil, nil
		}
		return nil, err
	}

	affected, err := r.change(p.Context, resource, granted, id, payload)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		return nil, nil
	}

	row, err := r.load(p.Context, resource, granted, id)
	if err != nil {
		return nil, err
	}
	return resource.serialise(row), nil
}

func (r *Registry) resolveDelete(resource *Resource, p graphql.ResolveParams) (any, error) {
	granted, err := r.graphqlBegin(p.Context, resource, ActionDelete)
	if err != nil {
		return nil, err
	}

	id := p.Args["id"]
	if _, err := r.fetch(p.Context, resource, granted, ActionDelete, id); err != nil {
		if errors.Is(err, errNotFound) {
			return false, nil
		}
		return nil, err
	}

	affected, err := r.remove(p.Context, resource, granted, id)
	return affected > 0, err
}

// arguments collects the arguments that name a field; payload decides which
// of them may be written.
func arguments(resource *Resource, p graphql.ResolveParams, skipID bool) map[string]any {
	raw := map[string]any{}
	for _, field := range resource.Fields {
		if skipID && field.Name == "id" {
			continue
		}
		if value, ok := p.Args[field.Name]; ok {
			raw[field.Name] = value
		}
	}
	return raw
}

func upperFirst(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func (r *Registry) objectFields(resource *Resource) graphql.Fields {
	fields := graphql.Fields{}
	for _, field := range resource.Fields {
		if resource.Hidden[field.Name] || field.Name == deletedAt {
			continue
		}
		name := field.Name
		fields[name] = &graphql.Field{
			Type: scalarOf(field),
			Resolve: func(p graphql.ResolveParams) (any, error) {
				row, ok := p.Source.(map[string]any)
				if !ok {
					return nil, nil
				}
				return stringify(row[name]), nil
			},
		}
	}
	return fields
}

func scalarOf(field Field) graphql.Output {
	if field.Decimal {
		return graphql.String
	}
	switch field.Type {
	case "integer":
		return graphql.Int
	case "number":
		return graphql.Float
	case "boolean":
		return graphql.Boolean
	default:
		return graphql.String
	}
}

func stringify(value any) any {
	switch typed := value.(type) {
	case []byte:
		return string(typed)
	case nil:
		return nil
	default:
		return value
	}
}

func withID(args graphql.FieldConfigArgument) graphql.FieldConfigArgument {
	out := graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}}
	for name, arg := range args {
		out[name] = arg
	}
	return out
}
