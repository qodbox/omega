package api

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	defaultPerPage = 25
	maxPerPage     = 200
)

// registryLocal marks a request as served by a registry route, so Authorize
// knows the handler will ask the policies itself, with the row as subject.
type registryLocal struct{}

func (r *Registry) Mount(group fiber.Router, middleware ...fiber.Handler) {
	mark := func(c fiber.Ctx) error {
		c.Locals(registryLocal{}, r)
		return c.Next()
	}

	route := func(add func(string, any, ...any) fiber.Router, path string, handler fiber.Handler) fiber.Router {
		chain := make([]any, 0, len(middleware)+1)
		for _, m := range middleware {
			chain = append(chain, m)
		}
		chain = append(chain, handler)
		return add(path, mark, chain...)
	}

	for _, resource := range r.All() {
		resource := resource
		if resource.Allows(ActionList) {
			route(group.Get, "/"+resource.Plural, func(c fiber.Ctx) error { return r.list(c, resource) }).
				Name("api." + resource.Plural + ".index")
		}
		if resource.Allows(ActionView) {
			route(group.Get, "/"+resource.Plural+"/:id", func(c fiber.Ctx) error { return r.show(c, resource) }).
				Name("api." + resource.Plural + ".show")
		}
		if resource.Allows(ActionCreate) {
			route(group.Post, "/"+resource.Plural, func(c fiber.Ctx) error { return r.create(c, resource) }).
				Name("api." + resource.Plural + ".store")
		}
		if resource.Allows(ActionUpdate) {
			route(group.Patch, "/"+resource.Plural+"/:id", func(c fiber.Ctx) error { return r.update(c, resource) }).
				Name("api." + resource.Plural + ".update")
		}
		if resource.Allows(ActionDelete) {
			route(group.Delete, "/"+resource.Plural+"/:id", func(c fiber.Ctx) error { return r.destroy(c, resource) }).
				Name("api." + resource.Plural + ".destroy")
		}
	}
}

// gated reports whether a policy gate is configured.
func (r *Registry) gated() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.gate != nil && r.actorFrom != nil
}

// restBegin resolves the caller of a REST route. The actor travels in fiber's
// Locals, which are fasthttp user values: the RequestCtx is the context that
// carries them, the same one the GraphQL adaptor hands to the resolvers.
func (r *Registry) restBegin(c fiber.Ctx, resource *Resource, action string) (*access, error) {
	granted, err := r.begin(c.Context(), c.RequestCtx(), resource, action, false)
	return granted, restError(err)
}

func restError(err error) error {
	if err == nil {
		return nil
	}
	var forbidden forbiddenError
	var rejected invalidError
	switch {
	case errors.Is(err, errUnauthenticated):
		return fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
	case errors.Is(err, errNotFound):
		return fiber.ErrNotFound
	case errors.Is(err, errInvalidJSON):
		return fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	case errors.As(err, &forbidden):
		return fiber.NewError(fiber.StatusForbidden, "You may not "+forbidden.ability+".")
	case errors.As(err, &rejected):
		return fiber.NewError(fiber.StatusUnprocessableEntity, rejected.message)
	}
	return err
}

func queryParameters(c fiber.Ctx) [][2]string {
	parameters := [][2]string{}
	for key, value := range c.RequestCtx().QueryArgs().All() {
		parameters = append(parameters, [2]string{string(key), string(value)})
	}
	return parameters
}

func (r *Registry) list(c fiber.Ctx, resource *Resource) error {
	granted, err := r.restBegin(c, resource, ActionList)
	if err != nil {
		return err
	}

	query, err := applyFilters(r.scope(c.Context(), resource, granted), resource, r.Limits(), queryParameters(c))
	if err != nil {
		return restError(err)
	}

	counted := c.Query("count") != "false"
	var total int64
	if counted {
		if err := query.Count(&total).Error; err != nil {
			return err
		}
	}

	page := max(fiber.Query[int](c, "page", 1), 1)
	perPage := fiber.Query[int](c, "per_page", defaultPerPage)
	perPage = min(max(perPage, 1), maxPerPage)

	query, err = sorted(query, resource, c.Query("sort"))
	if err != nil {
		return restError(err)
	}

	rows := []map[string]any{}
	if err := readable(query, resource).Limit(perPage).Offset((page - 1) * perPage).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		resource.serialise(row)
	}

	meta := fiber.Map{"page": page, "per_page": perPage}
	if counted {
		meta["total"] = total
		meta["last_page"] = lastPage(total, perPage)
	}
	return c.JSON(fiber.Map{"data": rows, "meta": meta})
}

// sorted orders on a visible column: sorting on a hidden one would leak its
// order, a classic oracle on hashes and secrets.
func sorted(query *gorm.DB, resource *Resource, sort string) (*gorm.DB, error) {
	if sort == "" {
		return query, nil
	}
	column := strings.TrimPrefix(sort, "-")
	if !resource.filterable(column) {
		return nil, invalid("unknown sort column %s", column)
	}
	direction := " asc"
	if strings.HasPrefix(sort, "-") {
		direction = " desc"
	}
	return query.Order(quote(query, columnOf(resource, column)) + direction), nil
}

