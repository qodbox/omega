package auth

import (
	"errors"

	"github.com/gofiber/fiber/v2"

	"omega/app/models"
	presenter "omega/app/presenters/auth"
	request "omega/app/requests/auth"
	service "omega/app/services/auth"
	jwt "omega/internal/auth"
	"omega/internal/validation"
)

type Controller struct {
	auth  *service.Service
	guard *jwt.Guard
}

func NewController(service *service.Service, guard *jwt.Guard) *Controller {
	return &Controller{auth: service, guard: guard}
}

func (a *Controller) Register(c *fiber.Ctx) error {
	body, err := validation.Bind[request.Register](c)
	if err != nil {
		return err
	}

	session, err := a.auth.Register(c.UserContext(), *body)
	if err != nil {
		return translate(err)
	}
	return c.Status(fiber.StatusCreated).JSON(sessionResponse(session))
}

func (a *Controller) Login(c *fiber.Ctx) error {
	body, err := validation.Bind[request.Login](c)
	if err != nil {
		return err
	}

	session, err := a.auth.Login(c.UserContext(), *body, c.IP())
	if err != nil {
		return translate(err)
	}
	return c.JSON(sessionResponse(session))
}

func (a *Controller) Refresh(c *fiber.Ctx) error {
	body, err := validation.Bind[request.Refresh](c)
	if err != nil {
		return err
	}

	tokens, err := a.auth.Refresh(c.UserContext(), body.RefreshToken)
	if err != nil {
		return translate(err)
	}
	return c.JSON(fiber.Map{"tokens": tokens})
}

func (a *Controller) Logout(c *fiber.Ctx) error {
	body, err := validation.Bind[request.Refresh](c)
	if err != nil {
		return err
	}

	a.auth.Logout(body.RefreshToken)
	return c.SendStatus(fiber.StatusNoContent)
}

func (a *Controller) Me(c *fiber.Ctx) error {
	user, err := a.caller(c)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": presenter.NewUser(user)})
}

func (a *Controller) ChangePassword(c *fiber.Ctx) error {
	user, err := a.caller(c)
	if err != nil {
		return err
	}

	body, err := validation.Bind[request.ChangePassword](c)
	if err != nil {
		return err
	}

	tokens, err := a.auth.ChangePassword(c.UserContext(), user, *body)
	if err != nil {
		return translate(err)
	}
	return c.JSON(fiber.Map{"tokens": tokens})
}

func (a *Controller) caller(c *fiber.Ctx) (*models.User, error) {
	user, ok := a.guard.User(c).(*models.User)
	if !ok {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
	}
	return user, nil
}

func sessionResponse(session *service.Session) fiber.Map {
	return fiber.Map{
		"data":   presenter.NewUser(session.User),
		"tokens": session.Tokens,
	}
}

func translate(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidCredentials):

		return fiber.NewError(fiber.StatusUnauthorized, "Invalid credentials.")
	case errors.Is(err, service.ErrInvalidRefresh):
		return fiber.NewError(fiber.StatusUnauthorized, "Invalid refresh token.")
	case errors.Is(err, service.ErrNoProviderEmail):
		return fiber.NewError(fiber.StatusUnprocessableEntity,
			"This provider did not return an email address.")
	case errors.Is(err, jwt.ErrTooManyAttempts):
		return fiber.NewError(fiber.StatusTooManyRequests,
			"Too many failed attempts for this account. Try again later.")
	case errors.Is(err, service.ErrUnverifiedProvider):
		return fiber.NewError(fiber.StatusForbidden,
			"This provider did not verify your email address. Sign in with your password instead.")
	default:

		return err
	}
}
