"use client"

import { Component, Suspense, type ReactNode } from "react"
import { RotateCcwIcon, TriangleAlertIcon } from "lucide-react"
import { useTranslation } from "@/lib/i18n"

import { Button } from "@/components/ui/button"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"

export class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  render() {
    if (this.state.failed) {
      return (
        <Suspense fallback={null}>
          <CrashScreen />
        </Suspense>
      )
    }
    return this.props.children
  }
}

function CrashScreen() {
  const { t } = useTranslation()

  return (
    <div className="flex min-h-svh items-center justify-center p-6">
      <Empty>
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <TriangleAlertIcon />
          </EmptyMedia>
          <EmptyTitle>{t("crash.title")}</EmptyTitle>
          <EmptyDescription>{t("crash.body")}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
            <RotateCcwIcon />
            {t("crash.reload")}
          </Button>
        </EmptyContent>
      </Empty>
    </div>
  )
}
