import type { components } from "./api-types"

export type User = components["schemas"]["user"]

export type Tokens = {
  access_token: string
  refresh_token: string
  token_type: string
  expires_in: number
}

export type Page<T> = {
  data: T[]
  meta: { total: number; page: number; per_page: number; last_page: number }
}

const BASE = "/api"
const STORE = "omega.tokens"

// This module knows no framework: it exposes a subscription, and each
// interface en fait ce qu'elle veut (contexte React, ref Vue, autre).
const listeners = new Set<(tokens: Tokens | null) => void>()

export function subscribe(listener: (tokens: Tokens | null) => void) {
  listeners.add(listener)
  return () => listeners.delete(listener)
}

export function readTokens(): Tokens | null {
  if (typeof localStorage === "undefined") return null
  const raw = localStorage.getItem(STORE)
  return raw ? (JSON.parse(raw) as Tokens) : null
}

export function writeTokens(tokens: Tokens | null) {
  if (typeof localStorage !== "undefined") {
    if (tokens) localStorage.setItem(STORE, JSON.stringify(tokens))
    else localStorage.removeItem(STORE)
  }

  for (const listener of listeners) listener(tokens)

  // Another tab signing out must sign this one out too.
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event("omega:auth"))
  }
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

let refreshing: Promise<Tokens | null> | null = null

async function refresh(): Promise<Tokens | null> {
  const current = readTokens()
  if (!current?.refresh_token) return null

  refreshing ??= fetch(`${BASE}/auth/refresh`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refresh_token: current.refresh_token }),
  })
    .then(async (response) => {
      if (!response.ok) return null
      const body = (await response.json()) as { tokens: Tokens }
      writeTokens(body.tokens)
      return body.tokens
    })
    .catch(() => null)
    .finally(() => {
      refreshing = null
    })

  return refreshing
}

type Options = RequestInit & { auth?: boolean; retry?: boolean }

export async function request<T>(path: string, options: Options = {}): Promise<T> {
  const { auth = true, retry = true, ...init } = options
  const headers = new Headers(init.headers)

  if (init.body && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json")
  }
  if (auth) {
    const tokens = readTokens()
    if (tokens) headers.set("Authorization", `Bearer ${tokens.access_token}`)
  }

  const response = await fetch(`${BASE}${path}`, { ...init, headers })

  // An expired access token is the normal case, not a failure: rotate once
  // and replay. Only a dead refresh token ends the session.
  if (response.status === 401 && auth && retry) {
    const renewed = await refresh()
    if (renewed) return request<T>(path, { ...options, retry: false })
    writeTokens(null)
  }

  if (response.status === 204) return undefined as T

  const body = await response.json().catch(() => ({}))

  if (!response.ok) {
    throw new ApiError(
      response.status,
      (body as { error?: string }).error ?? `HTTP ${response.status}`,
      (body as { errors?: Record<string, string> }).errors,
    )
  }
  return body as T
}

export const api = {
  login: (email: string, password: string) =>
    request<{ data: User; tokens: Tokens }>("/auth/login", {
      method: "POST",
      auth: false,
      body: JSON.stringify({ email, password }),
    }),

  register: (name: string, email: string, password: string) =>
    request<{ data: User; tokens: Tokens }>("/auth/register", {
      method: "POST",
      auth: false,
      body: JSON.stringify({ name, email, password }),
    }),

  me: () => request<{ data: User }>("/auth/me"),

  logout: async () => {
    const tokens = readTokens()
    if (tokens) {
      await request<void>("/auth/logout", {
        method: "POST",
        body: JSON.stringify({ refresh_token: tokens.refresh_token }),
      }).catch(() => undefined)
    }
    writeTokens(null)
  },

  users: (params: Record<string, string | number | undefined>) => {
    const query = new URLSearchParams()
    for (const [key, value] of Object.entries(params)) {
      if (value !== undefined && value !== "") query.set(key, String(value))
    }
    return request<Page<User>>(`/users?${query}`)
  },

  user: (id: number | string) => request<{ data: User }>(`/users/${id}`),
}
