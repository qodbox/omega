"use client"

import { useMemo, useState } from "react"
import { ListIcon } from "lucide-react"
import { useDocTranslations, useTranslation } from "@/lib/i18n"
import Link from "next/link"
import { useParams } from "next/navigation"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from "@/components/ui/sheet"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { docs, sections, type DocPage } from "@/lib/docs"
import { cn } from "@/lib/utils"

export default function DocsPage() {
  const { t } = useTranslation()
  useDocTranslations()

  const params = useParams<{ slug?: string[] }>()

  // Optional catch-all: with no segment, serve the first page rather than
  // redirecting — no visible jump on load.
  const slug = params.slug?.[0]
  const current = useMemo(
    () => docs.find((page) => page.slug === slug) ?? docs[0],
    [slug],
  )

  const index = docs.indexOf(current)
  const previous = docs[index - 1]
  const next = docs[index + 1]

  return (
    <div className="mx-auto flex max-w-6xl gap-10 px-4 py-10">
      <aside className="sticky top-20 hidden h-fit w-56 shrink-0 lg:block">
        <DocsNav slug={current.slug} />
      </aside>

      <article className="min-w-0 flex-1">
        <title>{`${t(`doc.${current.slug}.title`, { defaultValue: current.title })} · Omega`}</title>

        <DocsNavSheet slug={current.slug} />

        <Badge variant="secondary" className="mb-3">
          {t(`docSection.${current.section}`, {
            defaultValue: sections.find((section) => section.key === current.section)?.title,
          })}
        </Badge>
        <h1 className="text-4xl font-semibold tracking-tight">
          {t(`doc.${current.slug}.title`, { defaultValue: current.title })}
        </h1>
        <p className="mt-3 text-lg text-muted-foreground">
          {t(`doc.${current.slug}.lead`, { defaultValue: current.lead })}
        </p>

        <div className="mt-10 flex flex-col gap-10">
          {current.blocks.map((block, position) => (
            <Block key={position} block={block} slug={current.slug} index={position} />
          ))}
        </div>

        <Separator className="my-12" />

        <div className="flex items-center justify-between gap-4">
          {previous ? (
            <Button asChild variant="ghost">
              <Link href={`/docs/${previous.slug}`}>
                ← {t(`doc.${previous.slug}.title`, { defaultValue: previous.title })}
              </Link>
            </Button>
          ) : (
            <span />
          )}
          {next && (
            <Button asChild variant="ghost">
              <Link href={`/docs/${next.slug}`}>
                {t(`doc.${next.slug}.title`, { defaultValue: next.title })} →
              </Link>
            </Button>
          )}
        </div>
      </article>
    </div>
  )
}

function DocsNav({ slug, onNavigate }: { slug: string; onNavigate?: () => void }) {
  const { t } = useTranslation()

  return (
    <nav className="flex flex-col gap-6">
      {sections.map((section) => (
        <div key={section.key} className="flex flex-col gap-1">
          <p className="px-3 text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {t(`docSection.${section.key}`, { defaultValue: section.title })}
          </p>
          {docs
            .filter((page) => page.section === section.key)
            .map((page) => (
              <Link
                key={page.slug}
                href={`/docs/${page.slug}`}
                onClick={onNavigate}
                className={cn(
                  "rounded-md px-3 py-1.5 text-sm transition-colors hover:bg-accent",
                  page.slug === slug && "bg-accent font-medium",
                )}
              >
                {t(`doc.${page.slug}.title`, { defaultValue: page.title })}
              </Link>
            ))}
        </div>
      ))}
    </nav>
  )
}

function DocsNavSheet({ slug }: { slug: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <Button variant="outline" size="sm" className="mb-6 lg:hidden">
          <ListIcon />
          {t("docs.browse")}
        </Button>
      </SheetTrigger>
      <SheetContent side="left" className="w-72 overflow-y-auto">
        <SheetHeader>
          <SheetTitle>{t("nav.docs")}</SheetTitle>
          <SheetDescription>{t("docs.browse")}</SheetDescription>
        </SheetHeader>
        <div className="px-4 pb-6">
          <DocsNav slug={slug} onNavigate={() => setOpen(false)} />
        </div>
      </SheetContent>
    </Sheet>
  )
}

function Block({
  block,
  slug,
  index,
}: {
  block: DocPage["blocks"][number]
  slug: string
  index: number
}) {
  const { t } = useTranslation()
  const key = `doc.${slug}.b${index}`
  const heading = (fallback?: string) =>
    fallback ? t(`${key}.heading`, { defaultValue: fallback }) : undefined
  const prose = (fallback: string) => t(`${key}.body`, { defaultValue: fallback })

  if (block.kind === "text") {
    return (
      <section className="flex flex-col gap-3">
        {block.heading && <h2 className="text-2xl font-semibold">{heading(block.heading)}</h2>}
        <p className="leading-relaxed text-muted-foreground">{prose(block.body)}</p>
      </section>
    )
  }

  if (block.kind === "code") {
    return (
      <section className="flex flex-col gap-3">
        {block.heading && <h2 className="text-2xl font-semibold">{heading(block.heading)}</h2>}
        <Card className="overflow-hidden bg-muted/40 py-0">
          <CardContent className="p-0">
            <pre className="overflow-x-auto p-5 font-mono text-sm leading-relaxed">
              <code>{block.body}</code>
            </pre>
          </CardContent>
        </Card>
      </section>
    )
  }

  if (block.kind === "note") {
    return (
      <Alert>
        <AlertTitle>{heading(block.heading)}</AlertTitle>
        <AlertDescription>{prose(block.body)}</AlertDescription>
      </Alert>
    )
  }

  return (
    <section className="flex flex-col gap-3">
      {block.heading && <h2 className="text-2xl font-semibold">{heading(block.heading)}</h2>}
      <div className="rounded-md border">
        <Table>
          <TableHeader>
            <TableRow>
              {block.columns.map((column, position) => (
                <TableHead key={column}>
                  {t(`${key}.c${position}`, { defaultValue: column })}
                </TableHead>
              ))}
            </TableRow>
          </TableHeader>
          <TableBody>
            {block.rows.map((row, position) => (
              <TableRow key={position}>
                {row.map((cell, column) => (
                  <TableCell key={column} className={column === 0 ? "font-mono text-xs" : ""}>
                    {column === 0 ? cell : t(`${key}.r${position}c${column}`, { defaultValue: cell })}
                  </TableCell>
                ))}
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </section>
  )
}
