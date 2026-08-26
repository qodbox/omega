"use client"

import { toast } from "@/lib/toast"
import i18n from "@/lib/i18n"

import { ApiError } from "./api"

export function describe(failure: unknown): { title: string; detail?: string } {
  const t = i18n.t.bind(i18n)

  if (failure instanceof ApiError) {
    if (failure.fields && Object.keys(failure.fields).length > 0) {
      return {
        title: t("errors.validation"),
        detail: Object.values(failure.fields).join(" "),
      }
    }
    switch (failure.status) {
      case 401:
        // A 401 while signing in means the credentials are wrong; anywhere
        // else it means the session died under the user.
        if (/credential/i.test(failure.message)) {
          return { title: t("errors.credentials") }
        }
        return { title: t("errors.unauthorized") }
      case 403:
        return { title: t("errors.forbidden") }
      case 404:
        return { title: t("errors.notFound") }
      case 429:
        return { title: t("errors.tooMany") }
      default:
        if (failure.status >= 500) {
          return { title: t("errors.server"), detail: `HTTP ${failure.status}` }
        }
        return { title: failure.message }
    }
  }

  // fetch rejects rather than resolving when the server is down or the
  // proxy answers 502 with no body — that is what "Failed to fetch" means.
  return { title: t("errors.unreachable") }
}

export function notifyError(failure: unknown) {
  const { title, detail } = describe(failure)
  toast.error(title, { description: detail })
}

export function notifySuccess(message: string, description?: string) {
  toast.success(message, { description })
}
