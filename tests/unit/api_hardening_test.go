package unit

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/graphql-go/graphql"
	"gorm.io/gorm"

	"omega/internal/api"
	"omega/internal/policy"
)

// A member and an admin, as the policies see them.
type hardActor struct {
	ID   uint
	Role string
}

type hardActorKey struct{}

type hardOrder struct {
	ID        uint      `gorm:"primarykey" json:"id"`
	UserID    uint      `json:"user_id"`
	Reference string    `gorm:"size:12" json:"reference"`
	Amount    string    `gorm:"type:numeric(36,18)" json:"amount"`
	Status    string    `json:"status"`
	Phone     string    `json:"phone"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (hardOrder) TableName() string { return "orders" }

var (
	hardAlice = &hardActor{ID: 1, Role: "member"}
	hardBob   = &hardActor{ID: 2, Role: "member"}
	hardAdmin = &hardActor{ID: 9, Role: "admin"}
)

func hardGate() *policy.Registry {
	gate := policy.New()
	gate.Before(func(_ context.Context, actor any, _ string) (bool, bool) {
		if typed, ok := actor.(*hardActor); ok && typed.Role == "admin" {
			return true, true
		}
		return false, false
	})
	// Not "orders.*": that would grant orders.any too.
	gate.Grant("member", "orders.list", "orders.view", "orders.create", "orders.delete")
	gate.Define("orders.update", func(_ context.Context, actor, subject any) bool {
		order, ok := subject.(*hardOrder)
		return ok && order.Status != "locked"
	})
	return gate
}

type hardFixture struct {
	db       *gorm.DB
	registry *api.Registry
	app      *fiber.App
	schema   graphql.Schema
}

func hardSetup(t *testing.T, options ...api.Option) *hardFixture {
	t.Helper()

	db := apiCovOpen(t)
	if err := db.AutoMigrate(&hardOrder{}); err != nil {
		t.Fatalf("migration: %v", err)
	}
	for _, order := range []hardOrder{
		{UserID: 1, Reference: "A-1", Amount: "10.5", Status: "open", Phone: "+50937000001"},
		{UserID: 1, Reference: "A-2", Amount: "20", Status: "locked", Phone: "+50937000001"},
		{UserID: 2, Reference: "B-1", Amount: "30", Status: "open", Phone: "+50937000002"},
	} {
		if err := db.Create(&order).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}

	registry := api.NewRegistry(db)
	if len(options) == 0 {
		options = []api.Option{api.Owner("user_id"), api.Guarded("status")}
	}
	api.RegisterWith[hardOrder](registry, "orders", "order", options...)

	gate := hardGate()
	registry.Protect(gate.Allows, func(ctx context.Context) any { return ctx.Value(hardActorKey{}) })

	app := fiber.New()
	pick := func(c fiber.Ctx) error {
		switch c.Get("X-Actor") {
		case "alice":
			c.Locals(hardActorKey{}, hardAlice)
		case "bob":
			c.Locals(hardActorKey{}, hardBob)
		case "admin":
			c.Locals(hardActorKey{}, hardAdmin)
		}
		return c.Next()
	}
	can := gate.Middleware(func(c fiber.Ctx) any { return c.Locals(hardActorKey{}) })
	registry.Mount(app.Group("/api"), pick, api.Authorize(can))

	schema, err := registry.Schema()
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	return &hardFixture{db: db, registry: registry, app: app, schema: schema}
}

func hardCall(t *testing.T, app *fiber.App, actor, method, path, payload string) (int, map[string]any) {
	t.Helper()

	var reader io.Reader
	if payload != "" {
		reader = strings.NewReader(payload)
	}
	request := httptest.NewRequest(method, path, reader)
	if payload != "" {
		request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationJSON)
	}
	if actor != "" {
		request.Header.Set("X-Actor", actor)
	}
	response, err := app.Test(request, fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer response.Body.Close()

	raw, _ := io.ReadAll(response.Body)
	body := map[string]any{}
	_ = json.Unmarshal(raw, &body)
	body["_raw"] = string(raw)
	return response.StatusCode, body
}

func hardRows(body map[string]any) []map[string]any {
	list, _ := body["data"].([]any)
	rows := make([]map[string]any, 0, len(list))
	for _, item := range list {
		row, _ := item.(map[string]any)
		rows = append(rows, row)
	}
	return rows
}

func hardData(body map[string]any) map[string]any {
	data, _ := body["data"].(map[string]any)
	return data
}

func hardGraphQL(t *testing.T, schema graphql.Schema, actor *hardActor, query string) *graphql.Result {
	t.Helper()
	ctx := context.Background()
	if actor != nil {
		ctx = context.WithValue(ctx, hardActorKey{}, actor)
	}
	return graphql.Do(graphql.Params{Schema: schema, RequestString: query, Context: ctx})
}

func gqlList(result *graphql.Result, field string) []map[string]any {
	data, _ := result.Data.(map[string]any)
	list, _ := data[field].([]any)
	rows := []map[string]any{}
	for _, item := range list {
		row, _ := item.(map[string]any)
		rows = append(rows, row)
	}
	return rows
}

// --- Ownership ------------------------------------------------------------

func TestOwnershipScopesTheRESTList(t *testing.T) {
	f := hardSetup(t)

	status, body := hardCall(t, f.app, "alice", "GET", "/api/orders", "")
	if status != fiber.StatusOK {
		t.Fatalf("status %d: %v", status, body["_raw"])
	}
	rows := hardRows(body)
	if len(rows) != 2 {
		t.Fatalf("alice sees %d orders, want her 2", len(rows))
	}
	for _, row := range rows {
		if row["user_id"] != float64(1) {
			t.Errorf("alice sees an order of user %v", row["user_id"])
		}
	}
	if meta, _ := body["meta"].(map[string]any); meta["total"] != float64(2) {
		t.Errorf("the total counts other users' rows: %v", meta)
	}

	_, body = hardCall(t, f.app, "admin", "GET", "/api/orders", "")
	if rows := hardRows(body); len(rows) != 3 {
		t.Errorf("orders.any must lift the scope: %d rows", len(rows))
	}
}

func TestOwnershipAnswers404OnSomeoneElsesRow(t *testing.T) {
	f := hardSetup(t)

	cases := []struct{ method, path, payload string }{
		{"GET", "/api/orders/3", ""},
		{"PATCH", "/api/orders/3", `{"reference":"PRIS"}`},
		{"DELETE", "/api/orders/3", ""},
	}
	for _, tc := range cases {
		if status, body := hardCall(t, f.app, "alice", tc.method, tc.path, tc.payload); status != fiber.StatusNotFound {
			t.Errorf("%s %s by alice: status %d, want 404 — %v", tc.method, tc.path, status, body["_raw"])
		}
	}

	var order hardOrder
	f.db.First(&order, 3)
	if order.Reference != "B-1" {
		t.Errorf("bob's order was changed: %q", order.Reference)
	}

	if status, _ := hardCall(t, f.app, "bob", "GET", "/api/orders/3", ""); status != fiber.StatusOK {
		t.Errorf("bob reads his own order: status %d", status)
	}
	if status, _ := hardCall(t, f.app, "admin", "GET", "/api/orders/3", ""); status != fiber.StatusOK {
		t.Errorf("admin reads any order: status %d", status)
	}
	if status, _ := hardCall(t, f.app, "bob", "DELETE", "/api/orders/3", ""); status != fiber.StatusNoContent {
		t.Errorf("bob deletes his own order: status %d", status)
	}
}

func TestOwnershipForcesTheOwnerOnCreate(t *testing.T) {
	f := hardSetup(t)

	status, body := hardCall(t, f.app, "alice", "POST", "/api/orders",
		`{"reference":"A-3","amount":"1","user_id":2,"status":"paid"}`)
	if status != fiber.StatusCreated {
		t.Fatalf("status %d: %v", status, body["_raw"])
	}
	data := hardData(body)
	if data["user_id"] != float64(1) {
		t.Errorf("the client chose the owner: user_id = %v", data["user_id"])
	}
	if data["status"] == "paid" {
		t.Error("a member wrote a guarded column")
	}

	status, body = hardCall(t, f.app, "admin", "POST", "/api/orders",
		`{"reference":"X-1","amount":"1","user_id":2,"status":"paid"}`)
	if status != fiber.StatusCreated {
		t.Fatalf("admin create: %d %v", status, body["_raw"])
	}
	if data := hardData(body); data["user_id"] != float64(2) || data["status"] != "paid" {
		t.Errorf("orders.any chooses the owner and writes guarded columns: %v", data)
	}
}

func TestOwnershipKeepsTheOwnerOnUpdate(t *testing.T) {
	f := hardSetup(t)

	status, body := hardCall(t, f.app, "alice", "PATCH", "/api/orders/1", `{"user_id":2,"reference":"A-1b"}`)
	if status != fiber.StatusOK {
		t.Fatalf("status %d: %v", status, body["_raw"])
	}
	if data := hardData(body); data["user_id"] != float64(1) || data["reference"] != "A-1b" {
		t.Errorf("a member gave her row away: %v", data)
	}
}

func TestPoliciesReceiveTheRowAsSubject(t *testing.T) {
	f := hardSetup(t)

	if status, body := hardCall(t, f.app, "alice", "PATCH", "/api/orders/2", `{"reference":"Z"}`); status != fiber.StatusForbidden {
		t.Errorf("the rule saw a locked order and must refuse: status %d — %v", status, body["_raw"])
	}
	if status, body := hardCall(t, f.app, "alice", "PATCH", "/api/orders/1", `{"reference":"Z"}`); status != fiber.StatusOK {
		t.Errorf("the rule saw an open order and must allow: status %d — %v", status, body["_raw"])
	}

	result := hardGraphQL(t, f.schema, hardAlice, `mutation { update_order(id: "2", reference: "Z") { id } }`)
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, "you may not orders.update") {
		t.Errorf("GraphQL must refuse the locked order too: %v", result.Errors)
	}
}

func TestOwnershipWithoutAnActorIsRefused(t *testing.T) {
	f := hardSetup(t)

	if status, _ := hardCall(t, f.app, "", "GET", "/api/orders", ""); status != fiber.StatusUnauthorized {
		t.Errorf("anonymous list: status %d, want 401", status)
	}
	if status, _ := hardCall(t, f.app, "", "GET", "/api/orders/1", ""); status != fiber.StatusUnauthorized {
		t.Errorf("anonymous show: status %d, want 401", status)
	}

	// Without a gate an owned resource cannot be honoured: refused.
	registry := api.NewRegistry(f.db)
	api.RegisterWith[hardOrder](registry, "orders", "order", api.Owner("user_id"))
	app := fiber.New()
	registry.Mount(app.Group("/api"))
	if status, _ := hardCall(t, app, "", "GET", "/api/orders", ""); status != fiber.StatusForbidden {
		t.Errorf("owned resource without gate: status %d, want 403", status)
	}
}

func TestGraphQLHonoursOwnershipLikeREST(t *testing.T) {
	f := hardSetup(t)

	rows := gqlList(hardGraphQL(t, f.schema, hardAlice, `{ orders { id user_id } }`), "orders")
	if len(rows) != 2 {
		t.Fatalf("alice sees %d orders over GraphQL, want 2", len(rows))
	}
	for _, row := range rows {
		if row["user_id"] != 1 {
			t.Errorf("GraphQL leaks an order of user %v", row["user_id"])
		}
	}

	count := hardGraphQL(t, f.schema, hardAlice, `{ orders_count }`)
	if data, _ := count.Data.(map[string]any); data["orders_count"] != 2 {
		t.Errorf("orders_count counts other users' rows: %v", count.Data)
	}

	one := hardGraphQL(t, f.schema, hardAlice, `{ order(id: "3") { id } }`)
	if data, _ := one.Data.(map[string]any); data["order"] != nil || len(one.Errors) > 0 {
		t.Errorf("someone else's order must read as null: %v %v", one.Data, one.Errors)
	}

	update := hardGraphQL(t, f.schema, hardAlice, `mutation { update_order(id: "3", reference: "PRIS") { id } }`)
	if data, _ := update.Data.(map[string]any); data["update_order"] != nil {
		t.Errorf("alice updated bob's order: %v", update.Data)
	}

	deleted := hardGraphQL(t, f.schema, hardAlice, `mutation { delete_order(id: "3") }`)
	if data, _ := deleted.Data.(map[string]any); data["delete_order"] != false {
		t.Errorf("alice deleted bob's order: %v %v", deleted.Data, deleted.Errors)
	}

	created := hardGraphQL(t, f.schema, hardAlice, `mutation { create_order(reference: "G-1", amount: "2.5", user_id: 2) { id user_id amount } }`)
	if len(created.Errors) > 0 {
		t.Fatalf("create: %v", created.Errors)
	}
	data, _ := created.Data.(map[string]any)
	order, _ := data["create_order"].(map[string]any)
	if order["user_id"] != 1 || order["amount"] != "2.5" {
		t.Errorf("GraphQL create must force the owner and keep the decimal: %v", order)
	}

	all := gqlList(hardGraphQL(t, f.schema, hardAdmin, `{ orders(limit: 50) { id } }`), "orders")
	if len(all) != 4 {
		t.Errorf("admin sees %d orders over GraphQL, want 4", len(all))
	}

	anonymous := hardGraphQL(t, f.schema, nil, `{ orders { id } }`)
	if len(anonymous.Errors) == 0 {
		t.Error("an anonymous GraphQL caller must be refused")
	}
}

func TestHiddenColumnsMatchAcrossRESTAndGraphQL(t *testing.T) {
	f := hardSetup(t)

	_, body := hardCall(t, f.app, "alice", "GET", "/api/orders/1", "")
	if _, leaked := hardData(body)["phone"]; leaked {
		t.Error("REST serves the phone number")
	}
	result := hardGraphQL(t, f.schema, hardAlice, `{ orders { phone } }`)
	if len(result.Errors) == 0 {
		t.Error("GraphQL serves the phone number")
	}

	if status, _ := hardCall(t, f.app, "alice", "GET", "/api/orders?phone=%2B50937000001", ""); status != fiber.StatusUnprocessableEntity {
		t.Errorf("filtering on a hidden column: status %d, want 422", status)
	}
	if status, _ := hardCall(t, f.app, "alice", "GET", "/api/orders?sort=phone", ""); status != fiber.StatusUnprocessableEntity {
		t.Errorf("sorting on a hidden column: status %d, want 422", status)
	}

	// A hidden column is never written, whatever the transport.
	status, body := hardCall(t, f.app, "alice", "PATCH", "/api/orders/1", `{"phone":"+1555"}`)
	if status != fiber.StatusOK {
		t.Fatalf("patch: %d %v", status, body["_raw"])
	}
	mutation := hardGraphQL(t, f.schema, hardAlice, `mutation { update_order(id: "1", phone: "+1555") { id } }`)
	if len(mutation.Errors) == 0 {
		t.Error("GraphQL accepts a hidden column as argument")
	}
	var order hardOrder
	f.db.First(&order, 1)
	if order.Phone != "+50937000001" {
		t.Errorf("a hidden column was written: %q", order.Phone)
	}
}

// --- Options --------------------------------------------------------------

func TestReadOnlyResourceServesListAndShowOnly(t *testing.T) {
	f := hardSetup(t, api.ReadOnly())

	if status, _ := hardCall(t, f.app, "admin", "GET", "/api/orders", ""); status != fiber.StatusOK {
		t.Errorf("list: %d", status)
	}
	if status, _ := hardCall(t, f.app, "admin", "GET", "/api/orders/1", ""); status != fiber.StatusOK {
		t.Errorf("show: %d", status)
	}
	if status, _ := hardCall(t, f.app, "admin", "POST", "/api/orders", `{"reference":"X"}`); status != fiber.StatusMethodNotAllowed {
		t.Errorf("create on a read-only resource: %d, want 405", status)
	}
	if status, _ := hardCall(t, f.app, "admin", "PATCH", "/api/orders/1", `{"reference":"X"}`); status != fiber.StatusMethodNotAllowed {
		t.Errorf("update on a read-only resource: %d, want 405", status)
	}

	if f.schema.MutationType() != nil {
		t.Error("a read-only resource has GraphQL mutations")
	}
	spec := f.registry.OpenAPI("t", "1", "/api")
	if spec.Paths["/orders"].Post != nil || spec.Paths["/orders/{id}"].Patch != nil {
		t.Error("OpenAPI documents writes a read-only resource does not serve")
	}
}

func TestActionsRestrictTheRoutes(t *testing.T) {
	f := hardSetup(t, api.Actions(api.ActionCreate, api.ActionView))

	if status, _ := hardCall(t, f.app, "admin", "GET", "/api/orders", ""); status == fiber.StatusOK {
		t.Error("list is served although not allowed")
	}
	if status, body := hardCall(t, f.app, "admin", "POST", "/api/orders", `{"reference":"X","amount":"1"}`); status != fiber.StatusCreated {
		t.Errorf("create: %d %v", status, body["_raw"])
	}
	if status, _ := hardCall(t, f.app, "admin", "DELETE", "/api/orders/1", ""); status != fiber.StatusMethodNotAllowed {
		t.Errorf("delete although not allowed: %d", status)
	}

	result := hardGraphQL(t, f.schema, hardAdmin, `{ orders { id } }`)
	if len(result.Errors) == 0 {
		t.Error("GraphQL lists although list is not allowed")
	}
	if _, present := f.registry.OpenAPI("t", "1", "/api").Paths["/orders"]; !present {
		t.Error("the collection path must stay for create")
	}
}

func TestVisibleLiftsTheDefaultHiding(t *testing.T) {
	f := hardSetup(t, api.Visible("phone"))
	_, body := hardCall(t, f.app, "admin", "GET", "/api/orders/1", "")
	if hardData(body)["phone"] != "+50937000001" {
		t.Errorf("Visible(phone) must serve the column: %v", body["_raw"])
	}

	g := hardSetup(t, api.Hidden("reference"), api.Visible("reference"))
	_, body = hardCall(t, g.app, "admin", "GET", "/api/orders/1", "")
	if _, leaked := hardData(body)["reference"]; leaked {
		t.Error("Visible cannot expose what Hidden named")
	}
}

func TestConfigureReachesADiscoveredTable(t *testing.T) {
	db := apiCovOpen(t)
	if err := db.Exec(`CREATE TABLE payouts (id INTEGER PRIMARY KEY, user_id INTEGER, msisdn TEXT, label TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO payouts (user_id, msisdn, label) VALUES (1, '50937000001', 'mine'), (2, '50937000002', 'theirs')`)

	registry := api.NewRegistry(db)
	if err := registry.Discover(db); err != nil {
		t.Fatal(err)
	}
	if err := registry.Configure("payouts", api.Owner("user_id")); err != nil {
		t.Fatal(err)
	}
	if err := registry.Configure("absent", api.ReadOnly()); err == nil {
		t.Error("configuring an unknown resource must fail")
	}

	gate := hardGate()
	gate.Grant("member", "payouts.list")
	registry.Protect(gate.Allows, func(ctx context.Context) any { return ctx.Value(hardActorKey{}) })
	app := fiber.New()
	registry.Mount(app.Group("/api"), func(c fiber.Ctx) error {
		c.Locals(hardActorKey{}, hardAlice)
		return c.Next()
	})

	_, body := hardCall(t, app, "", "GET", "/api/payouts", "")
	rows := hardRows(body)
	if len(rows) != 1 || rows[0]["label"] != "mine" {
		t.Errorf("Owner on a discovered table: %v", body["_raw"])
	}
	if _, leaked := rows[0]["msisdn"]; leaked {
		t.Error("msisdn is served on a discovered table")
	}
}

func TestIdentifyWithOverridesTheActorID(t *testing.T) {
	f := hardSetup(t)
	f.registry.IdentifyWith(func(any) any { return uint(2) })

	_, body := hardCall(t, f.app, "alice", "GET", "/api/orders", "")
	if rows := hardRows(body); len(rows) != 1 || rows[0]["reference"] != "B-1" {
		t.Errorf("IdentifyWith is ignored: %v", body["_raw"])
	}

	f.registry.IdentifyWith(func(any) any { return nil })
	if status, _ := hardCall(t, f.app, "alice", "GET", "/api/orders", ""); status != fiber.StatusForbidden {
		t.Errorf("an actor without id on an owned resource: %d, want 403", status)
	}
}

type keyedActor struct{ key string }

func (k keyedActor) Key() string  { return k.key }
func (k keyedActor) Role() string { return "member" }

type methodActor struct{}

func (methodActor) ID() any      { return uint(2) }
func (methodActor) Role() string { return "member" }

func TestActorIdentityFallsBackOnIDMethodAndKey(t *testing.T) {
	f := hardSetup(t)
	app := fiber.New()
	actors := map[string]any{"keyed": keyedActor{key: "1"}, "method": methodActor{}, "plain": "nobody"}
	f.registry.Mount(app.Group("/api"), func(c fiber.Ctx) error {
		c.Locals(hardActorKey{}, actors[c.Get("X-Actor")])
		return c.Next()
	})

	gate := policy.New().Grant("member", "orders.list")
	f.registry.Protect(gate.Allows, func(ctx context.Context) any { return ctx.Value(hardActorKey{}) })

	if _, body := hardCall(t, app, "keyed", "GET", "/api/orders", ""); len(hardRows(body)) != 2 {
		t.Errorf("Key() identity: %v", body["_raw"])
	}
	if _, body := hardCall(t, app, "method", "GET", "/api/orders", ""); len(hardRows(body)) != 1 {
		t.Errorf("ID() identity: %v", body["_raw"])
	}
	if status, _ := hardCall(t, app, "plain", "GET", "/api/orders", ""); status != fiber.StatusForbidden {
		t.Errorf("an actor that is not a struct: %d", status)
	}
}

// --- Sensitive columns ----------------------------------------------------

func TestPersonalDataIsHiddenByDefault(t *testing.T) {
	db := apiCovOpen(t)
	if err := db.Exec(`CREATE TABLE customers (
		id INTEGER PRIMARY KEY, name TEXT, email TEXT, contact_email TEXT,
		phone TEXT, phone_number TEXT, mobile_number TEXT, msisdn TEXT, home_phone TEXT,
		address TEXT, billing_address TEXT, ip TEXT, ip_address TEXT, last_login_ip TEXT,
		date_of_birth TEXT, dob TEXT, national_id TEXT, passport_number TEXT,
		document_url TEXT, kyc_status TEXT, wallet_address TEXT, api_key TEXT,
		provider TEXT, commission NUMERIC(10,4)
	)`).Error; err != nil {
		t.Fatal(err)
	}

	registry := api.NewRegistry(db)
	if err := registry.Discover(db); err != nil {
		t.Fatal(err)
	}
	resource := registry.Find("customers")
	for _, column := range []string{
		"email", "contact_email", "phone", "phone_number", "mobile_number", "msisdn", "home_phone",
		"address", "billing_address", "ip", "ip_address", "last_login_ip", "date_of_birth", "dob",
		"national_id", "passport_number", "document_url", "kyc_status", "wallet_address", "api_key",
	} {
		if !resource.Hidden[column] {
			t.Errorf("%s is served by discovery", column)
		}
	}
	for _, column := range []string{"name", "provider", "commission"} {
		if resource.Hidden[column] {
			t.Errorf("%s should stay visible", column)
		}
	}

	type member struct {
		ID    uint   `json:"id"`
		Email string `json:"email"`
		Phone string `json:"phone"`
	}
	registered := api.Register[member](registry, "members", "member")
	if registered.Hidden["email"] {
		t.Error("a registered model serves its email")
	}
	if !registered.Hidden["phone"] {
		t.Error("a registered model hides its phone by default")
	}
}

func TestDescribedColumnsOnlyLeaveARegisteredModel(t *testing.T) {
	db := apiCovOpen(t)
	if err := db.Exec(`CREATE TABLE profiles (id INTEGER PRIMARY KEY, label TEXT, internal_note TEXT)`).Error; err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO profiles (label, internal_note) VALUES ('ok', 'never served')`)

	type profile struct {
		ID           uint   `json:"id"`
		Label        string `json:"label"`
		InternalNote string `json:"-"`
	}
	registry := openGate(api.NewRegistry(db))
	api.Register[profile](registry, "profiles", "profile")
	app := apiCovMount(registry)

	status, body := apiCovCall(t, app, "GET", "/api/profiles/1", "")
	if status != fiber.StatusOK || strings.Contains(body, "never served") {
		t.Errorf("a json:\"-\" column reached the client: %d %s", status, body)
	}
}

