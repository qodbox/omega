import { useTranslation } from "@/lib/i18n"
import { Link } from "react-router"
import { ArrowRight, Boxes, Database, ShieldCheck, Zap } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

const featureIcons = [Database, ShieldCheck, Zap, Boxes]

const stack = [
  "Go 1.26",
  "Fiber v2",
  "GORM",
  "REST",
  "OpenAPI 3.1",
  "GraphQL",
  "JWT",
  "React 19",
  "shadcn/ui",
]

export function HomePage() {
  const { t } = useTranslation()

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-20 px-4 py-16">
      <section className="flex flex-col items-center gap-6 text-center">
        <Badge variant="secondary">{t("home.badge")}</Badge>
        <h1 className="max-w-3xl text-balance text-5xl font-semibold tracking-tight sm:text-6xl">
          {t("home.title")}
        </h1>
        <p className="max-w-2xl text-pretty text-lg text-muted-foreground">
          {t("home.lead")}
        </p>
        <div className="flex flex-wrap justify-center gap-3">
          <Button asChild size="lg">
            <Link to="/docs">
              {t("home.readDocs")} <ArrowRight className="size-4" />
            </Link>
          </Button>
          <Button asChild size="lg" variant="outline">
            <Link to="/dashboard">{t("home.openDemo")}</Link>
          </Button>
        </div>
      </section>

      <section>
        <Card className="overflow-hidden bg-muted/40">
          <CardContent className="p-0">
            <pre className="overflow-x-auto p-6 font-mono text-sm leading-relaxed">
              <code>
                <span className="text-muted-foreground">$</span> omega make:model Post{"\n"}
                <span className="text-muted-foreground">$</span> omega migrate{"\n"}
                {"\n"}
                <span className="text-muted-foreground">
                  GET /api/posts · POST /api/posts · GET /api/posts/:id{"\n"}
                  PATCH /api/posts/:id · DELETE /api/posts/:id{"\n"}+ OpenAPI + GraphQL
                </span>
              </code>
            </pre>
          </CardContent>
        </Card>
      </section>

      <section className="grid gap-5 sm:grid-cols-2">
        {featureIcons.map((Icon, index) => (
          <Card key={index}>
            <CardHeader>
              <div className="mb-2 flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
                <Icon className="size-4.5" />
              </div>
              <CardTitle className="text-base">{t(`home.f${index + 1}.title`)}</CardTitle>
              <CardDescription className="leading-relaxed">
                {t(`home.f${index + 1}.body`)}
              </CardDescription>
            </CardHeader>
          </Card>
        ))}
      </section>

      <section className="flex flex-col items-center gap-4">
        <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground">
          {t("home.stack")}
        </h2>
        <div className="flex flex-wrap justify-center gap-2">
          {stack.map((item) => (
            <Badge key={item} variant="outline" className="px-3 py-1">
              {item}
            </Badge>
          ))}
        </div>
      </section>
    </div>
  )
}
