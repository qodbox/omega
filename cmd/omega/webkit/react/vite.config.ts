import path from "node:path"
import { defineConfig, loadEnv, type Plugin } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

import { contentSecurityPolicy, securityHeaders } from "./security.ts"

// Like Nuxt and Next: the port is frozen at scaffolding time, OMEGA_URL replaces it.
const omega = process.env.OMEGA_URL ?? "http://127.0.0.1:3000"

// The built index.html carries the policy as a <meta>, so it holds on any
// static host. A <meta> cannot set frame-ancestors: the server in front must
// still send the headers (see README). Build only: the dev server injects an
// inline fast-refresh preamble a strict policy would block.
function cspMeta(apiUrl: string | undefined): Plugin {
  return {
    name: "omega:csp-meta",
    apply: "build",
    transformIndexHtml: () => [
      {
        tag: "meta",
        attrs: {
          "http-equiv": "Content-Security-Policy",
          content: contentSecurityPolicy({ apiUrl, meta: true }),
        },
        injectTo: "head-prepend",
      },
    ],
  }
}

export default defineConfig(({ mode }) => {
  // Only VITE_* reaches the bundle, and only VITE_API_URL is meant to.
  const env = loadEnv(mode, process.cwd(), "VITE_")

  return {
    plugins: [react(), tailwindcss(), cspMeta(env.VITE_API_URL)],
    resolve: {
      alias: { "@": path.resolve(import.meta.dirname, "./src") },
    },
    server: {
      port: Number(process.env.WEB_PORT ?? 5173),
      // Fail rather than slide onto the API port: the proxy would loop.
      strictPort: true,
      headers: securityHeaders(),
      proxy: {
        "/api": { target: omega, changeOrigin: true },
        "/graphql": { target: omega, changeOrigin: true },
      },
    },
    // `vite preview` answers like the production server should.
    preview: {
      headers: {
        ...securityHeaders({ production: true }),
        "Content-Security-Policy": contentSecurityPolicy({ apiUrl: env.VITE_API_URL }),
      },
    },
  }
})
