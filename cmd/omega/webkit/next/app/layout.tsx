import type { Metadata } from "next"
import type { ReactNode } from "react"
import { connection } from "next/server"

import { ErrorBoundary } from "@/components/error-boundary"
import { Providers } from "@/components/providers"
import { Toaster } from "@/lib/toast"

import "./globals.css"

export const metadata: Metadata = {
  title: "Omega",
  description: "Go JSON API framework",
}

export default async function RootLayout({ children }: { children: ReactNode }) {
  // The CSP nonce changes on every request, so pages render per request: a
  // page prerendered at build time would carry no nonce and be blocked.
  await connection()

  return (
    <html lang="fr" suppressHydrationWarning>
      <body>
        <ErrorBoundary>
          <Providers>{children}</Providers>
        </ErrorBoundary>
        <Toaster />
      </body>
    </html>
  )
}