type EmbeddedBase struct {
	ID        uint `gorm:"primarykey"`
	CreatedAt time.Time
}

type embeddedNote struct {
	EmbeddedBase
	Title string `json:"title"`
}

func (embeddedNote) TableName() string { return "embedded_notes" }

func TestEmbeddedStructsAreDescribed(t *testing.T) {
	db := apiCovOpen(t)
	if err := db.AutoMigrate(&embeddedNote{}); err != nil {
		t.Fatal(err)
	}
	db.Create(&embeddedNote{Title: "un"})

	registry := openGate(api.NewRegistry(db))
	api.Register[embeddedNote](registry, "embedded_notes", "embedded_note")
	status, body := apiCovCall(t, apiCovMount(registry), "GET", "/api/embedded_notes/1", "")
	if status != fiber.StatusOK || !strings.Contains(body, `"id":1`) || !strings.Contains(body, `"title":"un"`) {
		t.Errorf("the embedded fields are lost: %d %s", status, body)
	}
}

// --- Writes: decimals, sizes, types ---------------------------------------

func decimalEqual(a, b string) bool {
	x, okX := new(big.Rat).SetString(a)
	y, okY := new(big.Rat).SetString(b)
	return okX && okY && x.Cmp(y) == 0
}

func ledgerSetup(t *testing.T) (*gorm.DB, *fiber.App, graphql.Schema) {
	t.Helper()
	db := apiCovOpen(t)
	if err := db.Exec(`CREATE TABLE ledgers (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		amount NUMERIC(36,18),
		rate REAL,
		quantity INTEGER,
		settled BOOLEAN,
		label VARCHAR(5),
		meta JSON
	)`).Error; err != nil {
		t.Fatal(err)
	}
	registry := openGate(api.NewRegistry(db))
	if err := registry.Discover(db); err != nil {
		t.Fatal(err)
	}
	registry.AllowWrites("ledgers")
	schema, err := registry.Schema()
	if err != nil {
		t.Fatal(err)
	}
	return db, apiCovMount(registry), schema
}

