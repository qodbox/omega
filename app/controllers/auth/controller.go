package auth

import (
	"errors"

	"github.com/gofiber/fiber/v3"

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

func (a *Controller) Register(c fiber.Ctx) error {
	body, err := validation.Bind[request.Register](c)
	if err != nil {
		return err
	}

	session, err := a.auth.Register(c.Context(), *body)
	if err != nil {
		return translate(err)
	}
	return answerSession(c, a.guard, fiber.StatusCreated, session)
}

func (a *Controller) Login(c fiber.Ctx) error {
	body, err := validation.Bind[request.Login](c)
	if err != nil {
		return err
	}

	session, err := a.auth.Login(c.Context(), *body, c.IP())
	if err != nil {
		return translate(err)
	}
	return answerSession(c, a.guard, fiber.StatusOK, session)
}

// Refresh reads the refresh token from the omega_refresh cookie in cookie
// mode (the request has no body then), or from the body for a bearer client.
// The CSRF check on the cookie path runs in the route's middleware.
func (a *Controller) Refresh(c fiber.Ctx) error {
	raw, fromCookie, err := a.refreshToken(c)
	if err != nil {
		return err
	}

	session, err := a.auth.Refresh(c.Context(), raw)
	if err != nil {
		if fromCookie {
			a.guard.ClearSession(c)
		}
		return translate(err)
	}

	if fromCookie && a.guard.CookieMode(c) {
		if err := a.guard.SetSession(c, session.Tokens); err != nil {
			return err
		}
		return c.JSON(cookieBody(a.guard, session))
	}
	return c.JSON(fiber.Map{"tokens": session.Tokens})
}

// Logout revokes the refresh token's login and, in cookie mode, expires the
// three cookies.
func (a *Controller) Logout(c fiber.Ctx) error {
	if a.guard.CookiesEnabled() && a.guard.RefreshToken(c) == "" && len(c.Body()) == 0 {
		// The refresh cookie already expired: nothing to revoke, only the
		// leftover cookies to clear.
		a.guard.ClearSession(c)
		return c.SendStatus(fiber.StatusNoContent)
	}

	raw, fromCookie, err := a.refreshToken(c)
	if err != nil {
		return err
	}

	a.auth.Logout(raw)
	if fromCookie || a.guard.CookiesEnabled() {
		a.guard.ClearSession(c)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// LogoutAll signs the caller out on every device: refresh tokens revoked,
// access tokens refused from the next request on.
func (a *Controller) LogoutAll(c fiber.Ctx) error {
	user, err := a.caller(c)
	if err != nil {
		return err
	}
	if err := a.auth.LogoutAll(c.Context(), user); err != nil {
		return err
	}
	if a.guard.CookiesEnabled() {
		a.guard.ClearSession(c)
	}
	return c.SendStatus(fiber.StatusNoContent)
}

// CSRF (re)sets the readable omega_csrf cookie. Public: the client calls it
// once at boot, before anyone signs in.
func (a *Controller) CSRF(c fiber.Ctx) error {
	if _, err := a.guard.EnsureCSRF(c); err != nil {
		return err
	}
	return c.SendStatus(fiber.StatusNoContent)
}

func (a *Controller) refreshToken(c fiber.Ctx) (string, bool, error) {
	if raw := a.guard.RefreshToken(c); raw != "" {
		return raw, true, nil
	}

	body, err := validation.Bind[request.Refresh](c)
	if err != nil {
		return "", false, err
	}
	return body.RefreshToken, false, nil
}

func (a *Controller) Me(c fiber.Ctx) error {
	user, err := a.caller(c)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"data": presenter.NewUser(user)})
}

func (a *Controller) ChangePassword(c fiber.Ctx) error {
	user, err := a.caller(c)
	if err != nil {
		return err
	}

	body, err := validation.Bind[request.ChangePassword](c)
	if err != nil {
		return err
	}

	tokens, err := a.auth.ChangePassword(c.Context(), user, *body)
	if err != nil {
		return translate(err)
	}
	if jwt.Transport(c) == jwt.TransportCookie {
		if err := a.guard.SetSession(c, tokens); err != nil {
			return err
		}
		return c.JSON(cookieBody(a.guard, &service.Session{User: user, Tokens: tokens}))
	}
	return c.JSON(fiber.Map{"tokens": tokens})
}

func (a *Controller) caller(c fiber.Ctx) (*models.User, error) {
	user, ok := a.guard.User(c).(*models.User)
	if !ok {
		return nil, fiber.NewError(fiber.StatusUnauthorized, "Authentication required.")
	}
	return user, nil
}

// answerSession hands a new session over: in cookies, with the user and no
// token in the body, or as the bearer body API clients have always read.
func answerSession(c fiber.Ctx, guard *jwt.Guard, status int, session *service.Session) error {
	if guard != nil && guard.CookieMode(c) {
		if err := guard.SetSession(c, session.Tokens); err != nil {
			return err
		}
		return c.Status(status).JSON(cookieBody(guard, session))
	}
	return c.Status(status).JSON(sessionResponse(session))
}

func cookieBody(guard *jwt.Guard, session *service.Session) fiber.Map {
	return fiber.Map{
		"token_type": "Cookie",
		"expires_in": int(guard.AccessTTL().Seconds()),
		"user":       presenter.NewUser(session.User),
	}
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
