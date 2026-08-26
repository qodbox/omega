package validation

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
)

var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New(validator.WithRequiredStructEnabled())

	_ = v.RegisterValidation("notcommon", func(field validator.FieldLevel) bool {
		return !IsCommonPassword(field.Field().String())
	})

	v.RegisterTagNameFunc(func(field reflect.StructField) string {
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			return field.Name
		}
		return name
	})
	return v
}

type Error struct {
	Fields map[string]string
}

func (e *Error) Error() string { return "validation failed" }

var messages = map[string]string{}

func Rule(tag string, check func(value string) bool, message string) {
	_ = validate.RegisterValidation(tag, func(field validator.FieldLevel) bool {
		return check(field.Field().String())
	})
	messages[tag] = message
}

func Failed(field, message string) *Error {
	return &Error{Fields: map[string]string{field: message}}
}

func Bind[T any](c *fiber.Ctx) (*T, error) {
	var body T

	if err := c.BodyParser(&body); err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "The request body is not valid JSON.")
	}
	if err := Struct(&body); err != nil {
		return nil, err
	}
	return &body, nil
}

func Struct(value any) error {
	err := validate.Struct(value)
	if err == nil {
		return nil
	}

	var invalid *validator.InvalidValidationError
	if ok := asInvalid(err, &invalid); ok {
		return fmt.Errorf("validation: %w", err)
	}

	fields := map[string]string{}
	var failures validator.ValidationErrors
	if ok := asFailures(err, &failures); ok {
		for _, failure := range failures {
			fields[fieldPath(failure)] = message(failure)
		}
	}
	return &Error{Fields: fields}
}

func fieldPath(failure validator.FieldError) string {
	if _, rest, found := strings.Cut(failure.Namespace(), "."); found && rest != "" {
		return rest
	}
	return failure.Field()
}

func message(failure validator.FieldError) string {
	switch failure.Tag() {
	case "required":
		return "This field is required."
	case "email":
		return "A valid email address is required."
	case "min":
		if failure.Kind() == reflect.String {
			return fmt.Sprintf("At least %s characters are required.", failure.Param())
		}
		return fmt.Sprintf("The minimum is %s.", failure.Param())
	case "max":
		if failure.Kind() == reflect.String {
			return fmt.Sprintf("At most %s characters are allowed.", failure.Param())
		}
		return fmt.Sprintf("The maximum is %s.", failure.Param())
	case "eqfield":
		return "The two fields do not match."
	case "oneof":
		return "Expected one of: " + strings.ReplaceAll(failure.Param(), " ", ", ") + "."
	case "url":
		return "A valid URL is required."
	case "notcommon":
		return "This password is too common. Choose another one."
	case "nefield":
		return "This value must differ from the other field."
	default:
		if custom, ok := messages[failure.Tag()]; ok {
			return custom
		}
		return "This value does not satisfy the " + failure.Tag() + " rule."
	}
}

func asInvalid(err error, target **validator.InvalidValidationError) bool {
	typed, ok := err.(*validator.InvalidValidationError)
	if ok {
		*target = typed
	}
	return ok
}

func asFailures(err error, target *validator.ValidationErrors) bool {
	typed, ok := err.(validator.ValidationErrors)
	if ok {
		*target = typed
	}
	return ok
}
