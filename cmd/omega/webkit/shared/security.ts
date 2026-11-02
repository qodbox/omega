// One security policy for the three interfaces, read by their build or server
// configuration (nuxt.config, vite.config, next.config and proxy.ts). Never
// imported by browser code.

export type PolicyOptions = {
  // Next renders per request and stamps its inline scripts with this nonce.
  nonce?: string
  // sha256 hashes of the inline scripts a static build emits (Nuxt).
  scriptHashes?: string[]
  // Public API origin when it is not this one (NEXT_PUBLIC_API_URL & co).
  apiUrl?: string
  // Dev servers need eval for fast refresh and a websocket for HMR.
  dev?: boolean
  // A <meta> policy ignores frame-ancestors and the report directives.
  meta?: boolean
}

function origin(url: string | undefined): string | null {
  if (!url) return null
  try {
    return new URL(url).origin
  } catch {
    return null
  }
}

export function contentSecurityPolicy(options: PolicyOptions = {}): string {
  const api = origin(options.apiUrl)

  const script = ["'self'"]
  if (options.nonce) script.push(`'nonce-${options.nonce}'`, "'strict-dynamic'")
  for (const hash of options.scriptHashes ?? []) script.push(`'${hash}'`)
  if (options.dev) script.push("'unsafe-eval'")

  const connect = ["'self'"]
  if (api) connect.push(api, api.replace(/^http/, "ws"))
  if (options.dev) connect.push("ws:", "wss:")

  const directives: Record<string, string[]> = {
    "default-src": ["'self'"],
    "script-src": script,
    // Components set inline style attributes (radix, reka, sonner); scripts
    // stay locked down, which is where an injection does its damage.
    "style-src": ["'self'", "'unsafe-inline'"],
    "img-src": ["'self'", "data:", "blob:"],
    "font-src": ["'self'", "data:"],
    "connect-src": connect,
    "object-src": ["'none'"],
    "base-uri": ["'self'"],
    "form-action": ["'self'"],
    "manifest-src": ["'self'"],
    "worker-src": ["'self'", "blob:"],
  }
  if (!options.meta) directives["frame-ancestors"] = ["'none'"]

  return Object.entries(directives)
    .map(([name, values]) => [name, ...values].join(" "))
    .join("; ")
}

// Everything but the CSP, which depends on how each stack renders.
export function securityHeaders(options: { production?: boolean } = {}): Record<string, string> {
  const headers: Record<string, string> = {
    "X-Content-Type-Options": "nosniff",
    "Referrer-Policy": "strict-origin-when-cross-origin",
    "Permissions-Policy":
      "camera=(), microphone=(), geolocation=(), payment=(), usb=(), browsing-topics=()",
    "X-Frame-Options": "DENY",
    "Cross-Origin-Opener-Policy": "same-origin",
  }
  // Only meaningful over HTTPS; a browser ignores it on plain HTTP.
  if (options.production) {
    headers["Strict-Transport-Security"] = "max-age=63072000; includeSubDomains"
  }
  return headers
}