// fetch loads a row the caller may reach and asks the policies about it. A row
// outside the actor's scope is a 404, never a 403: its existence is not theirs
// to learn.
func (r *Registry) fetch(ctx context.Context, resource *Resource, granted *access, action string, id any) (map[string]any, error) {
	row, err := r.load(ctx, resource, granted, id)
	if err != nil {
		return nil, err
	}
	if err := r.authorizeRow(ctx, resource, granted, action, row); err != nil {
		return nil, err
	}
	return row, nil
}

func (r *Registry) show(c fiber.Ctx, resource *Resource) error {
	granted, err := r.restBegin(c, resource, ActionView)
	if err != nil {
		return err
	}
	row, err := r.fetch(c.Context(), resource, granted, ActionView, c.Params("id"))
	if err != nil {
		return restError(err)
	}
	return c.JSON(fiber.Map{"data": resource.serialise(row)})
}

func (r *Registry) create(c fiber.Ctx, resource *Resource) error {
	granted, err := r.restBegin(c, resource, ActionCreate)
	if err != nil {
		return err
	}
	raw, err := decodeBody(c.Body())
	if err != nil {
		return restError(err)
	}
	payload, err := resource.payload(c.Context(), granted, raw, false, r.exactDigits())
	if err != nil {
		return restError(err)
	}

	if err := r.insert(c.Context(), resource, payload); err != nil {
		return writeRejected(c.Context(), resource, err)
	}

	// A table without an id column cannot be read back: the row is written, the
	// answer is a 404 as it has always been.
	row, err := r.load(c.Context(), resource, granted, insertedID(payload))
	if err != nil {
		return fiber.ErrNotFound
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": resource.serialise(row)})
}

func (r *Registry) insert(ctx context.Context, resource *Resource, payload map[string]any) error {
	now := time.Now()
	for _, column := range []string{createdAt, updatedAt} {
		if resource.hasColumn(column) {
			payload[column] = now
		}
	}
	query := r.db.WithContext(ctx).Table(tableOf(r.db, resource))
	if dialect(query) == "postgres" && resource.hasColumn("id") {
		// Without a model gorm cannot know the key: PostgreSQL has no
		// LastInsertId, so the id is asked back explicitly.
		query = query.Clauses(clause.Returning{Columns: []clause.Column{{Name: "id"}}})
	}
	return query.Create(payload).Error
}

func (r *Registry) update(c fiber.Ctx, resource *Resource) error {
	granted, err := r.restBegin(c, resource, ActionUpdate)
	if err != nil {
		return err
	}
	raw, err := decodeBody(c.Body())
	if err != nil {
		return restError(err)
	}
	payload, err := resource.payload(c.Context(), granted, raw, true, r.exactDigits())
	if err != nil {
		return restError(err)
	}

	id := c.Params("id")
	if _, err := r.fetch(c.Context(), resource, granted, ActionUpdate, id); err != nil {
		return restError(err)
	}

	affected, err := r.change(c.Context(), resource, granted, id, payload)
	if err != nil {
		return writeRejected(c.Context(), resource, err)
	}
	if affected == 0 {
		return fiber.ErrNotFound
	}

	row, err := r.load(c.Context(), resource, granted, id)
	if err != nil {
		return restError(err)
	}
	return c.JSON(fiber.Map{"data": resource.serialise(row)})
}

func (r *Registry) change(ctx context.Context, resource *Resource, granted *access, id any, payload map[string]any) (int64, error) {
	if resource.hasColumn(updatedAt) {
		payload[updatedAt] = time.Now()
	}
	result := r.scope(ctx, resource, granted).Where("id = ?", id).Updates(payload)
	return result.RowsAffected, result.Error
}

func (r *Registry) destroy(c fiber.Ctx, resource *Resource) error {
	granted, err := r.restBegin(c, resource, ActionDelete)
	if err != nil {
		return err
	}

	id := c.Params("id")
	if _, err := r.fetch(c.Context(), resource, granted, ActionDelete, id); err != nil {
		return restError(err)
	}

	affected, err := r.remove(c.Context(), resource, granted, id)
	if err != nil {
		return err
	}
	if affected == 0 {
		return fiber.ErrNotFound
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (r *Registry) remove(ctx context.Context, resource *Resource, granted *access, id any) (int64, error) {
	query := r.scope(ctx, resource, granted).Where("id = ?", id)

	var result *gorm.DB
	if resource.softDeletes() {
		result = query.Updates(map[string]any{deletedAt: time.Now()})
	} else {
		result = query.Delete(nil)
	}
	return result.RowsAffected, result.Error
}

func writeRejected(ctx context.Context, resource *Resource, err error) error {
	log.Ctx(ctx).Error().
		Err(err).
		Str("resource", resource.Plural).
		Msg("api: the database refused the write")

	return fiber.NewError(fiber.StatusUnprocessableEntity,
		"The database refused this "+resource.Singular+".")
}

func insertedID(payload map[string]any) any {
	for _, key := range []string{"@id", "id"} {
		if value, ok := payload[key]; ok {
			return value
		}
	}
	return nil
}

func tableOf(db *gorm.DB, resource *Resource) string {
	if resource.Table != "" {
		return resource.Table
	}

	if resource.Model == nil {
		return resource.Plural
	}
	statement := &gorm.Statement{DB: db}
	if err := statement.Parse(resource.Model); err != nil {
		return resource.Plural
	}
	return statement.Table
}

func lastPage(total int64, perPage int) int64 {
	if total == 0 {
		return 1
	}
	pages := total / int64(perPage)
	if total%int64(perPage) != 0 {
		pages++
	}
	return pages
}
