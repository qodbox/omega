package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/graphql-go/graphql"
	"gorm.io/gorm"
)

func (r *Registry) Schema() (graphql.Schema, error) {
	queries := graphql.Fields{}
	mutations := graphql.Fields{}

	for _, resource := range r.All() {
		resource := resource
		object := graphql.NewObject(graphql.ObjectConfig{
			Name:   upperFirst(resource.Singular),
			Fields: r.objectFields(resource),
		})

		queries[resource.Plural] = &graphql.Field{
			Type: graphql.NewList(object),
			Args: graphql.FieldConfigArgument{
				"limit":  &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: defaultPerPage},
				"offset": &graphql.ArgumentConfig{Type: graphql.Int, DefaultValue: 0},
				"sort":   &graphql.ArgumentConfig{Type: graphql.String},
			},
			Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveList(resource, p) },
		}

		queries[resource.Singular] = &graphql.Field{
			Type:    object,
			Args:    graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}},
			Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveOne(resource, p) },
		}

		queries[resource.Plural+"_count"] = &graphql.Field{
			Type: graphql.Int,
			Resolve: func(p graphql.ResolveParams) (any, error) {
				var total int64
				err := r.scoped(resource).Count(&total).Error
				return int(total), err
			},
		}

		if !resource.writable() {
			continue
		}

		input := graphql.FieldConfigArgument{}
		for _, field := range resource.Fields {
			if field.ReadOnly || resource.Hidden[field.Name] {
				continue
			}
			input[field.Name] = &graphql.ArgumentConfig{Type: scalarOf(field)}
		}

		mutations["create_"+resource.Singular] = &graphql.Field{
			Type: object, Args: input,
			Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveCreate(resource, p) },
		}
		mutations["update_"+resource.Singular] = &graphql.Field{
			Type:    object,
			Args:    withID(input),
			Resolve: func(p graphql.ResolveParams) (any, error) { return r.resolveUpdate(resource, p) },
		}
		mutations["delete_"+resource.Singular] = &graphql.Field{
			Type: graphql.Boolean,
			Args: graphql.FieldConfigArgument{"id": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)}},
			Resolve: func(p graphql.ResolveParams) (any, error) {
				result := r.deleteRow(resource, p.Args["id"])
				return result.RowsAffected > 0, result.Error
			},
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

func (r *Registry) scoped(resource *Resource) *gorm.DB {
	query := r.db.Table(tableOf(r.db, resource))
	if resource.softDeletes() {
		query = query.Where(deletedAt + " IS NULL")
	}
	return query
}

func (r *Registry) deleteRow(resource *Resource, id any) *gorm.DB {
	query := r.scoped(resource).Where("id = ?", id)
	if resource.softDeletes() {
		return query.Updates(map[string]any{deletedAt: time.Now()})
	}
	return query.Delete(nil)
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

func (r *Registry) resolveList(resource *Resource, p graphql.ResolveParams) (any, error) {
	query := r.scoped(resource)

	if sort, ok := p.Args["sort"].(string); ok && sort != "" {
		column := strings.TrimPrefix(sort, "-")
		if !resource.has(column) {
			return nil, fmt.Errorf("unknown sort column %s", column)
		}
		if strings.HasPrefix(sort, "-") {
			column += " desc"
		}
		query = query.Order(column)
	}

	limit, _ := p.Args["limit"].(int)
	offset, _ := p.Args["offset"].(int)

	rows := []map[string]any{}
	if err := query.Limit(min(max(limit, 1), maxPerPage)).Offset(max(offset, 0)).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		resource.serialise(row)
	}
	return rows, nil
}

func (r *Registry) resolveOne(resource *Resource, p graphql.ResolveParams) (any, error) {
	row := map[string]any{}
	if err := r.scoped(resource).Where("id = ?", p.Args["id"]).Take(&row).Error; err != nil {
		return nil, nil
	}
	return resource.serialise(row), nil
}

func (r *Registry) resolveCreate(resource *Resource, p graphql.ResolveParams) (any, error) {
	payload := arguments(resource, p, false)
	if len(payload) == 0 {
		return nil, fmt.Errorf("nothing to create")
	}

	now := time.Now()
	for _, column := range []string{createdAt, updatedAt} {
		if resource.hasColumn(column) {
			payload[column] = now
		}
	}

	if err := r.db.Table(tableOf(r.db, resource)).Create(payload).Error; err != nil {
		return nil, err
	}

	row := map[string]any{}
	if err := r.scoped(resource).Where("id = ?", insertedID(payload)).Take(&row).Error; err != nil {
		return nil, err
	}
	return resource.serialise(row), nil
}

func (r *Registry) resolveUpdate(resource *Resource, p graphql.ResolveParams) (any, error) {
	payload := arguments(resource, p, true)
	if resource.hasColumn(updatedAt) {
		payload[updatedAt] = time.Now()
	}

	result := r.scoped(resource).Where("id = ?", p.Args["id"]).Updates(payload)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}
	return r.resolveOne(resource, p)
}

func arguments(resource *Resource, p graphql.ResolveParams, skipID bool) map[string]any {
	payload := map[string]any{}
	for _, field := range resource.Fields {
		if field.ReadOnly || resource.Hidden[field.Name] {
			continue
		}
		if skipID && field.Name == "id" {
			continue
		}
		if value, ok := p.Args[field.Name]; ok {
			payload[field.Column] = value
		}
	}
	return payload
}
