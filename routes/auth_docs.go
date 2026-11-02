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

	// One shape for both transports. Bearer mode (cookies disabled, or the
	// X-Omega-Auth: bearer header) answers data + tokens; cookie mode answers
	// user + token_type "Cookie" + expires_in and sets the cookies instead.
	sessionSchema = api.Object(map[string]*api.Schema{
		"data":   {Ref: "#/components/schemas/user", Description: "Bearer mode: the user"},
		"tokens": {Type: "object", Properties: tokensSchema.Properties, Description: "Bearer mode only"},
		"user":   {Ref: "#/components/schemas/user", Description: "Cookie mode: the user"},
		"token_type": {Type: "string", Enum: []string{"Cookie"},
			Description: "Cookie mode only: the tokens are in the omega_access and omega_refresh cookies"},
		"expires_in": {Type: "integer", Description: "Cookie mode only: access token lifetime, in seconds"},
	})

	emailSchema    = &api.Schema{Type: "string", Format: "email", MaxLength: 255}
	passwordSchema = &api.Schema{Type: "string", Format: "password", MinLength: 12, MaxLength: 72,
		Description: "12 characters at least, 72 bytes at most"}
	secretSchema = &api.Schema{Type: "string", Format: "password"}

	modeHeader = api.Parameter{
		Name: "X-Omega-Auth", In: "header",
		Description: "Send `bearer` to receive the tokens in the body instead of cookies (API clients).",
		Schema:      &api.Schema{Type: "string", Enum: []string{"bearer"}},
	}
	csrfHeader = api.Parameter{
		Name: "X-CSRF-Token", In: "header",
		Description: "Cookie mode: the value of the omega_csrf cookie. Required on POST, PUT, PATCH " +
			"and DELETE when the request authenticates with cookies; ignored with a bearer token.",
		Schema: api.String(),
	}
)

// cookieModeNote is appended to the descriptions of the endpoints that set or
// read the session cookies.
const cookieModeNote = " In cookie mode (the default for browsers) the answer sets omega_access " +
	"(HttpOnly, Path=/), omega_refresh (HttpOnly, SameSite=Strict, Path=/api/auth) and the readable " +
	"omega_csrf cookie, and the body carries no token."

func csrfFailure() api.Response {
	return api.Failure("Cookie mode: CSRF token missing or invalid.")
}

// optionalBody is a JSON body a cookie-mode client leaves out.
func optionalBody(schema *api.Schema) *api.RequestBody {
	body := api.Body(schema)
	body.Required = false
	return body
}

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
			Description: "Answers with the new user and a fresh token pair." + cookieModeNote,
			OperationID: "auth.register",
			Parameters:  []api.Parameter{modeHeader},
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
			Tags:    []string{"auth"},
			Summary: "Sign in",
			Description: "Exchanges an email and a password for a token pair. An unknown email and " +
				"a wrong password get the same answer." + cookieModeNote,
			OperationID: "auth.login",
			Parameters:  []api.Parameter{modeHeader},
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
				"is single-use: replaying one that has already been spent is refused, and revokes " +
				"every session of the same sign-in. Cookie mode reads the token from the " +
				"omega_refresh cookie (send no body, and the X-CSRF-Token header), answers the " +
				"cookie-mode session body and sets fresh cookies; a bearer client sends " +
				"refresh_token in the body and gets `tokens` back.",
			OperationID: "auth.refresh",
			Parameters:  []api.Parameter{csrfHeader},
			RequestBody: optionalBody(api.Object(map[string]*api.Schema{
				"refresh_token": api.String(),
			})),
			Responses: map[string]api.Response{
				"200": api.JSON("A new token pair (bearer) or the session (cookie)", api.Object(map[string]*api.Schema{
					"tokens":     {Type: "object", Properties: tokensSchema.Properties, Description: "Bearer mode only"},
					"user":       {Ref: "#/components/schemas/user", Description: "Cookie mode only"},
					"token_type": {Type: "string", Enum: []string{"Cookie"}, Description: "Cookie mode only"},
					"expires_in": {Type: "integer", Description: "Cookie mode only"},
				})),
				"401": api.Failure("Invalid refresh token"),
				"403": csrfFailure(),
			},
		}).Public(),
	})

	registry.Document("/auth/logout", api.PathItem{
		Post: (&api.Operation{
			Tags:    []string{"auth"},
			Summary: "Sign out",
			Description: "Retires the refresh token and every token rotated from the same sign-in. " +
				"Cookie mode reads it from the omega_refresh cookie (send no body, and the " +
				"X-CSRF-Token header) and expires the three cookies. The access token keeps " +
				"working until it expires; /auth/logout-all refuses it at once.",
			OperationID: "auth.logout",
			Parameters:  []api.Parameter{csrfHeader},
			RequestBody: optionalBody(api.Object(map[string]*api.Schema{
				"refresh_token": api.String(),
			})),
			Responses: map[string]api.Response{
				"204": api.Empty("Signed out"),
				"403": csrfFailure(),
			},
		}).Public(),
	})

	registry.Document("/auth/logout-all", api.PathItem{
		Post: &api.Operation{
			Tags:    []string{"auth"},
			Summary: "Sign out everywhere",
			Description: "Revokes every refresh token of the account and bumps its token version, so " +
				"every access token already handed out is refused from the next request on.",
			OperationID: "auth.logout_all",
			Parameters:  []api.Parameter{csrfHeader},
			Responses: map[string]api.Response{
				"204": api.Empty("Signed out on every device"),
				"401": api.Failure("Authentication required"),
				"403": csrfFailure(),
			},
		},
	})

	registry.Document("/auth/csrf", api.PathItem{
		Get: (&api.Operation{
			Tags:    []string{"auth"},
			Summary: "Get a CSRF token",
			Description: "Sets the readable omega_csrf cookie (keeping a valid one), so a cookie-mode " +
				"client can echo it in X-CSRF-Token. Call it once at boot; no sign-in needed.",
			OperationID: "auth.csrf",
			Responses:   map[string]api.Response{"204": api.Empty("The omega_csrf cookie is set")},
		}).Public(),
	})

	registry.Document("/auth/me", api.PathItem{
		Get: &api.Operation{
			Tags:    []string{"auth"},
			Summary: "The current user",
			Description: "Authenticates with the bearer token or, in cookie mode, the omega_access " +
				"cookie. A browser client reads its signed-in state from here.",
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
			Description: "Signs every other session out (refresh tokens revoked, access tokens " +
				"refused) and answers with a fresh pair, so the caller stays signed in on this " +
				"device only. A cookie-authenticated call gets fresh cookies and the cookie-mode body.",
			OperationID: "auth.password",
			Parameters:  []api.Parameter{csrfHeader},
			RequestBody: api.Body(api.Object(map[string]*api.Schema{
				"current_password": secretSchema,
				"new_password":     passwordSchema,
			}, "current_password", "new_password")),
			Responses: map[string]api.Response{
				"200": tokens("A new token pair"),
				"401": api.Failure("Authentication required"),
				"403": csrfFailure(),
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
			Tags:    []string{"auth"},
			Summary: "Finish an OAuth sign-in",
			Description: "Where the provider sends the caller back. Answers what a local sign-in " +
				"answers: the token pair in bearer mode, the session cookies in cookie mode.",
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
