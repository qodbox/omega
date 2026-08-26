"use client"

import { useState } from "react"
import { EyeIcon, EyeOffIcon } from "lucide-react"
import { useTranslation } from "@/lib/i18n"
import { useRouter } from "next/navigation"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Spinner } from "@/components/ui/spinner"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { ApiError } from "@/lib/api"
import { useAuth } from "@/lib/auth"
import { notifyError, notifySuccess } from "@/lib/notify"

export default function LoginPage() {
  const { signIn, signUp } = useAuth()
  const router = useRouter()

  const { t } = useTranslation()
  const [pending, setPending] = useState(false)
  const [fields, setFields] = useState<Record<string, string>>({})

  async function run(action: () => Promise<void>, success: string) {
    setPending(true)
    setFields({})
    try {
      await action()
      notifySuccess(t(success))
      void router.push("/dashboard")
    } catch (failure) {
      if (failure instanceof ApiError && failure.fields) setFields(failure.fields)
      notifyError(failure)
    } finally {
      setPending(false)
    }
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-muted/30 p-6">
      <title>{`${t("auth.signIn")} · Omega`}</title>
      <Card className="w-full max-w-sm">
        <CardHeader>
          <div className="mb-2 flex size-10 items-center justify-center rounded-lg bg-primary text-lg font-bold text-primary-foreground">
            Ω
          </div>
          <CardTitle>Omega</CardTitle>
          <CardDescription>{t("auth.title")}</CardDescription>
        </CardHeader>

        <CardContent>
          <Tabs defaultValue="signin" onValueChange={() => setFields({})}>
            <TabsList className="mb-4 w-full">
              <TabsTrigger value="signin" className="flex-1">{t("auth.signIn")}</TabsTrigger>
              <TabsTrigger value="signup" className="flex-1">{t("auth.signUp")}</TabsTrigger>
            </TabsList>

            <TabsContent value="signin">
              <form
                onSubmit={(event) => {
                  event.preventDefault()
                  const form = new FormData(event.currentTarget)
                  void run(
                    () => signIn(String(form.get("email")), String(form.get("password"))),
                    "errors.signedIn",
                  )
                }}
              >
                <FieldGroup className="gap-4">
                  <TextField
                    name="email"
                    label={t("auth.email")}
                    type="email"
                    placeholder={t("auth.emailPlaceholder")}
                    autoComplete="email"
                    error={fields.email}
                    defaultValue="admin@omega.test"
                  />
                  <TextField
                    name="password"
                    label={t("auth.password")}
                    type="password"
                    placeholder={t("auth.passwordPlaceholder")}
                    autoComplete="current-password"
                    error={fields.password}
                  />
                  <Button type="submit" disabled={pending} className="w-full">
                    {pending && <Spinner />}
                    {pending ? t("auth.signingIn") : t("auth.signIn")}
                  </Button>
                </FieldGroup>
              </form>
            </TabsContent>

            <TabsContent value="signup">
              <form
                onSubmit={(event) => {
                  event.preventDefault()
                  const form = new FormData(event.currentTarget)
                  void run(
                    () =>
                      signUp(
                        String(form.get("name")),
                        String(form.get("email")),
                        String(form.get("password")),
                      ),
                    "errors.accountCreated",
                  )
                }}
              >
                <FieldGroup className="gap-4">
                  <TextField
                    name="name"
                    label={t("auth.name")}
                    placeholder={t("auth.namePlaceholder")}
                    autoComplete="name"
                    error={fields.name}
                  />
                  <TextField
                    name="email"
                    label={t("auth.email")}
                    type="email"
                    placeholder={t("auth.emailPlaceholder")}
                    autoComplete="email"
                    error={fields.email}
                  />
                  <TextField
                    name="password"
                    label={t("auth.password")}
                    type="password"
                    placeholder={t("auth.passwordPlaceholder")}
                    autoComplete="new-password"
                    error={fields.password}
                  />
                  <Button type="submit" disabled={pending} className="w-full">
                    {pending && <Spinner />}
                    {pending ? t("auth.creating") : t("auth.signUp")}
                  </Button>
                </FieldGroup>
              </form>
            </TabsContent>
          </Tabs>
        </CardContent>
      </Card>
    </div>
  )
}

function TextField({
  name,
  label,
  type = "text",
  autoComplete,
  error,
  defaultValue,
  placeholder,
}: {
  name: string
  label: string
  type?: string
  autoComplete?: string
  error?: string
  defaultValue?: string
  placeholder?: string
}) {
  const { t } = useTranslation()
  const [revealed, setRevealed] = useState(false)
  const isPassword = type === "password"

  return (
    <Field data-invalid={Boolean(error)}>
      <FieldLabel htmlFor={name}>{label}</FieldLabel>
      <div className="relative">
        <Input
          id={name}
          name={name}
          type={isPassword && revealed ? "text" : type}
          defaultValue={defaultValue}
          placeholder={placeholder}
          autoComplete={autoComplete}
          aria-invalid={Boolean(error)}
          className={isPassword ? "pr-9" : undefined}
        />
        {isPassword && (
          <Button
            type="button"
            variant="ghost"
            size="icon-sm"
            tabIndex={-1}
            aria-pressed={revealed}
            aria-label={revealed ? t("auth.hidePassword") : t("auth.showPassword")}
            onClick={() => setRevealed((shown) => !shown)}
            className="absolute inset-y-0 right-1 my-auto text-ink-gray-5"
          >
            {revealed ? <EyeOffIcon /> : <EyeIcon />}
          </Button>
        )}
      </div>
      <FieldError>{error}</FieldError>
    </Field>
  )
}
