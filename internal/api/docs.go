package api

import (
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"regexp"
	"strings"

	"github.com/gofiber/fiber/v3"
)

//go:embed assets
var assets embed.FS

const assetRoot = "assets/"

type Docs struct {
	Title string

	OpenAPIPath string

	GraphQLPath string

	AssetPrefix string
}

func (d Docs) Asset() fiber.Handler {
	return func(c fiber.Ctx) error {
		name := c.Params("file")

		if name == "" || strings.ContainsAny(name, `/\`) {
			return fiber.ErrNotFound
		}

		body, err := assets.ReadFile(assetRoot + name)
		if err != nil {
			return fiber.ErrNotFound
		}

		switch {
		case strings.HasSuffix(name, ".js"):
			c.Type("js", "utf-8")
		case strings.HasSuffix(name, ".css"):
			c.Type("css", "utf-8")
		default:
			c.Type("bin")
		}

		c.Set(fiber.HeaderCacheControl, "public, max-age=31536000, immutable")
		return c.Send(body)
	}
}

func (d Docs) SwaggerUI() fiber.Handler {
	return page(strings.NewReplacer(
		"{{title}}", d.Title,
		"{{assets}}", d.AssetPrefix,
		"{{spec}}", d.OpenAPIPath,
	).Replace(swaggerPage))
}

func (d Docs) GraphiQL() fiber.Handler {
	return page(strings.NewReplacer(
		"{{title}}", d.Title,
		"{{assets}}", d.AssetPrefix,
		"{{endpoint}}", d.GraphQLPath,
	).Replace(graphiqlPage))
}

// page serves a console with a policy that lets it run, and nothing else:
// scripts from this origin plus the page's own inline script (pinned by its
// hash), requests to this origin only, no framing.
func page(html string) fiber.Handler {
	policy := PageCSP(html)
	return func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentSecurityPolicy, policy)
		c.Type("html", "utf-8")
		return c.SendString(html)
	}
}

var inlineScript = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// PageCSP is the Content-Security-Policy of a console page.
func PageCSP(html string) string {
	scripts := []string{"'self'"}
	for _, match := range inlineScript.FindAllStringSubmatch(html, -1) {
		sum := sha256.Sum256([]byte(match[1]))
		scripts = append(scripts, "'sha256-"+base64.StdEncoding.EncodeToString(sum[:])+"'")
	}

	return strings.Join([]string{
		"default-src 'none'",
		"script-src " + strings.Join(scripts, " "),
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data: blob:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'self'",
	}, "; ")
}

const swaggerPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{title}} — REST</title>
<link rel="stylesheet" href="{{assets}}/swagger-ui.css">
<style>body{margin:0}</style>
</head>
<body>
<div id="swagger"></div>
<script src="{{assets}}/swagger-ui-bundle.js"></script>
<script src="{{assets}}/swagger-ui-standalone-preset.js"></script>
<script>
  // Signed in with the session cookies, a state-changing call needs the CSRF
  // token echoed in a header; a bearer token set through Authorize does not.
  function csrfToken() {
    var match = document.cookie.match(/(?:^|; )omega_csrf=([^;]*)/);
    return match ? decodeURIComponent(match[1]) : '';
  }
  window.ui = SwaggerUIBundle({
    requestInterceptor: function (request) {
      var token = csrfToken();
      if (token) { request.headers['X-CSRF-Token'] = token; }
      return request;
    },
    url: '{{spec}}',
    dom_id: '#swagger',
    deepLinking: true,
    persistAuthorization: true,
    tryItOutEnabled: true,
    filter: true,
    docExpansion: 'list',
    defaultModelsExpandDepth: 1,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    plugins: [SwaggerUIBundle.plugins.DownloadUrl],
    layout: 'StandaloneLayout',
  });
</script>
</body>
</html>`

const graphiqlPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{title}} — GraphQL</title>
<link rel="stylesheet" href="{{assets}}/graphiql.min.css">
<link rel="stylesheet" href="{{assets}}/graphiql-explorer.css">
<style>
  body, html, #graphiql { margin: 0; height: 100vh; overflow: hidden; }

  /* The Explorer plugin ships height:unset !important on its wrapper. That
     lets the field tree grow past the panel, so the panel scrolls *and* the
     wrapper inside it scrolls — the two pairs of bars. Giving the column a
     real height and one scrolling child puts it back to a single pair. */
  .graphiql-plugin { display: flex; flex-direction: column; overflow: hidden; }
  .graphiql-plugin > div { flex: 1; min-height: 0; }

  .docExplorerWrap {
    height: 100% !important;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  .graphiql-explorer-root {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    overflow-x: auto;
    overscroll-behavior: contain;
    padding-bottom: var(--px-16, 16px);
  }

  .graphiql-explorer-root, .graphiql-explorer-root * {
    overflow-wrap: anywhere;
    box-sizing: border-box;
    max-width: 100%;
  }

  .graphiql-explorer-root select { min-width: 0; flex: 1 1 auto; }

  .graphiql-explorer-root .graphiql-explorer-action {
    flex-wrap: wrap;
    gap: 4px;
    max-width: 100%;
  }

  .graphiql-explorer-root .graphiql-explorer-action select {
    flex: 1 1 auto;
    min-width: 0;
    margin: 0;
  }
  .graphiql-explorer-root .graphiql-explorer-node { max-width: 100%; }
</style>
</head>
<body>
<div id="graphiql">Loading…</div>
<script src="{{assets}}/react.production.min.js"></script>
<script src="{{assets}}/react-dom.production.min.js"></script>
<script src="{{assets}}/graphiql.min.js"></script>
<script src="{{assets}}/graphiql-explorer.umd.js"></script>
<script>
  var api = window.GraphiQL;
  var Console = (api && api.GraphiQL) || api;
  var endpoint = '{{endpoint}}';
  var csrf = (document.cookie.match(/(?:^|; )omega_csrf=([^;]*)/) || [])[1];
  var session = csrf ? { 'X-CSRF-Token': decodeURIComponent(csrf) } : {};

  var fetcher = api && api.createFetcher
    ? api.createFetcher({ url: endpoint, headers: session })
    : function (params, opts) {
        var headers = { 'Content-Type': 'application/json', 'Accept': 'application/json' };
        for (var name in session) { headers[name] = session[name]; }
        var extra = (opts && opts.headers) || {};
        for (var key in extra) { headers[key] = extra[key]; }
        return fetch(endpoint, {
          method: 'POST',
          headers: headers,
          credentials: 'same-origin',
          body: JSON.stringify(params),
        }).then(function (response) { return response.json(); });
      };

  var plugins = [];
  if (window.GraphiQLPluginExplorer) {
    plugins.push(window.GraphiQLPluginExplorer.explorerPlugin());
  }

  if (!Console) {
    document.getElementById('graphiql').textContent = 'GraphiQL failed to load.';
  } else {
    ReactDOM.createRoot(document.getElementById('graphiql')).render(
      React.createElement(Console, {
        fetcher: fetcher,
        plugins: plugins,
        defaultEditorToolsVisibility: true,
        defaultHeaders: JSON.stringify({ Authorization: 'Bearer ' }, null, 2),
      })
    );
  }
</script>
</body>
</html>`
