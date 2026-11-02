# Omega interface (React + Vite)

A single-page app that talks to the Omega JSON API over HTTP only.

```bash
bun install --frozen-lockfile   # once bun.lock is committed
bun run dev        # http://localhost:5173, /api and /graphql proxied to Omega
bun run typecheck  # tsc -b
bun run lint       # oxlint
bun run build      # static output in dist/
bun run preview    # serves dist/ with the production headers
```

`OMEGA_URL` retargets the dev proxy (default `http://127.0.0.1:3000`).

## Configuration

Everything prefixed `VITE_` is inlined into the bundle every visitor downloads,
so it must never hold a secret. The only variable read is:

| Variable | Default | Meaning |
|---|---|---|
| `VITE_API_URL` | empty (same origin) | public base of the API, e.g. `https://api.example.com/api` |

Same origin is the recommended setup: serve `dist/` and proxy `/api` from the
same host. A separate API origin needs `app.cors.origins` to list this app,
and `AUTH_COOKIE_DOMAIN` set to the parent domain so the CSRF cookie is
readable here.

## Authentication

The session is held in cookies the browser manages; JavaScript never sees a
token (`src/lib/api.ts`):

- `omega_access` and `omega_refresh` are `HttpOnly`: an injected script cannot
  read them.
- every call uses `credentials: "include"`; `POST`, `PUT`, `PATCH` and `DELETE`
  echo the readable `omega_csrf` cookie in `X-CSRF-Token`. `GET /api/auth/csrf`
  sets it at boot.
- a `401` triggers one `POST /api/auth/refresh` (shared by every request that
  failed at the same time), then the request is replayed once.
- who is signed in comes from `GET /api/auth/me`; signing out in one tab signs
  every tab out (`BroadcastChannel("omega-auth")`, no token in the message).
- `openEvents()` and `openSocket()` authenticate with the cookie: no token in a
  URL.
- `?next=` after sign-in is followed only when it is a path on this origin.

## Security headers

`dist/index.html` carries a `<meta http-equiv="Content-Security-Policy">`
(`script-src 'self'`, no inline script), so the policy holds on any static
host. A `<meta>` cannot set `frame-ancestors`, `X-Content-Type-Options` and the
rest: the server in front of `dist/` must send them. `security.ts` is the
source of truth; `vite preview` sends exactly these headers.

nginx:

```nginx
server {
  listen 443 ssl;
  root /srv/web/dist;

  add_header Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; manifest-src 'self'; worker-src 'self' blob:; frame-ancestors 'none'" always;
  add_header X-Content-Type-Options "nosniff" always;
  add_header Referrer-Policy "strict-origin-when-cross-origin" always;
  add_header Permissions-Policy "camera=(), microphone=(), geolocation=(), payment=(), usb=(), browsing-topics=()" always;
  add_header X-Frame-Options "DENY" always;
  add_header Cross-Origin-Opener-Policy "same-origin" always;
  add_header Strict-Transport-Security "max-age=63072000; includeSubDomains" always;

  location /api/ { proxy_pass http://127.0.0.1:3000; }
  location / { try_files $uri /index.html; }
}
```

Caddy:

```caddy
example.com {
  root * /srv/web/dist
  header {
    Content-Security-Policy "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; form-action 'self'; manifest-src 'self'; worker-src 'self' blob:; frame-ancestors 'none'"
    X-Content-Type-Options nosniff
    Referrer-Policy strict-origin-when-cross-origin
    Permissions-Policy "camera=(), microphone=(), geolocation=(), payment=(), usb=(), browsing-topics=()"
    X-Frame-Options DENY
    Cross-Origin-Opener-Policy same-origin
    Strict-Transport-Security "max-age=63072000; includeSubDomains"
  }
  reverse_proxy /api/* 127.0.0.1:3000
  try_files {path} /index.html
  file_server
}
```

With `VITE_API_URL` on another origin, add that origin to `connect-src`.

No component renders raw HTML (`dangerouslySetInnerHTML`): the docs pages are
built from typed blocks in `src/lib/docs.ts`, rendered as text.

## Installs

`.npmrc` and `bunfig.toml` pin exact versions and refuse dependency install
scripts. Commit the lockfile and install from it in CI
(`bun install --frozen-lockfile` or `npm ci`).
