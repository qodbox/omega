package auth

import (
	"github.com/gofiber/fiber/v3"

	service "omega/app/services/auth"
	oauth "omega/internal/auth"
)

type SocialController struct {
	auth      *service.Service
	guard     *oauth.Guard
	providers []string
}

func NewSocialController(service *service.Service, providers []string) *SocialController {
	return &SocialController{auth: service, providers: providers}
}

// WithGuard lets the callback answer in cookies, like a local sign-in.
func (s *SocialController) WithGuard(guard *oauth.Guard) *SocialController {
	s.guard = guard
	return s
}

func (s *SocialController) Providers(c fiber.Ctx) error {
	names := s.providers
	if names == nil {
		names = []string{}
	}
	return c.JSON(fiber.Map{"data": names})
}

func (s *SocialController) Redirect(c fiber.Ctx) error {
	if !s.knows(c.Params("provider")) {
		return fiber.NewError(fiber.StatusNotFound, "This provider is not enabled.")
	}
	return oauth.BeginOAuth(c, c.Params("provider"))
}

func (s *SocialController) Callback(c fiber.Ctx) error {
	if !s.knows(c.Params("provider")) {
		return fiber.NewError(fiber.StatusNotFound, "This provider is not enabled.")
	}

	external, err := oauth.CompleteOAuth(c, c.Params("provider"))
	if err != nil {
		return fiber.NewError(fiber.StatusUnauthorized, "The provider refused the sign-in.")
	}

	session, err := s.auth.SignInWithProvider(c.Context(), external)
	if err != nil {
		return translate(err)
	}
	return answerSession(c, s.guard, fiber.StatusOK, session)
}

func (s *SocialController) knows(name string) bool {
	for _, provider := range s.providers {
		if provider == name {
			return true
		}
	}
	return false
}
