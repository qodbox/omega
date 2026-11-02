import type { NextConfig } from "next"

import { securityHeaders } from "./security"

// Omega serves JSON only: send /api and /graphql to it in development.
// Set OMEGA_URL to change the port.
const omega = process.env.OMEGA_URL ?? "http://127.0.0.1:3000"

const config: NextConfig = {
  poweredByHeader: false,
  // Without this, a single icon import drags in part of the whole package.
  experimental: {
    optimizePackageImports: ["lucide-react"],
  },
  // The Content-Security-Policy carries a per-request nonce: see proxy.ts.
  async headers() {
    const headers = securityHeaders({ production: process.env.NODE_ENV === "production" })
    return [
      {
        source: "/:path*",
        headers: Object.entries(headers).map(([key, value]) => ({ key, value })),
      },
    ]
  },
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${omega}/api/:path*` },
      { source: "/graphql", destination: `${omega}/graphql` },
    ]
  },
}

export default config
