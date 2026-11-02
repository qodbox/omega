import { createContext, use, useEffect, useMemo, useState } from "react"
import type { ReactNode } from "react"

import { api, configureApi, currentUser, restoreSession, subscribe, type User } from "./api"

// Only a public base URL may come from the environment: everything VITE_* is
// shipped to every visitor.
configureApi({ baseUrl: import.meta.env.VITE_API_URL })

type AuthValue = {
  user: User | null
  loading: boolean
  signIn: (email: string, password: string) => Promise<void>
  signUp: (name: string, email: string, password: string) => Promise<void>
  signOut: () => Promise<void>
}

const AuthContext = createContext<AuthValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(currentUser)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    // The shared client owns the state (cookies, refresh, other tabs): this
    // provider only mirrors it.
    const unsubscribe = subscribe(setUser)
    void restoreSession().finally(() => setLoading(false))
    return unsubscribe
  }, [])

  const value = useMemo<AuthValue>(
    () => ({
      user,
      loading,
      signIn: async (email, password) => {
        await api.login(email, password)
      },
      signUp: async (name, email, password) => {
        await api.register(name, email, password)
      },
      signOut: () => api.logout(),
    }),
    [user, loading],
  )

  return <AuthContext value={value}>{children}</AuthContext>
}

export function useAuth() {
  const value = use(AuthContext)
  if (!value) throw new Error("useAuth must be used inside AuthProvider")
  return value
}
