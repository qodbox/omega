import { CompassIcon } from "lucide-react"
import { useTranslation } from "@/lib/i18n"
import { Link } from "react-router"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"

export function NotFoundPage() {
  const { t } = useTranslation()

  return (
    <div className="flex min-h-[70svh] items-center justify-center p-6">
      <title>{`${t("notFound.title")} · Omega`}</title>
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <CompassIcon />
          </EmptyMedia>
          <EmptyTitle>{t("notFound.title")}</EmptyTitle>
          <EmptyDescription>{t("notFound.body")}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button asChild size="sm">
            <Link to="/">{t("notFound.home")}</Link>
          </Button>
        </EmptyContent>
      </Empty>
    </div>
  )
}
