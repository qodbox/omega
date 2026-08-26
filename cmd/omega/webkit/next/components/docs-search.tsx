"use client"

import { lazy, Suspense, useEffect, useState } from "react"
import { SearchIcon } from "lucide-react"
import { useTranslation } from "@/lib/i18n"

import { Button } from "@/components/ui/button"

const DocsSearchDialog = lazy(() =>
  import("@/components/docs-search-dialog").then((module) => ({
    default: module.DocsSearchDialog,
  })),
)

const isApple = /mac|iphone|ipad/i.test(navigator.userAgent)

export function DocsSearch() {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key.toLowerCase() !== "k") return
      if (!(isApple ? event.metaKey : event.ctrlKey)) return
      event.preventDefault()
      setOpen((current) => !current)
    }

    window.addEventListener("keydown", onKeyDown)
    return () => window.removeEventListener("keydown", onKeyDown)
  }, [])

  return (
    <>
      <Button
        variant="outline"
        size="sm"
        onClick={() => setOpen(true)}
        aria-label={t("docs.search")}
        className="text-ink-gray-5 md:w-56 md:justify-start"
      >
        <SearchIcon />
        <span className="hidden md:inline">{t("docs.search")}</span>
      </Button>

      {open && (
        <Suspense fallback={null}>
          <DocsSearchDialog open onOpenChange={setOpen} />
        </Suspense>
      )}
    </>
  )
}
