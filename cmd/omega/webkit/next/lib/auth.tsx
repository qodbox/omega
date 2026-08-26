"use client"

import { createContext, use, useCallback, useEffect, useMemo, useState } from "react"
import type { ReactNode } from "react"

import { api, readTokens, writeTokens, type User } from "./api"

type AuthValue = {
  user: User | null
  loading: boolean
  signIn: (email: string, password: string) => Promise<void>
  signUp: (name: string, email: string, password: string) => Promise<void>
  signOut: () => Promise<void>
}

const AuthContext = createContext<AuthValue | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  const resolve = useCallback(async () => {
    if (!readTokens()) {
      setUser(null)
      setLoading(false)
      return
    }
    try {
      const { data } = await api.me()
      setUser(data)
    } catch {
      writeTokens(null)
      setUser(null)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    void resolve()
    // Another tab signing out must sign this one out too.
    const onChange = () => void resolve()
    window.addEventListener("omega:auth", onChange)
    window.addEventListener("storage", onChange)
    return () => {
      window.removeEventListener("omega:auth", onChange)
      window.removeEventListener("storage", onChange)
    }
  }, [resolve])

  const value = useMemo<AuthValue>(
    () => ({
      user,
      loading,
      signIn: async (email, password) => {
        const { data, tokens } = await api.login(email, password)
        writeTokens(tokens)
        setUser(data)
      },
      signUp: async (name, email, password) => {
        const { data, tokens } = await api.register(name, email, password)
        writeTokens(tokens)
        setUser(data)
      },
      signOut: async () => {
        await api.logout()
        setUser(null)
      },
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
