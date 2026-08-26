"use client"

import { useDocTranslations, useTranslation } from "@/lib/i18n"
import { useRouter } from "next/navigation"

import {
  CommandDialog,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@/components/ui/command"
import { docs, sections } from "@/lib/docs"

export function DocsSearchDialog({
  open,
  onOpenChange,
}: {
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const { t } = useTranslation()
  useDocTranslations()

  const router = useRouter()

  return (
    <CommandDialog
      open={open}
      onOpenChange={onOpenChange}
      title={t("docs.search")}
      description={t("docs.browse")}
    >
      <CommandInput placeholder={t("docs.search")} />
      <CommandList>
        <CommandEmpty>{t("docs.noResult")}</CommandEmpty>
        {sections.map((section) => {
          const pages = docs.filter((page) => page.section === section.key)
          if (pages.length === 0) return null

          return (
            <CommandGroup
              key={section.key}
              heading={t(`docSection.${section.key}`, { defaultValue: section.title })}
            >
              {pages.map((page) => {
                const title = t(`doc.${page.slug}.title`, { defaultValue: page.title })
                const lead = t(`doc.${page.slug}.lead`, { defaultValue: page.lead })

                return (
                  <CommandItem
                    key={page.slug}
                    value={page.slug}
                    keywords={[title, lead, ...page.blocks.map((block) => block.heading ?? "")]}
                    onSelect={() => {
                      onOpenChange(false)
                      void router.push(`/docs/${page.slug}`)
                    }}
                  >
                    <div className="flex min-w-0 flex-col gap-0.5">
                      <span className="truncate">{title}</span>
                      <span className="truncate text-xs text-muted-foreground">{lead}</span>
                    </div>
                  </CommandItem>
                )
              })}
            </CommandGroup>
          )
        })}
      </CommandList>
    </CommandDialog>
  )
}
