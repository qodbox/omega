package auth

import (
	"github.com/gofiber/fiber/v3"

	service "omega/app/services/auth"
	oauth "omega/internal/auth"
)

type SocialController struct {
	auth      *service.Service
	providers []string
}

func NewSocialController(service *service.Service, providers []string) *SocialController {
	return &SocialController{auth: service, providers: providers}
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
	return c.JSON(sessionResponse(session))
}

func (s *SocialController) knows(name string) bool {
	for _, provider := range s.providers {
		if provider == name {
			return true
		}
	}
	return false
}
