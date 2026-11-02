import type { components } from "./api-types"

export type User = components["schemas"]["user"]

export type Page<T> = {
  data: T[]
  meta: { total: number; page: number; per_page: number; last_page: number }
}

// The session lives in HttpOnly cookies the browser attaches on its own:
// no token is ever readable from JavaScript, so an injected script cannot
// walk away with the session. This module only ever sees the user.
//
//   omega_access   HttpOnly  sent on every /api call
//   omega_refresh  HttpOnly  sent to /api/auth only
//   omega_csrf     readable  echoed in X-CSRF-Token on unsafe methods
//
// It knows no framework: it exposes a subscription, and each interface makes
// it reactive its own way (React context, Vue ref, anything else).

const CSRF_COOKIE = "omega_csrf"
const CSRF_HEADER = "X-CSRF-Token"
const CHANNEL = "omega-auth"
const UNSAFE = new Set(["POST", "PUT", "PATCH", "DELETE"])

let base = "/api"

// Only the public API base URL is configurable, never a secret. It defaults to
// the same origin, which is what the dev proxies and a reverse proxy give.
export function configureApi(options: { baseUrl?: string }) {
  const url = options.baseUrl?.trim()
  if (url) base = url.replace(/\/+$/, "")
}

export function apiUrl(path: string) {
  return `${base}${path}`
}

export class ApiError extends Error {
  status: number
  fields?: Record<string, string>

  constructor(status: number, message: string, fields?: Record<string, string>) {
    super(message)
    this.status = status
    this.fields = fields
  }
}

// --- Signed-in state ---------------------------------------------------------

type Listener = (user: User | null) => void

const listeners = new Set<Listener>()
let current: User | null = null

export function subscribe(listener: Listener) {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

export function currentUser() {
  return current
}

function publish(user: User | null) {
  current = user
  for (const listener of listeners) listener(user)
}

// Other tabs only learn that something changed, never a token: on "signed-out"
// they drop the user, on "signed-in" they ask /auth/me themselves.
type AuthMessage = { type: "signed-in" | "signed-out" }

let channel: BroadcastChannel | null | undefined

function tabs(): BroadcastChannel | null {
  if (channel !== undefined) return channel
  if (typeof window === "undefined" || typeof BroadcastChannel === "undefined") {
    return (channel = null)
  }

  channel = new BroadcastChannel(CHANNEL)
  channel.onmessage = (event: MessageEvent<AuthMessage>) => {
    if (event.data?.type === "signed-out") publish(null)
    if (event.data?.type === "signed-in") void restoreSession({ force: true })
  }
  return channel
}

function announce(type: AuthMessage["type"]) {
  tabs()?.postMessage({ type } satisfies AuthMessage)
}

function signedOut() {
  const wasSignedIn = current !== null
  publish(null)
  if (wasSignedIn) announce("signed-out")
}

// --- CSRF --------------------------------------------------------------------

export function readCsrf(): string | null {
  if (typeof document === "undefined") return null
  for (const pair of document.cookie.split(";")) {
    const [name, ...value] = pair.trim().split("=")
    if (name === CSRF_COOKIE) return decodeURIComponent(value.join("="))
  }
  return null
}

let csrfLoading: Promise<void> | null = null

// GET /auth/csrf (re)sets omega_csrf. Called once at boot, then again only when
// the cookie went missing or the server refused the one we sent.
export function ensureCsrf(force = false): Promise<void> {
  if (!force && readCsrf()) return Promise.resolve()

  csrfLoading ??= fetch(apiUrl("/auth/csrf"), { credentials: "include" })
    .then(() => undefined)
    .catch(() => undefined)
    .finally(() => {
      csrfLoading = null
    })
  return csrfLoading
}

// --- Transport ---------------------------------------------------------------

type Options = RequestInit & { auth?: boolean; retry?: boolean; csrfRetry?: boolean }

async function send(path: string, init: RequestInit): Promise<Response> {
  const method = (init.method ?? "GET").toUpperCase()
  const headers = new Headers(init.headers)

  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json")
  }
  if (UNSAFE.has(method)) {
    await ensureCsrf()
    const csrf = readCsrf()
    if (csrf) headers.set(CSRF_HEADER, csrf)
  }

  return fetch(apiUrl(path), { ...init, method, headers, credentials: "include" })
}

let refreshing: Promise<boolean> | null = null

// One rotation in flight at a time, even when ten requests fail together.
function refresh(): Promise<boolean> {
  refreshing ??= send("/auth/refresh", { method: "POST" })
    .then((response) => response.ok)
    .catch(() => false)
    .finally(() => {
      refreshing = null
    })
  return refreshing
}

