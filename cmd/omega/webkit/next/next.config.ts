import type { NextConfig } from "next"

// Omega serves JSON only: send /api and /graphql to it in development.
// Set OMEGA_URL to change the port.
const omega = process.env.OMEGA_URL ?? "http://127.0.0.1:3000"

const config: NextConfig = {
  // Without this, a single icon import drags in part of the whole package.
  experimental: {
    optimizePackageImports: ["lucide-react"],
  },
  async rewrites() {
    return [
      { source: "/api/:path*", destination: `${omega}/api/:path*` },
      { source: "/graphql", destination: `${omega}/graphql` },
    ]
  },
}

export default config
