package api

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

const (
	defaultPerPage = 25
	maxPerPage     = 200
)

func (r *Registry) Mount(group fiber.Router, middleware ...fiber.Handler) {
	chain := func(handler fiber.Handler) []fiber.Handler {
		return append(append([]fiber.Handler{}, middleware...), handler)
	}

	for _, resource := range r.All() {
		resource := resource
		group.Get("/"+resource.Plural, chain(func(c *fiber.Ctx) error { return r.list(c, resource) })...).
			Name("api." + resource.Plural + ".index")
		group.Get("/"+resource.Plural+"/:id", chain(func(c *fiber.Ctx) error { return r.show(c, resource) })...).
			Name("api." + resource.Plural + ".show")

		if !resource.writable() {
			continue
		}

		group.Post("/"+resource.Plural, chain(func(c *fiber.Ctx) error { return r.create(c, resource) })...).
			Name("api." + resource.Plural + ".store")
		group.Patch("/"+resource.Plural+"/:id", chain(func(c *fiber.Ctx) error { return r.update(c, resource) })...).
			Name("api." + resource.Plural + ".update")
		group.Delete("/"+resource.Plural+"/:id", chain(func(c *fiber.Ctx) error { return r.destroy(c, resource) })...).
			Name("api." + resource.Plural + ".destroy")
	}
}

func (r *Registry) query(c *fiber.Ctx, resource *Resource) *gorm.DB {
	query := r.db.WithContext(c.UserContext()).Table(tableOf(r.db, resource))
	if resource.softDeletes() {
		query = query.Where(deletedAt + " IS NULL")
	}
	return query
}

func (r *Registry) list(c *fiber.Ctx, resource *Resource) error {
	query, err := applyFilters(r.query(c, resource), resource, c)
	if err != nil {
		return err
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return err
	}

	page := max(c.QueryInt("page", 1), 1)
	perPage := c.QueryInt("per_page", defaultPerPage)
	perPage = min(max(perPage, 1), maxPerPage)

	if sort := c.Query("sort"); sort != "" {
		column := strings.TrimPrefix(sort, "-")
		if !resource.has(column) {
			return fiber.NewError(fiber.StatusUnprocessableEntity, "unknown sort column "+column)
		}
		direction := " asc"
		if strings.HasPrefix(sort, "-") {
			direction = " desc"
		}
		query = query.Order(column + direction)
	}

	rows := []map[string]any{}
	if err := query.Limit(perPage).Offset((page - 1) * perPage).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		resource.serialise(row)
	}

	return c.JSON(fiber.Map{
		"data": rows,
		"meta": fiber.Map{
			"total":     total,
			"page":      page,
			"per_page":  perPage,
			"last_page": lastPage(total, perPage),
		},
	})
}

func (r *Registry) show(c *fiber.Ctx, resource *Resource) error {
	row, err := r.find(c, resource, c.Params("id"))
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": row})
}

func (r *Registry) find(c *fiber.Ctx, resource *Resource, id any) (map[string]any, error) {
	row := map[string]any{}
	if err := r.query(c, resource).Where("id = ?", id).Take(&row).Error; err != nil {
		return nil, fiber.ErrNotFound
	}
	return resource.serialise(row), nil
}

func (r *Registry) create(c *fiber.Ctx, resource *Resource) error {
	payload, err := body(c, resource, false)
	if err != nil {
		return err
	}

	now := time.Now()
	for _, column := range []string{createdAt, updatedAt} {
		if resource.hasColumn(column) {
			payload[column] = now
		}
	}

	if err := r.db.WithContext(c.UserContext()).
		Table(tableOf(r.db, resource)).Create(payload).Error; err != nil {
		return writeRejected(c, resource, err)
	}

	row, err := r.find(c, resource, insertedID(payload))
	if err != nil {
		return err
	}
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{"data": row})
}

func (r *Registry) update(c *fiber.Ctx, resource *Resource) error {
	payload, err := body(c, resource, true)
	if err != nil {
		return err
	}
	if resource.hasColumn(updatedAt) {
		payload[updatedAt] = time.Now()
	}

	result := r.query(c, resource).Where("id = ?", c.Params("id")).Updates(payload)
	if result.Error != nil {
		return writeRejected(c, resource, result.Error)
	}
	if result.RowsAffected == 0 {
		return fiber.ErrNotFound
	}
	return r.show(c, resource)
}

func (r *Registry) destroy(c *fiber.Ctx, resource *Resource) error {
	query := r.query(c, resource).Where("id = ?", c.Params("id"))

	var result *gorm.DB
	if resource.softDeletes() {
		result = query.Updates(map[string]any{deletedAt: time.Now()})
	} else {
		result = query.Delete(nil)
	}

	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fiber.ErrNotFound
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func writeRejected(c *fiber.Ctx, resource *Resource, err error) error {
	log.Ctx(c.UserContext()).Error().
		Err(err).
		Str("resource", resource.Plural).
		Msg("api: the database refused the write")

	return fiber.NewError(fiber.StatusUnprocessableEntity,
		"The database refused this "+resource.Singular+".")
}

func body(c *fiber.Ctx, resource *Resource, partial bool) (map[string]any, error) {
	var raw map[string]any
	if err := c.BodyParser(&raw); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid JSON body")
	}

	payload := map[string]any{}
	for _, field := range resource.Fields {
		if field.ReadOnly || resource.Hidden[field.Name] {
			continue
		}
		if value, ok := raw[field.Name]; ok {
			payload[field.Column] = value
		}
	}
	if len(payload) == 0 && !partial {
		return nil, fiber.NewError(fiber.StatusUnprocessableEntity, "no writable field in the body")
	}
	return payload, nil
}

func insertedID(payload map[string]any) any {
	for _, key := range []string{"@id", "id"} {
		if value, ok := payload[key]; ok {
			return value
		}
	}
	return nil
}

var operators = map[string]string{
	"__like": " LIKE ?",
	"__gt":   " > ?",
	"__gte":  " >= ?",
	"__lt":   " < ?",
	"__lte":  " <= ?",
	"__ne":   " <> ?",
}

func applyFilters(query *gorm.DB, resource *Resource, c *fiber.Ctx) (*gorm.DB, error) {
	var failure error

	c.Context().QueryArgs().VisitAll(func(key, value []byte) {
		if failure != nil {
			return
		}
		name := string(key)
		switch name {
		case "page", "per_page", "sort":
			return
		}

		column, clause := name, " = ?"
		for suffix, expression := range operators {
			if strings.HasSuffix(name, suffix) {
				column, clause = strings.TrimSuffix(name, suffix), expression
				break
			}
		}
		if strings.HasSuffix(name, "__in") {
			column = strings.TrimSuffix(name, "__in")
			if !resource.filterable(column) {
				failure = fiber.NewError(fiber.StatusUnprocessableEntity, "unknown filter "+column)
				return
			}
			query = query.Where(column+" IN ?", strings.Split(string(value), ","))
			return
		}

		if !resource.filterable(column) {
			failure = fiber.NewError(fiber.StatusUnprocessableEntity, "unknown filter "+column)
			return
		}
		query = query.Where(column+clause, string(value))
	})

	return query, failure
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
