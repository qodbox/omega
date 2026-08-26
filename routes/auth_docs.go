package routes

import (
	"strings"

	"omega/internal/api"
)

var (
	tokensSchema = api.Object(map[string]*api.Schema{
		"access_token":  api.String(),
		"refresh_token": api.String(),
		"token_type":    {Type: "string", Examples: []any{"Bearer"}},
		"expires_in":    {Type: "integer", Description: "Access token lifetime, in seconds"},
	})

	sessionSchema = api.Object(map[string]*api.Schema{
		"data":   api.Ref("user"),
		"tokens": tokensSchema,
	})

	emailSchema    = &api.Schema{Type: "string", Format: "email", MaxLength: 255}
	passwordSchema = &api.Schema{Type: "string", Format: "password", MinLength: 8, MaxLength: 72}
	secretSchema   = &api.Schema{Type: "string", Format: "password"}
)

func validationFailure() api.Response {
	return api.JSON("Validation failed", api.Object(map[string]*api.Schema{
		"error":  api.String(),
		"status": api.Integer(),
		"errors": api.Dictionary(api.String()),
	}))
}

func session(description string) api.Response { return api.JSON(description, sessionSchema) }

func tokens(description string) api.Response {
	return api.JSON(description, api.Object(map[string]*api.Schema{"tokens": tokensSchema}))
}

func documentAuth(registry *api.Registry, providers []string) {
	registry.Document("/auth/register", api.PathItem{
		Post: (&api.Operation{
			Tags:        []string{"auth"},
			Summary:     "Create an account",
			Description: "Answers with the new user and a fresh token pair.",
			OperationID: "auth.register",
			RequestBody: api.Body(api.Object(map[string]*api.Schema{
				"name":     {Type: "string", MaxLength: 120},
				"email":    emailSchema,
				"password": passwordSchema,
			}, "name", "email", "password")),
			Responses: map[string]api.Response{
				"201": session("The account and its tokens"),
				"422": validationFailure(),
			},
		}).Public(),
	})

	registry.Document("/auth/login", api.PathItem{
		Post: (&api.Operation{
			Tags:        []string{"auth"},
			Summary:     "Sign in",
			Description: "Exchanges an email and a password for a token pair.",
			OperationID: "auth.login",
			RequestBody: api.Body(api.Object(map[string]*api.Schema{
				"email":    emailSchema,
				"password": secretSchema,
			}, "email", "password")),
			Responses: map[string]api.Response{
				"200": session("The user and their tokens"),
				"401": api.Failure("Invalid credentials"),
				"422": validationFailure(),
			},
		}).Public(),
	})

	registry.Document("/auth/refresh", api.PathItem{
		Post: (&api.Operation{
			Tags:    []string{"auth"},
			Summary: "Rotate the tokens",
			Description: "Spends a refresh token and answers with a new pair. A refresh token " +
				"is single-use: replaying one that has already been spent is refused.",
			OperationID: "auth.refresh",
			RequestBody: api.Body(api.Object(map[string]*api.Schema{
				"refresh_token": api.String(),
			}, "refresh_token")),
			Responses: map[string]api.Response{
				"200": tokens("A new token pair"),
				"401": api.Failure("Invalid refresh token"),
			},
		}).Public(),
	})

	registry.Document("/auth/logout", api.PathItem{
		Post: (&api.Operation{
			Tags:    []string{"auth"},
			Summary: "Sign out",
			Description: "Retires the refresh token. The access token keeps working until it " +
				"expires, which is the trade a stateless token makes.",
			OperationID: "auth.logout",
			RequestBody: api.Body(api.Object(map[string]*api.Schema{
				"refresh_token": api.String(),
			}, "refresh_token")),
			Responses: map[string]api.Response{"204": api.Empty("Signed out")},
		}).Public(),
	})

	registry.Document("/auth/me", api.PathItem{
		Get: &api.Operation{
			Tags:        []string{"auth"},
			Summary:     "The current user",
			OperationID: "auth.me",
			Responses: map[string]api.Response{
				"200": api.JSON("The caller", api.Object(map[string]*api.Schema{"data": api.Ref("user")})),
				"401": api.Failure("Authentication required"),
			},
		},
	})

	registry.Document("/auth/password", api.PathItem{
		Post: &api.Operation{
			Tags:    []string{"auth"},
			Summary: "Change the password",
			Description: "Signs every other session out and answers with a fresh pair, so the " +
				"caller stays signed in on this device only.",
			OperationID: "auth.password",
			RequestBody: api.Body(api.Object(map[string]*api.Schema{
				"current_password": secretSchema,
				"new_password":     passwordSchema,
			}, "current_password", "new_password")),
			Responses: map[string]api.Response{
				"200": tokens("A new token pair"),
				"401": api.Failure("Authentication required"),
				"422": validationFailure(),
			},
		},
	})

	registry.Document("/auth/providers", api.PathItem{
		Get: (&api.Operation{
			Tags:        []string{"auth"},
			Summary:     "The enabled OAuth providers",
			Description: "Names usable in /auth/{provider}. Enabled here: " + providerList(providers) + ".",
			OperationID: "auth.providers",
			Responses: map[string]api.Response{
				"200": api.JSON("The provider names", api.Object(map[string]*api.Schema{
					"data": api.Array(api.String()),
				})),
			},
		}).Public(),
	})

	provider := api.PathParameter("provider", "", providerSchema(providers))

	registry.Document("/auth/{provider}", api.PathItem{
		Parameters: []api.Parameter{provider},
		Get: (&api.Operation{
			Tags:        []string{"auth"},
			Summary:     "Start an OAuth sign-in",
			Description: "Redirects to the provider's consent screen. Open it in a browser: it answers a 302, not JSON.",
			OperationID: "auth.provider.redirect",
			Responses: map[string]api.Response{
				"302": api.Empty("Redirect to the provider"),
				"404": api.Failure("This provider is not enabled"),
			},
		}).Public(),
	})

	registry.Document("/auth/{provider}/callback", api.PathItem{
		Parameters: []api.Parameter{provider},
		Get: (&api.Operation{
			Tags:        []string{"auth"},
			Summary:     "Finish an OAuth sign-in",
			Description: "Where the provider sends the caller back. Answers the same token pair a local sign-in produces.",
			OperationID: "auth.provider.callback",
			Responses: map[string]api.Response{
				"200": session("The user and their tokens"),
				"401": api.Failure("The provider refused the sign-in"),
			},
		}).Public(),
	})
}

func providerSchema(providers []string) *api.Schema {
	schema := api.String()
	if len(providers) > 0 {
		schema.Enum = providers
	}
	return schema
}

func providerList(providers []string) string {
	if len(providers) == 0 {
		return "none"
	}
	return strings.Join(providers, ", ")
}
