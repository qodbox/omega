import tailwindcss from '@tailwindcss/vite'

export default defineNuxtConfig({
  compatibilityDate: '2026-08-25',
  ssr: false,
  devtools: { enabled: false },
  css: ['~/assets/css/main.css'],
  vite: { plugins: [tailwindcss()] },
  nitro: { static: true },
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
