# Omega interface (Next.js)

The App Router interface for the Omega JSON API.

```bash
bun install --frozen-lockfile   # once bun.lock is committed
bun run dev        # http://localhost:5173, /api and /graphql rewritten to Omega
bun run typecheck  # next typegen && tsc --noEmit
bun run lint       # oxlint
bun run build && bun run start
```

`OMEGA_URL` retargets the rewrites (default `http://127.0.0.1:3000`).

## Configuration

`NEXT_PUBLIC_*` is inlined into the client bundle, so it must never hold a
secret. The only variable read is `NEXT_PUBLIC_API_URL`, the public API base
(empty: same origin through the rewrites, the recommended setup).

## Authentication

Shared with the Nuxt and React interfaces (`lib/api.ts`): the session is held
in `HttpOnly` cookies JavaScript never reads, unsafe methods echo the
`omega_csrf` cookie in `X-CSRF-Token`, a `401` triggers one shared refresh and
a single replay, `GET /api/auth/me` says who is signed in, and signing out in
one tab signs every tab out. `?next=` after sign-in only follows a path on this
origin.

## Security headers

- `proxy.ts` sets a Content-Security-Policy with a fresh nonce per request;
  Next stamps it on every script it renders, `'strict-dynamic'` lets those
  scripts load the chunks, and nothing else runs. Because the nonce changes on
  every request, the root layout opts into per-request rendering
  (`await connection()`): a page prerendered at build time would carry no
  nonce and be blocked.
- `next.config.ts` `headers()` adds `X-Content-Type-Options`,
  `Referrer-Policy`, `Permissions-Policy`, `X-Frame-Options`,
  `Cross-Origin-Opener-Policy` and, in production, HSTS. The CSP carries
  `frame-ancestors 'none'`.
- `security.ts` holds the policy for the three interfaces.

No component renders raw HTML (`dangerouslySetInnerHTML`): the docs pages are
typed blocks rendered as text.

## Installs

`.npmrc` and `bunfig.toml` pin exact versions and refuse dependency install
scripts. Commit the lockfile and install from it in CI
(`bun install --frozen-lockfile` or `npm ci`).