func TestDecimalsRoundTripExactly(t *testing.T) {
	_, app, schema := ledgerSetup(t)

	// SQLite keeps 15 significant digits in a NUMERIC column (see the
	// PostgreSQL test for the full 36): within that, every value is exact.
	amounts := []string{
		`"123456789.123456"`,
		`"0.000000000000000001"`,
		`"-999999999999.999"`,
		`1234.56789012345`,
		`"100.50"`,
		`7`,
		`"0.1"`,
	}
	for _, amount := range amounts {
		status, body := hardCall(t, app, "", "POST", "/api/ledgers", `{"amount":`+amount+`}`)
		if status != fiber.StatusCreated {
			t.Fatalf("create %s: %d %v", amount, status, body["_raw"])
		}
		created := hardData(body)
		got, isString := created["amount"].(string)
		if !isString {
			t.Errorf("%s comes back as %T, want a string", amount, created["amount"])
			continue
		}
		if !decimalEqual(got, strings.Trim(amount, `"`)) || strings.ContainsAny(got, "eE") {
			t.Errorf("%s comes back as %s", amount, got)
		}

		_, shown := hardCall(t, app, "", "GET", fmt.Sprintf("/api/ledgers/%v", created["id"]), "")
		if again, _ := hardData(shown)["amount"].(string); again != got {
			t.Errorf("show reads %q, create answered %q", again, got)
		}
	}

	result := apiCovGraphQL(t, schema, `{ ledgers(limit: 1) { amount } }`)
	rows := gqlList(result, "ledgers")
	if len(rows) != 1 || rows[0]["amount"] != "123456789.123456" {
		t.Errorf("GraphQL reads the decimal as %v (%v)", rows, result.Errors)
	}

	mutation := apiCovGraphQL(t, schema, `mutation { create_ledger(amount: "0.000000000000000002") { amount } }`)
	data, _ := mutation.Data.(map[string]any)
	if created, _ := data["create_ledger"].(map[string]any); created == nil || !decimalEqual(fmt.Sprint(created["amount"]), "0.000000000000000002") {
		t.Errorf("GraphQL writes the decimal as %v (%v)", mutation.Data, mutation.Errors)
	}
}

