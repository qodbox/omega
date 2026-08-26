import path from "node:path"
import { defineConfig } from "vite"
import react from "@vitejs/plugin-react"
import tailwindcss from "@tailwindcss/vite"

// Like Nuxt and Next: the port is frozen at scaffolding time, OMEGA_URL replaces it.
const omega = process.env.OMEGA_URL ?? "http://127.0.0.1:3000"

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: { "@": path.resolve(import.meta.dirname, "./src") },
  },
  server: {
    port: Number(process.env.WEB_PORT ?? 5173),
    // Fail rather than slide onto the API port: the proxy would loop.
    strictPort: true,
    proxy: {
      "/api": { target: omega, changeOrigin: true },
      "/graphql": { target: omega, changeOrigin: true },
    },
  },
})
