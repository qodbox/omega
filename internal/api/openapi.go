package api

func (r *Registry) OpenAPI(title, version, server string) Spec {
	paths := map[string]PathItem{}
	schemas := map[string]*Schema{}

	for _, resource := range r.All() {
		schemas[resource.Singular] = r.schema(resource, false)
		schemas[resource.Singular+"Input"] = r.schema(resource, true)

		collection := r.collectionPath(resource)
		if !resource.Allows(ActionList) {
			collection.Get = nil
		}
		if !resource.Allows(ActionCreate) {
			collection.Post = nil
		}
		if collection.Get != nil || collection.Post != nil {
			paths["/"+resource.Plural] = collection
		}

		item := r.itemPath(resource)
		if !resource.Allows(ActionView) {
			item.Get = nil
		}
		if !resource.Allows(ActionUpdate) {
			item.Patch = nil
		}
		if !resource.Allows(ActionDelete) {
			item.Delete = nil
		}
		if item.Get != nil || item.Patch != nil || item.Delete != nil {
			paths["/"+resource.Plural+"/{id}"] = item
		}
	}

	r.mu.RLock()
	for path, item := range r.extra {
		paths[path] = item
	}
	r.mu.RUnlock()

	paths["/health"] = PathItem{
		Get: (&Operation{
			Tags:        []string{"system"},
			Summary:     "Liveness and database probe",
			OperationID: "health",
			Responses: map[string]Response{
				"200": JSON("The service is up", Object(map[string]*Schema{
					"status":   String(),
					"version":  String(),
					"database": String(),
				})),
				"503": JSON("The database is unreachable", Object(map[string]*Schema{
					"status":   String(),
					"database": String(),
				})),
			},
		}).Public(),
	}

	return Spec{
		OpenAPI: "3.1.0",
		Info: Info{
			Title:       title,
			Version:     version,
			Description: "Generated at runtime from the registered models.",
		},
		Servers: []Server{{URL: server}},
		Paths:   paths,
		Components: Components{
			Schemas: schemas,
			SecuritySchemes: map[string]SecurityScheme{
				"bearerAuth": {
					Type:         "http",
					Scheme:       "bearer",
					BearerFormat: "JWT",
					Description:  "The access_token returned by /auth/login, /auth/register or /auth/refresh.",
				},
			},
		},
		Security: []SecurityRequirement{{"bearerAuth": {}}},
	}
}

func (r *Registry) collectionPath(resource *Resource) PathItem {
	page := Object(map[string]*Schema{
		"data": Array(Ref(resource.Singular)),
		"meta": Object(map[string]*Schema{
			"total":     Integer(),
			"page":      Integer(),
			"per_page":  Integer(),
			"last_page": Integer(),
		}),
	})

	return PathItem{
		Get: &Operation{
			Tags:    []string{resource.Plural},
			Summary: "List " + resource.Plural,
			Description: "Filter on any visible column: `?name=Ada`. Operators are suffixes — " +
				"`__contains`, `__starts`, `__ends` (literal text), `__like` (% and _ are wildcards, " +
				"\\ escapes them), `__gt`, `__gte`, `__lt`, `__lte`, `__ne`, `__in` (comma separated, " +
				"at most 100 values). At most 10 filters per request. `?count=false` skips the total.",
			OperationID: resource.Plural + ".index",
			Parameters:  r.listParameters(resource),
			Responses: map[string]Response{
				"200": JSON("A page of "+resource.Plural, page),
				"401": Failure("Authentication required"),
			},
		},
		Post: &Operation{
			Tags:        []string{resource.Plural},
			Summary:     "Create a " + resource.Singular,
			OperationID: resource.Plural + ".store",
			RequestBody: Body(Ref(resource.Singular + "Input")),
			Responses: map[string]Response{
				"201": JSON("The created "+resource.Singular, wrap(resource.Singular)),
				"401": Failure("Authentication required"),
				"422": Failure("Validation failed"),
			},
		},
	}
}

func (r *Registry) itemPath(resource *Resource) PathItem {
	return PathItem{
		Parameters: []Parameter{PathParameter("id", "", String())},
		Get: &Operation{
			Tags:        []string{resource.Plural},
			Summary:     "Read a " + resource.Singular,
			OperationID: resource.Plural + ".show",
			Responses: map[string]Response{
				"200": JSON("The "+resource.Singular, wrap(resource.Singular)),
				"404": Failure("Not found"),
			},
		},
		Patch: &Operation{
			Tags:        []string{resource.Plural},
			Summary:     "Update a " + resource.Singular,
			OperationID: resource.Plural + ".update",
			RequestBody: Body(Ref(resource.Singular + "Input")),
			Responses: map[string]Response{
				"200": JSON("The updated "+resource.Singular, wrap(resource.Singular)),
				"404": Failure("Not found"),
				"422": Failure("Validation failed"),
			},
		},
		Delete: &Operation{
			Tags:        []string{resource.Plural},
			Summary:     "Delete a " + resource.Singular,
			OperationID: resource.Plural + ".destroy",
			Responses: map[string]Response{
				"204": Empty("Deleted"),
				"404": Failure("Not found"),
			},
		},
	}
}

func (r *Registry) schema(resource *Resource, input bool) *Schema {
	properties := map[string]*Schema{}

	for _, field := range resource.Fields {
		if resource.Hidden[field.Name] || field.Name == deletedAt {
			continue
		}
		if input && field.ReadOnly {
			continue
		}
		properties[field.Name] = &Schema{
			Type:     field.Type,
			Format:   field.Format,
			ReadOnly: field.ReadOnly,
		}
	}
	return Object(properties)
}

func (r *Registry) listParameters(resource *Resource) []Parameter {
	parameters := []Parameter{
		QueryParameter("page", "Page number, from 1", Integer()),
		QueryParameter("per_page", "Rows per page, up to 200", Integer()),
		QueryParameter("sort", "Column to sort on, - for descending", String()),
		QueryParameter("count", "false omits meta.total and meta.last_page (no COUNT query)", &Schema{Type: "boolean"}),
	}

	for _, field := range resource.Fields {
		if !resource.filterable(field.Name) {
			continue
		}
		parameters = append(parameters, QueryParameter(
			field.Name, "Filter on "+field.Name, &Schema{Type: field.Type},
		))
	}
	return parameters
}

func wrap(name string) *Schema {
	return Object(map[string]*Schema{"data": Ref(name)})
}