func TestWritesAreValidatedAgainstTheColumns(t *testing.T) {
	_, app, schema := ledgerSetup(t)

	rejected := []string{
		`{"amount":"1e5"}`,
		`{"amount":"abc"}`,
		`{"amount":{"value":1}}`,
		`{"amount":"1234567890123456789.5"}`,
		`{"amount":"1.0000000000000000001"}`,
		`{"amount":"123456789012345678.123456789012345678"}`,
		`{"amount":1234.567890123456789}`,
		`{"amount":true}`,
		`{"quantity":1.5}`,
		`{"quantity":"douze"}`,
		`{"quantity":true}`,
		`{"rate":"vite"}`,
		`{"rate":[1]}`,
		`{"settled":"oui"}`,
		`{"label":"abcdef"}`,
		`{"label":["a"]}`,
	}
	for _, payload := range rejected {
		if status, body := hardCall(t, app, "", "POST", "/api/ledgers", payload); status != fiber.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422 — %v", payload, status, body["_raw"])
		}
	}

	accepted := []string{
		`{"amount":"1.000000000000000000"}`,
		`{"quantity":"12"}`,
		`{"quantity":12}`,
		`{"rate":"0.25"}`,
		`{"rate":0.25}`,
		`{"settled":true}`,
		`{"label":"éèàçù"}`,
		`{"label":12}`,
		`{"meta":{"provider":"moncash"}}`,
		`{"amount":null,"label":"x"}`,
		`{"label":"x","inconnu":"ignored"}`,
	}
	for _, payload := range accepted {
		if status, body := hardCall(t, app, "", "POST", "/api/ledgers", payload); status != fiber.StatusCreated {
			t.Errorf("%s: status %d, want 201 — %v", payload, status, body["_raw"])
		}
	}

	if status, _ := hardCall(t, app, "", "POST", "/api/ledgers", `{"label":"x"} {"label":"y"}`); status != fiber.StatusBadRequest {
		t.Errorf("two JSON documents: status %d, want 400", status)
	}

	result := apiCovGraphQL(t, schema, `mutation { create_ledger(label: "beaucoup trop long") { id } }`)
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, "longer than 5") {
		t.Errorf("GraphQL writes are validated the same way: %v", result.Errors)
	}
	result = apiCovGraphQL(t, schema, `mutation { create_ledger(quantity: 3, rate: 2, amount: "1") { id } }`)
	if len(result.Errors) > 0 {
		t.Errorf("GraphQL scalars must pass: %v", result.Errors)
	}
}

