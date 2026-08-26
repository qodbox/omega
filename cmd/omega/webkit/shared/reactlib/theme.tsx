"use client"

import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from "react"

type Theme = "light" | "dark" | "system"

const Context = createContext<{ theme: Theme; setTheme: (next: Theme) => void }>({
  theme: "system",
  setTheme: () => {},
})

function apply(theme: Theme) {
  const dark =
    theme === "dark" ||
    (theme === "system" && window.matchMedia("(prefers-color-scheme: dark)").matches)
  document.documentElement.classList.toggle("dark", dark)
}

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setState] = useState<Theme>("system")

  useEffect(() => {
    const stored = (localStorage.getItem("omega.theme") as Theme | null) ?? "system"
    setState(stored)
    apply(stored)

    const media = window.matchMedia("(prefers-color-scheme: dark)")
    const onChange = () => apply(stored)
    media.addEventListener("change", onChange)
    return () => media.removeEventListener("change", onChange)
  }, [])

  const setTheme = useCallback((next: Theme) => {
    setState(next)
    localStorage.setItem("omega.theme", next)
    apply(next)
  }, [])

  const value = useMemo(() => ({ theme, setTheme }), [theme, setTheme])

  return <Context.Provider value={value}>{children}</Context.Provider>
}

// Same signature as next-themes.
export function useTheme() {
  return useContext(Context)
}
