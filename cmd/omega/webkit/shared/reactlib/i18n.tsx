"use client"

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react"

import fr from "@/locales/fr.json"

type Messages = Record<string, unknown>

// Only French is in the initial bundle: it is the default language, so it
// must be there on the first render. The others arrive on demand.
const loaders: Record<string, () => Promise<Messages>> = {
  fr: () => Promise.resolve(fr),
  en: () => import("@/locales/en.json").then((module) => module.default),
  es: () => import("@/locales/es.json").then((module) => module.default),
}

const bundles: Record<string, Messages> = { fr }

// The documentation translations weigh more than the whole application's
// and only serve /docs: they arrive with it.
const docLoaders: Record<string, () => Promise<Messages>> = {
  fr: () => import("@/locales/fr.docs.json").then((module) => module.default),
  en: () => import("@/locales/en.docs.json").then((module) => module.default),
  es: () => import("@/locales/es.docs.json").then((module) => module.default),
}

const docsLoaded = new Set<string>()

export async function loadDocTranslations(code: string) {
  if (docsLoaded.has(code) || !docLoaders[code]) return
  Object.assign(bundles[code] ?? (bundles[code] = {}), await docLoaders[code]())
  docsLoaded.add(code)
}

async function load(code: string) {
  if (bundles[code]) return
  const loader = loaders[code]
  if (!loader) return
  bundles[code] = await loader()

  if (docsLoaded.size) await loadDocTranslations(code)
}

export const languages = [
  { code: "en", label: "English" },
  { code: "fr", label: "Français" },
  { code: "es", label: "Español" },
] as const

function lookup(messages: Messages, key: string): string | undefined {
  let node: unknown = messages
  for (const part of key.split(".")) {
    if (typeof node !== "object" || node === null) return undefined
    node = (node as Messages)[part]
  }
  return typeof node === "string" ? node : undefined
}

type Options = { defaultValue?: string } & Record<string, unknown>

// The current language outside React, for code that translates without a component.
let current = "fr"

function translate(key: string, options?: Options | string) {
  const spare = typeof options === "string" ? options : options?.defaultValue
  const raw = lookup(bundles[current] ?? fr, key) ?? spare ?? key

  if (!options || typeof options === "string") return raw
  return raw.replace(/\{\{?(\w+)\}?\}/g, (match, name: string) =>
    name in options ? String(options[name]) : match,
  )
}

const i18n = {
  t: translate,
  get language() {
    return current
  },
}

export default i18n

const Context = createContext<{
  language: string
  setLanguage: (code: string) => Promise<void>
}>({ language: "fr", setLanguage: async () => {} })

export function I18nProvider({ children }: { children: ReactNode }) {
  const [language, setState] = useState("fr")
  const wanted = useRef("fr")

  // The language only switches once its bundle has landed, otherwise the
  // screen would show raw keys while it downloads.
  const apply = useCallback(async (code: string) => {
    wanted.current = code
    await load(code)
    // Two switches in quick succession can come back out of order.
    if (wanted.current !== code) return
    current = code
    setState(code)
    document.documentElement.lang = code
  }, [])

  useEffect(() => {
    const stored = localStorage.getItem("omega.locale")
    const guess = stored ?? navigator.language.slice(0, 2)
    void apply(loaders[guess] ? guess : "fr")
  }, [apply])

  const setLanguage = useCallback(
    async (code: string) => {
      if (!loaders[code]) return
      localStorage.setItem("omega.locale", code)
      await apply(code)
    },
    [apply],
  )

  const value = useMemo(() => ({ language, setLanguage }), [language, setLanguage])

  return <Context.Provider value={value}>{children}</Context.Provider>
}

// Same signature as react-i18next, so the pages do not change.
export function useTranslation() {
  const { language, setLanguage } = useContext(Context)

  const t = useCallback(
    (key: string, options?: Options | string) => translate(key, options),
    // The language is in the dependency key so a switch re-renders.
    [language],
  )

  return { t, i18n: { language, changeLanguage: setLanguage, resolvedLanguage: language } }
}

// loadDocTranslations enriches the bundles outside React: without this extra
// render, /docs would stay on the English labels from docs.ts.
export function useDocTranslations() {
  const { i18n } = useTranslation()
  const [, bump] = useState(0)

  useEffect(() => {
    let alive = true
    void loadDocTranslations(i18n.language).then(() => {
      if (alive) bump((count) => count + 1)
    })
    return () => {
      alive = false
    }
  }, [i18n.language])
}
