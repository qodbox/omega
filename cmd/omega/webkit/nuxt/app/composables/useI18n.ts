import fr from '~/locales/fr.json'

type Messages = Record<string, unknown>

// Only the default language ships in the bundle; the others are fetched
// at the moment they are picked.
const loaders: Record<string, () => Promise<{ default: Messages }>> = {
  en: () => import('~/locales/en.json'),
  es: () => import('~/locales/es.json'),
}

const bundles = reactive<Record<string, Messages>>({ fr })

// The documentation translations weigh more than the whole application's,
// and only serve /docs: they arrive with it.
const docLoaders: Record<string, () => Promise<{ default: Messages }>> = {
  fr: () => import('~/locales/fr.docs.json'),
  en: () => import('~/locales/en.docs.json'),
  es: () => import('~/locales/es.docs.json'),
}

const docsLoaded = new Set<string>()

export const locales = [
  { code: 'fr', name: 'Français' },
  { code: 'en', name: 'English' },
  { code: 'es', name: 'Español' },
]

const locale = ref('fr')

function lookup(messages: Messages, key: string): string | undefined {
  let node: unknown = messages
  for (const part of key.split('.')) {
    if (typeof node !== 'object' || node === null) return undefined
    node = (node as Messages)[part]
  }
  return typeof node === 'string' ? node : undefined
}

async function loadDocsFor(code: string) {
  if (docsLoaded.has(code) || !docLoaders[code]) return
  Object.assign(bundles[code] ?? (bundles[code] = {}), (await docLoaders[code]()).default)
  docsLoaded.add(code)
}

export function useI18n() {
  function t(key: string, fallback?: string | Record<string, unknown>, values?: Record<string, unknown>) {
    const named = typeof fallback === 'object' ? fallback : values
    const spare = typeof fallback === 'string' ? fallback : undefined

    // The source text beats the default language: an English reader must not be
    // handed French. French only holds the place while a bundle is in flight.
    const raw = lookup(bundles[locale.value] ?? {}, key)
      ?? spare
      ?? lookup(bundles.fr ?? {}, key)
      ?? key

    if (!named) return raw

    return raw.replace(/\{\{?(\w+)\}?\}/g, (match, name: string) =>
      name in named ? String(named[name]) : match)
  }

  async function setLocale(code: string) {
    if (code !== 'fr' && !loaders[code]) return

    if (!bundles[code]) {
      bundles[code] = (await loaders[code]!()).default
    }
    locale.value = code
    if (docsLoaded.size) await loadDocsFor(code)

    if (import.meta.client) {
      localStorage.setItem('omega.locale', code)
      document.documentElement.lang = code
    }
  }

  const loadDocs = () => loadDocsFor(locale.value)

  function restore() {
    if (!import.meta.client) return
    const stored = localStorage.getItem('omega.locale')
    const guess = stored ?? navigator.language.slice(0, 2)
    void setLocale(guess === 'fr' || loaders[guess] ? guess : 'fr')
  }

  return { t, locale, locales, setLocale, restore, loadDocs }
}
