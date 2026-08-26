package unit

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"

	"omega/internal/validation"
)

type covPayload struct {
	Email string `json:"email" validate:"required,email"`
	Age   int    `json:"age" validate:"min=18,max=120"`
}

type covNested struct {
	Owner covPayload `json:"owner"`
}

type covUntagged struct {
	Untagged string `validate:"required"`
	Hidden   string `json:"-" validate:"required"`
	Optioned string `json:"optioned,omitempty" validate:"required"`
}

func covFieldsOf(t *testing.T, err error) map[string]string {
	t.Helper()

	var failure *validation.Error
	if !errors.As(err, &failure) {
		t.Fatalf("erreur de validation attendue, obtenu %T: %v", err, err)
	}
	return failure.Fields
}

func TestCovRuleRegistersATagAndItsMessage(t *testing.T) {
	validation.Rule("covslug", func(value string) bool {
		return value != "" && !strings.ContainsAny(value, " _")
	}, "Only dashes are allowed.")

	type payload struct {
		Slug string `json:"slug" validate:"covslug"`
	}

	if err := validation.Struct(&payload{Slug: "mon-article"}); err != nil {
		t.Fatalf("une valeur conforme ne doit pas echouer: %v", err)
	}

	fields := covFieldsOf(t, validation.Struct(&payload{Slug: "mon article"}))
	if fields["slug"] != "Only dashes are allowed." {
		t.Errorf("message = %q, attendu celui passe a Rule", fields["slug"])
	}
}

func TestCovRuleWithoutMessageFallsBackToTheTagName(t *testing.T) {
	validation.Rule("covdigits", func(value string) bool {
		return strings.Trim(value, "0123456789") == ""
	}, "")

	type payload struct {
		Code string `json:"code" validate:"covdigits"`
	}

	fields := covFieldsOf(t, validation.Struct(&payload{Code: "12a"}))
	if fields["code"] != "" {
		t.Errorf("un message vide doit rester vide, obtenu %q", fields["code"])
	}
}

func TestCovUnknownTagGetsAGenericMessage(t *testing.T) {
	type payload struct {
		Value string `json:"value" validate:"alpha"`
	}

	fields := covFieldsOf(t, validation.Struct(&payload{Value: "123"}))
	if !strings.Contains(fields["value"], "alpha") {
		t.Errorf("le message par defaut doit nommer la regle, obtenu %q", fields["value"])
	}
}

func TestCovFieldPathKeepsTheNesting(t *testing.T) {
	fields := covFieldsOf(t, validation.Struct(&covNested{}))

	if _, ok := fields["owner.email"]; !ok {
		t.Errorf("chemin imbrique attendu, obtenu %v", fields)
	}
}

func TestCovFieldNameFollowsTheJsonTag(t *testing.T) {
	fields := covFieldsOf(t, validation.Struct(&covUntagged{}))

	for _, want := range []string{"Untagged", "Hidden", "optioned"} {
		if _, ok := fields[want]; !ok {
			t.Errorf("champ %q absent de %v", want, fields)
		}
	}
}

func TestCovStructRefusesANonStruct(t *testing.T) {
	err := validation.Struct("pas une structure")

	if err == nil {
		t.Fatal("une valeur non structuree doit echouer")
	}
	var failure *validation.Error
	if errors.As(err, &failure) {
		t.Error("ce cas est une erreur de programmation, pas une erreur de champ")
	}
}

func TestCovBindReadsAValidBody(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c *fiber.Ctx) error {
		body, err := validation.Bind[covPayload](c)
		if err != nil {
			return err
		}
		return c.JSON(body)
	})

	if status := covPost(t, app, `{"email":"a@b.test","age":30}`); status != fiber.StatusOK {
		t.Fatalf("statut = %d", status)
	}
}

func TestCovBindRejectsBrokenJson(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c *fiber.Ctx) error {
		_, err := validation.Bind[covPayload](c)
		return err
	})

	if status := covPost(t, app, `{"email":`); status != fiber.StatusBadRequest {
		t.Fatalf("statut = %d, attendu 400", status)
	}
}

func TestCovBindReportsEveryInvalidField(t *testing.T) {
	app := fiber.New()
	app.Post("/", func(c *fiber.Ctx) error {
		_, err := validation.Bind[covPayload](c)
		if err == nil {
			return c.SendStatus(fiber.StatusOK)
		}

		fields := covFieldsOf(t, err)
		if _, ok := fields["email"]; !ok {
			t.Errorf("email absent de %v", fields)
		}
		if _, ok := fields["age"]; !ok {
			t.Errorf("age absent de %v", fields)
		}
		return c.SendStatus(fiber.StatusUnprocessableEntity)
	})

	if status := covPost(t, app, `{"email":"pas-une-adresse","age":3}`); status != fiber.StatusUnprocessableEntity {
		t.Fatalf("statut = %d", status)
	}
}

func TestCovMessagesAreHumanReadable(t *testing.T) {
	type payload struct {
		Required string `json:"required" validate:"required"`
		Email    string `json:"email" validate:"omitempty,email"`
		Short    string `json:"short" validate:"omitempty,min=5"`
		Long     string `json:"long" validate:"omitempty,max=3"`
		Small    int    `json:"small" validate:"omitempty,min=10"`
		Big      int    `json:"big" validate:"omitempty,max=2"`
		Choice   string `json:"choice" validate:"omitempty,oneof=un deux"`
		Link     string `json:"link" validate:"omitempty,url"`
		Password string `json:"password" validate:"omitempty,notcommon"`
		Other    string `json:"other" validate:"omitempty,nefield=Required"`
		Confirm  string `json:"confirm" validate:"omitempty,eqfield=Required"`
	}

	fields := covFieldsOf(t, validation.Struct(&payload{
		Email:    "pas-une-adresse",
		Short:    "abc",
		Long:     "beaucoup-trop-long",
		Small:    1,
		Big:      99,
		Choice:   "trois",
		Link:     "pas-une-url",
		Password: "password",
		Other:    "",
		Confirm:  "different",
	}))

	expected := map[string]string{
		"required": "This field is required.",
		"email":    "A valid email address is required.",
		"short":    "At least 5 characters are required.",
		"long":     "At most 3 characters are allowed.",
		"small":    "The minimum is 10.",
		"big":      "The maximum is 2.",
		"choice":   "Expected one of: un, deux.",
		"link":     "A valid URL is required.",
		"password": "This password is too common. Choose another one.",
		"confirm":  "The two fields do not match.",
	}

	for field, want := range expected {
		if got := fields[field]; got != want {
			t.Errorf("%s: message = %q, attendu %q", field, got, want)
		}
	}
}

func TestCovFailedBuildsASingleFieldError(t *testing.T) {
	err := validation.Failed("email", "Cette adresse est deja prise.")

	if err.Error() != "validation failed" {
		t.Errorf("Error() = %q", err.Error())
	}
	if err.Fields["email"] != "Cette adresse est deja prise." {
		t.Errorf("Fields = %v", err.Fields)
	}
}

func covPost(t *testing.T, app *fiber.App, body string) int {
	t.Helper()

	request := httptest.NewRequest("POST", "/", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")

	response, err := app.Test(request, -1)
	if err != nil {
		t.Fatalf("requete: %v", err)
	}
	defer func() { _ = response.Body.Close() }()

	return response.StatusCode
}