function isCsrfRefusal(response: Response, body: unknown) {
  const message = (body as { error?: string } | null)?.error ?? ""
  return response.status === 403 && /csrf/i.test(message)
}

export async function request<T>(path: string, options: Options = {}): Promise<T> {
  const { auth = true, retry = true, csrfRetry = true, ...init } = options

  const response = await send(path, init)

  // An expired access cookie is the normal case, not a failure: rotate once
  // and replay. Only a dead refresh cookie ends the session.
  if (response.status === 401 && auth && retry) {
    if (await refresh()) return request<T>(path, { ...options, retry: false })
    signedOut()
  }

  if (response.status === 204) return undefined as T

  const body: unknown = await response.json().catch(() => ({}))

  // A stale CSRF cookie (another tab signed in, the server rotated it): fetch
  // a fresh one and replay once.
  if (csrfRetry && isCsrfRefusal(response, body)) {
    await ensureCsrf(true)
    return request<T>(path, { ...options, csrfRetry: false })
  }

  if (!response.ok) {
    throw new ApiError(
      response.status,
      (body as { error?: string }).error ?? `HTTP ${response.status}`,
      (body as { errors?: Record<string, string> }).errors,
    )
  }
  return body as T
}

// --- Session -----------------------------------------------------------------

// Cookie mode answers {token_type: "Cookie", expires_in, user}. "data" is read
// too so the client keeps working against a backend that still nests it there.
type SessionBody = { token_type?: string; expires_in?: number; user?: User; data?: User }

let restoring: Promise<User | null> | null = null
let restored = false

// "Who am I" comes from GET /auth/me, never from something stored locally.
export function restoreSession(options: { force?: boolean } = {}): Promise<User | null> {
  tabs()
  if (restored && !options.force) return Promise.resolve(current)

  restoring ??= ensureCsrf()
    .then(() => request<{ data: User }>("/auth/me"))
    .then((answer) => answer.data)
    .catch(() => null)
    .then((user) => {
      restored = true
      publish(user)
      return user
    })
    .finally(() => {
      restoring = null
    })
  return restoring
}

async function openSession(path: string, payload: Record<string, string>) {
  const body = await request<SessionBody>(path, {
    method: "POST",
    auth: false,
    body: JSON.stringify(payload),
  })

  const user = body.user ?? body.data ?? (await request<{ data: User }>("/auth/me")).data
  restored = true
  publish(user)
  announce("signed-in")
  return user
}

export const api = {
  login: (email: string, password: string) => openSession("/auth/login", { email, password }),

  register: (name: string, email: string, password: string) =>
    openSession("/auth/register", { name, email, password }),

  me: () => request<{ data: User }>("/auth/me"),

  logout: async () => {
    // The server clears the three cookies and revokes the refresh token.
    await request<void>("/auth/logout", { method: "POST", retry: false }).catch(() => undefined)
    restored = true
    publish(null)
    announce("signed-out")
  },

  users: (params: Record<string, string | number | undefined>) => {
    const query = new URLSearchParams()
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== "") query.set(key, String(value))
    }
    return request<Page<User>>(`/users?${query}`)
  },

  user: (id: number | string) => request<{ data: User }>(`/users/${encodeURIComponent(String(id))}`),
}

// --- Realtime ----------------------------------------------------------------

// Both authenticate with the access cookie: no token ever travels in a URL,
// where it would land in proxy logs and the browser history.
export function openEvents(channels: string[] = []): EventSource {
  const query = channels.length ? `?${new URLSearchParams({ channels: channels.join(",") })}` : ""
  return new EventSource(apiUrl(`/events${query}`), { withCredentials: true })
}

export function openSocket(): WebSocket {
  const url = new URL(apiUrl("/ws"), window.location.href)
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:"
  return new WebSocket(url)
}

// --- Redirects after sign-in -------------------------------------------------

// ?next= comes from the address bar, so anyone can craft it. Only a path on
// this very origin is followed: "//evil.test", "/\\evil.test", "https://..."
// and "javascript:" all fall back.
export function safeNext(raw: unknown, fallback = "/dashboard"): string {
  if (typeof raw !== "string" || !raw.startsWith("/") || raw.startsWith("//")) return fallback
  // eslint-disable-next-line no-control-regex
  if (/[\\\u0000-\u001f\u007f]/.test(raw)) return fallback

  try {
    const origin = "http://omega.invalid"
    const url = new URL(raw, origin)
    if (url.origin !== origin || url.pathname.startsWith("/login")) return fallback
    return `${url.pathname}${url.search}${url.hash}`
  } catch {
    return fallback
  }
}