func TestRegisteredSizesAreEnforced(t *testing.T) {
	f := hardSetup(t)
	if status, body := hardCall(t, f.app, "alice", "POST", "/api/orders", `{"reference":"THIRTEEN-CHAR","amount":"1"}`); status != fiber.StatusUnprocessableEntity {
		t.Errorf("a reference longer than size:12: %d %v", status, body["_raw"])
	}
	if status, body := hardCall(t, f.app, "alice", "POST", "/api/orders", `{"amount":"1.0000000000000000001"}`); status != fiber.StatusUnprocessableEntity {
		t.Errorf("19 decimals in a numeric(36,18): %d %v", status, body["_raw"])
	}
}

// --- Query limits -----------------------------------------------------------

func filterSetup(t *testing.T) *fiber.App {
	t.Helper()
	db := apiCovOpen(t)
	if err := db.Exec(`CREATE TABLE rates (id INTEGER PRIMARY KEY, provider TEXT, note TEXT, quantity INTEGER)`).Error; err != nil {
		t.Fatal(err)
	}
	db.Exec(`INSERT INTO rates (provider, note, quantity) VALUES
		('moncash', '100%', 1), ('natcash', '100 pct', 2), ('mon_cash', 'a\b', 3), ('monXcash', 'plain', 4)`)
	registry := openGate(api.NewRegistry(db))
	if err := registry.Discover(db); err != nil {
		t.Fatal(err)
	}
	registry.SetLimits(api.Limits{MaxFilters: 3, MaxWildcards: 2, MaxPatternLength: 10})
	return apiCovMount(registry)
}

