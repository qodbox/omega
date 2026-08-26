package auth

import (
	"fmt"
	"sort"
	"strings"

	"github.com/markbates/goth"
	"github.com/markbates/goth/providers/discord"
	"github.com/markbates/goth/providers/facebook"
	"github.com/markbates/goth/providers/github"
	"github.com/markbates/goth/providers/gitlab"
	"github.com/markbates/goth/providers/google"
	"github.com/markbates/goth/providers/linkedin"
	"github.com/markbates/goth/providers/microsoftonline"
)

type Social struct {
	Name     string   `mapstructure:"name"`
	Key      string   `mapstructure:"key"`
	Secret   string   `mapstructure:"secret"`
	Callback string   `mapstructure:"callback"`
	Scopes   []string `mapstructure:"scopes"`
}

func (s Social) enabled() bool { return s.Key != "" && s.Secret != "" }

var builders = map[string]func(Social) goth.Provider{
	"google": func(s Social) goth.Provider {
		return google.New(s.Key, s.Secret, s.Callback, s.scopesOr("email", "profile")...)
	},
	"github": func(s Social) goth.Provider {
		return github.New(s.Key, s.Secret, s.Callback, s.scopesOr("user:email")...)
	},
	"gitlab": func(s Social) goth.Provider {
		return gitlab.New(s.Key, s.Secret, s.Callback, s.scopesOr("read_user")...)
	},
	"microsoftonline": func(s Social) goth.Provider {
		return microsoftonline.New(s.Key, s.Secret, s.Callback, s.scopesOr("openid", "email", "profile")...)
	},
	"facebook": func(s Social) goth.Provider {
		return facebook.New(s.Key, s.Secret, s.Callback, s.scopesOr("email")...)
	},
	"discord": func(s Social) goth.Provider {
		return discord.New(s.Key, s.Secret, s.Callback, s.scopesOr(discord.ScopeIdentify, discord.ScopeEmail)...)
	},
	"linkedin": func(s Social) goth.Provider {
		return linkedin.New(s.Key, s.Secret, s.Callback, s.scopesOr("r_liteprofile", "r_emailaddress")...)
	},
}

func (s Social) scopesOr(defaults ...string) []string {
	if len(s.Scopes) > 0 {
		return s.Scopes
	}
	return defaults
}

func UseProviders(socials []Social) ([]string, error) {
	var (
		providers []goth.Provider
		names     []string
	)

	for _, social := range socials {
		social.Name = strings.ToLower(strings.TrimSpace(social.Name))
		if !social.enabled() {
			continue
		}

		build, ok := builders[social.Name]
		if !ok {
			return nil, fmt.Errorf("auth: unknown provider %q (known: %s)", social.Name, strings.Join(known(), ", "))
		}
		if social.Callback == "" {
			return nil, fmt.Errorf("auth: provider %q has no callback URL", social.Name)
		}

		providers = append(providers, build(social))
		names = append(names, social.Name)
	}

	goth.ClearProviders()
	goth.UseProviders(providers...)

	sort.Strings(names)
	return names, nil
}

func known() []string {
	names := make([]string, 0, len(builders))
	for name := range builders {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
