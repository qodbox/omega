import tailwindcss from '@tailwindcss/vite'

import { securityHeaders } from './security'

export default defineNuxtConfig({
  compatibilityDate: '2026-08-25',
  ssr: false,
  devtools: { enabled: false },
  css: ['~/assets/css/main.css'],
  vite: { plugins: [tailwindcss()] },
  nitro: { static: true },
  // Only what may be read by every visitor: NUXT_PUBLIC_API_BASE, empty means
  // same origin. Never put a secret under public.
  runtimeConfig: {
    public: { apiBase: '' },
  },
  // Applied wherever Nitro serves the app, and turned into _headers or
  // equivalent by the presets that support it. The CSP itself is computed
  // from the built page: see server/plugins/csp.ts. A plain static host needs
  // the same headers configured by hand (README).
  routeRules: {
    '/**': { headers: securityHeaders({ production: process.env.NODE_ENV === 'production' }) },
  },
  app: {
    head: {
      title: 'Omega',
      link: [{ rel: 'icon', href: '/favicon.svg' }],
    },
  },
  // A fixed port: without it Nuxt falls back to 3000, the API's own, and the
  // proxy points at itself. Set WEB_PORT to change it.
  devServer: { port: Number(process.env.WEB_PORT ?? 5173) },

  // If the dev server lands on the API port, the proxy points at itself and
  // nothing answers. Better to fail loudly.
  hooks: {
    listen(_server, listener) {
      const api = new URL(process.env.OMEGA_URL ?? 'http://127.0.0.1:3000')
      if (String(listener.port) === api.port) {
        console.error(
          `\n  Le serveur de dev a pris le port ${api.port}, celui de l'API.\n`
          + `  Le proxy tournerait en boucle. Liberez ce port, ou lancez :\n`
          + `      WEB_PORT=5174 bun run dev\n`,
        )
        process.exit(1)
      }
    },
  },

  $development: {
    vite: {
      server: {
        proxy: {
          '/api': process.env.OMEGA_URL ?? 'http://127.0.0.1:3000',
          '/graphql': process.env.OMEGA_URL ?? 'http://127.0.0.1:3000',
        },
      },
    },
  },
})
