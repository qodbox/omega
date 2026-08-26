import type { Metadata } from "next"
import type { ReactNode } from "react"

import { ErrorBoundary } from "@/components/error-boundary"
import { Providers } from "@/components/providers"
import { Toaster } from "@/lib/toast"

import "./globals.css"

export const metadata: Metadata = {
  title: "Omega",
  description: "Go JSON API framework",
}

export default function RootLayout({ children }: { children: ReactNode }) {
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
