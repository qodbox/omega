"use client"

import { useState } from "react"
import { MenuIcon } from "lucide-react"
import { useTranslation } from "@/lib/i18n"
import Link from "next/link"

import { Button } from "@/components/ui/button"
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import { useAuth } from "@/lib/auth"

export function MobileNav() {
  const { t } = useTranslation()
  const { user } = useAuth()
  const [open, setOpen] = useState(false)

  const links = [
    { label: t("nav.docs"), to: "/docs", external: false },
    { label: user ? t("nav.dashboard") : t("nav.signIn"), to: user ? "/dashboard" : "/login", external: false },
    { label: t("nav.rest"), to: "/api/docs", external: true },
    { label: t("nav.graphql"), to: "/graphql", external: true },
  ]

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button variant="ghost" size="icon" className="sm:hidden" aria-label={t("nav.menu")}>
          <MenuIcon />
        </Button>
      </SheetTrigger>
      <SheetContent side="left" className="w-72">
        <SheetHeader>
          <SheetTitle>Omega</SheetTitle>
          <SheetDescription>{t("nav.menu")}</SheetDescription>
        </SheetHeader>
        <nav className="flex flex-col gap-1 px-4">
          {links.map((link) => (
            <SheetClose asChild key={link.to}>
              {link.external ? (
                <a
                  href={link.to}
                  target="_blank"
                  rel="noreferrer"
                  className="rounded-md px-3 py-2 text-sm transition-colors hover:bg-muted"
                >
                  {link.label}
                </a>
              ) : (
                <Link
                  href={link.to}
                  className="rounded-md px-3 py-2 text-sm transition-colors hover:bg-muted"
                >
                  {link.label}
                </Link>
              )}
            </SheetClose>
          ))}
        </nav>
      </SheetContent>
    </Sheet>
  )
}
