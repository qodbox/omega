"use client"

import type { ReactNode } from "react"
import Link from "next/link"

import { DocsSearch } from "@/components/docs-search"
import { LanguageToggle } from "@/components/language-toggle"
import { MobileNav } from "@/components/mobile-nav"
import { ThemeToggle } from "@/components/theme-toggle"
import { Button } from "@/components/ui/button"
import { useAuth } from "@/lib/auth"
import { useTranslation } from "@/lib/i18n"

export default function SiteLayout({ children }: { children: ReactNode }) {
  const { t } = useTranslation()
  const { user } = useAuth()

  return (
    <div className="min-h-svh">
      <a
        href="#content"
        className="sr-only focus:not-sr-only focus:absolute focus:top-4 focus:left-4 focus:z-50 focus:rounded-md focus:bg-background focus:px-3 focus:py-2 focus:text-sm"
      >
        {t("nav.skip")}
      </a>

      <header className="sticky top-0 z-30 border-b border-outline-gray-1 bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-header max-w-6xl items-center gap-4 px-4">
          <MobileNav />

          <Link href="/" className="flex items-center gap-2 font-semibold text-ink-gray-9">
            <span className="flex size-7 items-center justify-center rounded-md bg-primary text-sm font-bold text-primary-foreground">
              Ω
            </span>
            Omega
          </Link>

          <nav className="ml-2 hidden gap-1 sm:flex">
            <Button asChild variant="ghost" size="sm">
              <Link href="/docs">{t("nav.docs")}</Link>
            </Button>
            <Button asChild variant="ghost" size="sm">
              <a href="/api/docs" target="_blank" rel="noreferrer">{t("nav.rest")}</a>
            </Button>
            <Button asChild variant="ghost" size="sm">
              <a href="/graphql" target="_blank" rel="noreferrer">{t("nav.graphql")}</a>
            </Button>
          </nav>

          <div className="ml-auto flex items-center gap-1">
            <DocsSearch />
            <LanguageToggle />
            <ThemeToggle />
            <Button asChild size="sm">
              <Link href={user ? "/dashboard" : "/login"}>
                {user ? t("nav.dashboard") : t("nav.signIn")}
              </Link>
            </Button>
          </div>
        </div>
      </header>

      <main id="content">{children}</main>
    </div>
  )
}