func providersOf(body map[string]any) []string {
	out := []string{}
	for _, row := range hardRows(body) {
		out = append(out, fmt.Sprint(row["provider"]))
	}
	return out
}

func TestLikeFiltersEscapeTheClientInput(t *testing.T) {
	app := filterSetup(t)

	cases := []struct {
		query string
		want  string
	}{
		{"provider__contains=on_c", "mon_cash"},
		{"provider__starts=mon_", "mon_cash"},
		{"provider__ends=_cash", "mon_cash"},
		{"note__contains=0%25", "moncash"},
		{"provider__like=mon_cash", "mon_cash,monXcash"},
		{"provider__like=mon%25cash", "moncash,mon_cash,monXcash"},
		{"provider__like=mon%5C_cash", "mon_cash"},
		{"note__like=a%5C", ""},
		{"note__like=a%5C%5Cb", "mon_cash"},
		{"note__contains=a%5Cb", "mon_cash"},
	}
	for _, tc := range cases {
		status, body := hardCall(t, app, "", "GET", "/api/rates?sort=id&"+tc.query, "")
		if status != fiber.StatusOK {
			t.Errorf("%s: status %d — %v", tc.query, status, body["_raw"])
			continue
		}
		if got := strings.Join(providersOf(body), ","); got != tc.want {
			t.Errorf("%s matched %q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestQueryLimitsAreEnforced(t *testing.T) {
	app := filterSetup(t)

	refused := []string{
		"provider=a&note=b&quantity=1&id=1",
		"provider__like=%25a%25b%25",
		"provider__contains=0123456789x",
		"provider__in=" + strings.Repeat("a,", 100) + "a",
		"note__like=x&sort=inconnue",
	}
	for _, query := range refused {
		if status, body := hardCall(t, app, "", "GET", "/api/rates?"+query, ""); status != fiber.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, want 422 — %v", truncateQuery(query), status, body["_raw"])
		}
	}

	if status, _ := hardCall(t, app, "", "GET", "/api/rates?provider__in="+strings.Repeat("a,", 99)+"moncash&page=1&per_page=5&sort=-id", ""); status != fiber.StatusOK {
		t.Errorf("100 values in __in and reserved keys must pass: %d", status)
	}

	status, body := hardCall(t, app, "", "GET", "/api/rates?quantity__gte=2&quantity__lt=4&sort=-quantity", "")
	if status != fiber.StatusOK || strings.Join(providersOf(body), ",") != "mon_cash,natcash" {
		t.Errorf("range filters: %d %v", status, body["_raw"])
	}
}

func truncateQuery(query string) string {
	if len(query) > 60 {
		return query[:60] + "…"
	}
	return query
}

func TestCountFalseSkipsTheTotal(t *testing.T) {
	app := filterSetup(t)

	_, counted := hardCall(t, app, "", "GET", "/api/rates", "")
	if meta, _ := counted["meta"].(map[string]any); meta["total"] != float64(4) || meta["last_page"] != float64(1) {
		t.Errorf("the default list counts: %v", counted["meta"])
	}

	_, uncounted := hardCall(t, app, "", "GET", "/api/rates?count=false&per_page=2", "")
	meta, _ := uncounted["meta"].(map[string]any)
	if _, present := meta["total"]; present {
		t.Errorf("count=false still counts: %v", meta)
	}
	if _, present := meta["last_page"]; present {
		t.Errorf("count=false still computes last_page: %v", meta)
	}
	if len(hardRows(uncounted)) != 2 || meta["per_page"] != float64(2) {
		t.Errorf("count=false must still page: %v", uncounted["_raw"])
	}
}

// --- GraphQL limits ---------------------------------------------------------

const introspectionQuery = `query IntrospectionQuery {
  __schema {
    queryType { name } mutationType { name } subscriptionType { name }
    types { ...FullType }
    directives { name description locations args { ...InputValue } }
  }
}
fragment FullType on __Type {
  kind name description
  fields(includeDeprecated: true) { name description args { ...InputValue } type { ...TypeRef } isDeprecated deprecationReason }
  inputFields { ...InputValue }
  interfaces { ...TypeRef }
  enumValues(includeDeprecated: true) { name description isDeprecated deprecationReason }
  possibleTypes { ...TypeRef }
}
fragment InputValue on __InputValue { name description type { ...TypeRef } defaultValue }
fragment TypeRef on __Type {
  kind name
  ofType { kind name ofType { kind name ofType { kind name ofType { kind name ofType { kind name
    ofType { kind name ofType { kind name ofType { kind name ofType { kind name } } } } } } } } }
}`

// The introspection query above is the one the bundled GraphiQL sends (nine
// ofType levels): the default limits must let it through.

func guardApp(t *testing.T, limits api.Limits) *fiber.App {
	t.Helper()
	registry, _ := apiCovRegistry(t)
	registry.SetLimits(limits)
	app := fiber.New()
	app.Post("/graphql", registry.GraphQLGuard(), func(c fiber.Ctx) error { return c.SendString("executed") })
	return app
}

func guardCall(t *testing.T, app *fiber.App, path, contentType, body string) (int, string) {
	t.Helper()
	request := httptest.NewRequest("POST", path, strings.NewReader(body))
	if contentType != "" {
		request.Header.Set(fiber.HeaderContentType, contentType)
	}
	response, err := app.Test(request, fiber.TestConfig{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(raw)
}

func gqlBody(query string) string {
	encoded, _ := json.Marshal(map[string]any{"query": query})
	return string(encoded)
}

func TestGraphQLGuardBoundsTheQuery(t *testing.T) {
	app := guardApp(t, api.Limits{})

	accepted := []string{
		`{ carnets { id title } }`,
		introspectionQuery,
		`{ a: carnets(limit: 200) { id title } b: carnets(limit: 200) { id } }`,
		`query { ...Root } fragment Root on Query { carnets { id } }`,
		`{ carnets { ... on Carnet { id } } }`,
		`{ carnets { `,
	}
	for _, query := range accepted {
		if status, body := guardCall(t, app, "/graphql", "application/json", gqlBody(query)); status != fiber.StatusOK {
			t.Errorf("%.40q refused: %d %s", query, status, body)
		}
	}

	aliases := make([]string, 0, 11)
	for i := range 11 {
		aliases = append(aliases, fmt.Sprintf("a%d: carnets { id }", i))
	}
	deep := "{ __schema { types { fields { type " + strings.Repeat("{ ofType ", 12) + "{ name }" + strings.Repeat(" }", 12) + " } } } }"
	wide := make([]string, 0, 501)
	for i := range 501 {
		wide = append(wide, fmt.Sprintf("f%d: id", i))
	}

	refused := map[string]string{
		"aliases":  "{ " + strings.Join(aliases, " ") + " }",
		"depth":    deep,
		"fields":   "{ carnets { " + strings.Join(wide, " ") + " } }",
		"cost":     "{ a: carnets(limit: 200) { id title actif created_at updated_at } b: carnets(limit: 200) { id title actif created_at updated_at } c: carnets(limit: 200) { id title actif created_at updated_at } d: carnets(limit: 200) { id title actif created_at updated_at } e: carnets(limit: 200) { id title actif created_at updated_at } f: carnets(limit: 200) { id title actif created_at updated_at } g: carnets(limit: 200) { id title actif created_at updated_at } h: carnets(limit: 200) { id title actif created_at updated_at } i: carnets(limit: 200) { id title actif created_at updated_at } j: carnets(limit: 200) { id title actif created_at updated_at } }",
		"cycle":    "query { carnets { ...A } } fragment A on Carnet { id ...B } fragment B on Carnet { ...A }",
		"variable": `query ($n: Int) { a: carnets(limit: $n) { id title actif created_at updated_at } b: carnets(limit: $n) { id title actif created_at updated_at } c: carnets(limit: $n) { id title actif created_at updated_at } d: carnets(limit: $n) { id title actif created_at updated_at } e: carnets(limit: $n) { id title actif created_at updated_at } f: carnets(limit: $n) { id title actif created_at updated_at } g: carnets(limit: $n) { id title actif created_at updated_at } h: carnets(limit: $n) { id title actif created_at updated_at } i: carnets(limit: $n) { id title actif created_at updated_at } j: carnets(limit: $n) { id title actif created_at updated_at } }`,
	}
	for name, query := range refused {
		if status, body := guardCall(t, app, "/graphql", "application/json", gqlBody(query)); status != fiber.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 — %s", name, status, body)
		}
	}

	withVariables, _ := json.Marshal(map[string]any{"query": refused["variable"], "variables": map[string]any{"n": 1}})
	if status, body := guardCall(t, app, "/graphql", "application/json", string(withVariables)); status != fiber.StatusOK {
		t.Errorf("a small limit through a variable must pass: %d %s", status, body)
	}
}

func TestGraphQLGuardRefusesOtherShapes(t *testing.T) {
	app := guardApp(t, api.Limits{})

	if status, _ := guardCall(t, app, "/graphql", "application/json", `[{"query":"{ carnets { id } }"}]`); status != fiber.StatusBadRequest {
		t.Errorf("batch: %d, want 400", status)
	}
	if status, _ := guardCall(t, app, "/graphql?query=%7Bcarnets%7Bid%7D%7D", "application/json", gqlBody("{ __schema { queryType { name } } }")); status != fiber.StatusBadRequest {
		t.Errorf("a query in the URL: %d, want 400", status)
	}
	if status, _ := guardCall(t, app, "/graphql", "application/graphql", "{ carnets { id } }"); status != fiber.StatusUnsupportedMediaType {
		t.Errorf("application/graphql: %d, want 415", status)
	}
	if status, _ := guardCall(t, app, "/graphql", "application/json", `{"query":`); status != fiber.StatusBadRequest {
		t.Errorf("broken JSON: %d, want 400", status)
	}
}

func TestLimitsTakeTheirDefaults(t *testing.T) {
	registry := api.NewRegistry(nil)
	if registry.Limits() != api.DefaultLimits {
		t.Errorf("defaults = %+v", registry.Limits())
	}
	registry.SetLimits(api.Limits{MaxFilters: 2})
	if got := registry.Limits(); got.MaxFilters != 2 || got.MaxInValues != api.DefaultLimits.MaxInValues {
		t.Errorf("partial limits = %+v", got)
	}
}

// --- Discovery modes --------------------------------------------------------

func TestResolveDiscoveryPicksTheMode(t *testing.T) {
	cases := []struct {
		env, mode string
		want      api.DiscoveryMode
	}{
		{"local", "", api.DiscoverAll},
		{"testing", "", api.DiscoverAll},
		{"production", "", api.DiscoverNone},
		{"prod", "", api.DiscoverNone},
		{"production", "all", api.DiscoverAll},
		{"production", " Allowlist ", api.DiscoverAllowlist},
		{"local", "none", api.DiscoverNone},
	}
	for _, tc := range cases {
		got, err := api.ResolveDiscovery(tc.env, tc.mode, nil)
		if err != nil || got.Mode != tc.want {
			t.Errorf("(%s, %q) = %v %v, want %v", tc.env, tc.mode, got.Mode, err, tc.want)
		}
	}
	if _, err := api.ResolveDiscovery("local", "everything", nil); err == nil {
		t.Error("an unknown mode must fail the boot")
	}
}

func TestSplitListReadsYAMLAndEnv(t *testing.T) {
	cases := []struct {
		in   any
		want string
	}{
		{nil, ""},
		{"", ""},
		{"rates, providers,,", "rates|providers"},
		{[]string{"a", " b "}, "a|b"},
		{[]any{"a", 2}, "a|2"},
		{42, "42"},
	}
	for _, tc := range cases {
		if got := strings.Join(api.SplitList(tc.in), "|"); got != tc.want {
			t.Errorf("SplitList(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDiscoverWithHonoursTheMode(t *testing.T) {
	db := apiCovOpen(t)
	for _, ddl := range []string{
		`CREATE TABLE rates (id INTEGER PRIMARY KEY, provider TEXT)`,
		`CREATE TABLE providers (id INTEGER PRIMARY KEY, name TEXT)`,
		`CREATE TABLE jobs (id INTEGER PRIMARY KEY, payload TEXT)`,
	} {
		if err := db.Exec(ddl).Error; err != nil {
			t.Fatal(err)
		}
	}

	none := api.NewRegistry(db)
	if published, err := none.DiscoverWith(db, api.Discovery{Mode: api.DiscoverNone}); err != nil || len(published) != 0 || len(none.All()) != 0 {
		t.Errorf("none published %v (%v)", published, err)
	}

	all := api.NewRegistry(db)
	published, err := all.DiscoverWith(db, api.Discovery{Mode: api.DiscoverAll})
	if err != nil || strings.Join(published, ",") != "providers,rates" && strings.Join(published, ",") != "rates,providers" {
		t.Errorf("all published %v (%v)", published, err)
	}

	allow := api.NewRegistry(db)
	published, err = allow.DiscoverWith(db, api.Discovery{Mode: api.DiscoverAllowlist, Tables: []string{"rates", "jobs", "absent"}})
	if err != nil || strings.Join(published, ",") != "rates" {
		t.Errorf("allowlist published %v (%v)", published, err)
	}
	if allow.Find("jobs") != nil {
		t.Error("an internal table was published because it was allow-listed")
	}

	registered := api.NewRegistry(db)
	api.Register[apiCovFantome](registered, "rates", "rate")
	published, _ = registered.DiscoverWith(db, api.Discovery{Mode: api.DiscoverNone})
	if len(published) != 0 || registered.Find("rates") == nil {
		t.Error("a registered resource is always published, whatever the mode")
	}
}

func TestAllowWritesExtendsAnActionList(t *testing.T) {
	db := apiCovOpen(t)
	if err := db.Exec(`CREATE TABLE flags (id INTEGER PRIMARY KEY, settled NUMERIC, amount NUMERIC(10,2), quantity INTEGER)`).Error; err != nil {
		t.Fatal(err)
	}
	registry := openGate(api.NewRegistry(db))
	if err := registry.Discover(db); err != nil {
		t.Fatal(err)
	}
	if err := registry.Configure("flags", api.ReadOnly(), nil); err != nil {
		t.Fatal(err)
	}
	registry.AllowWrites("flags")
	resource := registry.Find("flags")
	if !resource.Allows(api.ActionCreate) || !resource.Allows(api.ActionDelete) || resource.Allows("export") {
		t.Errorf("AllowWrites must add the writes to an explicit list: %v", resource.Actions)
	}

	types := map[string]string{}
	for _, field := range resource.Fields {
		types[field.Name] = field.Type + "/" + field.Format
	}
	if types["settled"] != "number/" || types["amount"] != "string/decimal" {
		t.Errorf("a bare SQLite NUMERIC is a number, NUMERIC(p,s) a decimal: %v", types)
	}

	app := apiCovMount(registry)
	status, body := hardCall(t, app, "", "POST", "/api/flags", `{"settled":true,"amount":12,"quantity":"3"}`)
	if status != fiber.StatusCreated {
		t.Fatalf("create: %d %v", status, body["_raw"])
	}
	data := hardData(body)
	if data["amount"] != "12" || data["quantity"] != float64(3) {
		t.Errorf("decimal and integer normalisation: %v", data)
	}

	if status, _ := hardCall(t, app, "", "DELETE", "/api/flags/1", ""); status != fiber.StatusNoContent {
		t.Errorf("delete: %d", status)
	}
}

func TestRowPoliciesAlsoGuardGraphQLDeleteAndREST(t *testing.T) {
	f := hardSetup(t)
	gate := hardGate()
	gate.Define("orders.delete", func(_ context.Context, _ any, subject any) bool {
		order, ok := subject.(*hardOrder)
		return ok && order.Status != "locked"
	})
	f.registry.Protect(gate.Allows, func(ctx context.Context) any { return ctx.Value(hardActorKey{}) })

	if status, _ := hardCall(t, f.app, "alice", "DELETE", "/api/orders/2", ""); status != fiber.StatusForbidden {
		t.Errorf("deleting a locked order: %d, want 403", status)
	}
	result := hardGraphQL(t, f.schema, hardAlice, `mutation { delete_order(id: "2") }`)
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, "orders.delete") {
		t.Errorf("GraphQL delete of a locked order: %v", result.Errors)
	}
	result = hardGraphQL(t, f.schema, nil, `mutation { update_order(id: "1", reference: "x") { id } }`)
	if len(result.Errors) == 0 || !strings.Contains(result.Errors[0].Message, "authentication required") {
		t.Errorf("anonymous GraphQL update: %v", result.Errors)
	}
	result = hardGraphQL(t, f.schema, hardAlice, `mutation { update_order(id: "1", amount: "1e3") { id } }`)
	if len(result.Errors) == 0 {
		t.Error("GraphQL update must validate the decimal")
	}

	member := &hardActor{ID: 1, Role: "guest"}
	if result := hardGraphQL(t, f.schema, member, `{ orders_count }`); len(result.Errors) == 0 {
		t.Error("orders_count without orders.list must be refused")
	}
	if result := hardGraphQL(t, f.schema, member, `{ order(id: "1") { id } }`); len(result.Errors) == 0 {
		t.Error("order without orders.view must be refused")
	}
}
