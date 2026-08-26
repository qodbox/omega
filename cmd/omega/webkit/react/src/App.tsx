import { lazy, Suspense } from "react"
import { useTranslation } from "@/lib/i18n"
import { BrowserRouter, Link, Navigate, Outlet, Route, Routes, useLocation } from "react-router"

import { AppSidebar } from "@/components/app-sidebar"
import { DocsSearch } from "@/components/docs-search"
import { ErrorBoundary } from "@/components/error-boundary"
import { LanguageToggle } from "@/components/language-toggle"
import { MobileNav } from "@/components/mobile-nav"
import { ThemeToggle } from "@/components/theme-toggle"
import { ThemeProvider } from "@/lib/theme"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import { TooltipProvider } from "@/components/ui/tooltip"
import { I18nProvider } from "@/lib/i18n"
import { Toaster } from "@/lib/toast"
import { AuthProvider, useAuth } from "@/lib/auth"

const DashboardPage = lazy(() =>
  import("@/pages/dashboard").then((module) => ({ default: module.DashboardPage })),
)
const DocsPage = lazy(() => import("@/pages/docs").then((module) => ({ default: module.DocsPage })))
const HomePage = lazy(() => import("@/pages/home").then((module) => ({ default: module.HomePage })))
const LoginPage = lazy(() =>
  import("@/pages/login").then((module) => ({ default: module.LoginPage })),
)
const UsersPage = lazy(() =>
  import("@/pages/users").then((module) => ({ default: module.UsersPage })),
)
const UserDetailPage = lazy(() =>
  import("@/pages/user-detail").then((module) => ({ default: module.UserDetailPage })),
)
const NotFoundPage = lazy(() =>
  import("@/pages/not-found").then((module) => ({ default: module.NotFoundPage })),
)

export default function App() {
  return (
    <ErrorBoundary>
      <ThemeProvider>
        <I18nProvider>
          <AuthProvider>
            <BrowserRouter>
              <Suspense fallback={<PageFallback />}>
                <Routes>
                  <Route element={<PublicLayout />}>
                    <Route path="/" element={<HomePage />} />
                    <Route path="/docs" element={<DocsPage />} />
                    <Route path="/docs/:slug" element={<DocsPage />} />
                  </Route>

                  <Route path="/login" element={<LoginPage />} />

                  <Route element={<DashboardLayout />}>
                    <Route path="/dashboard" element={<DashboardPage />} />
                    <Route path="/users" element={<UsersPage />} />
                    <Route path="/users/:id" element={<UserDetailPage />} />
                  </Route>

                  <Route element={<PublicLayout />}>
                    <Route path="*" element={<NotFoundPage />} />
                  </Route>
                </Routes>
              </Suspense>
            </BrowserRouter>
            <Toaster />
          </AuthProvider>
        </I18nProvider>
      </ThemeProvider>
    </ErrorBoundary>
  )
}

function PageFallback() {
  return (
    <div className="flex min-h-svh items-center justify-center">
      <Spinner className="size-6 text-muted-foreground" />
    </div>
  )
}

function PublicLayout() {
  const { t } = useTranslation()
  const { user } = useAuth()

  return (
    <div className="min-h-svh">
      <a
        href="#content"
        className="sr-only focus:not-sr-only focus:absolute focus:left-4 focus:top-4 focus:z-50 focus:rounded-md focus:bg-background focus:px-3 focus:py-2 focus:text-sm focus:ring-3 focus:ring-ring/50"
      >
        {t("nav.skip")}
      </a>

      <header className="sticky top-0 z-30 border-b bg-background/85 backdrop-blur">
        <div className="mx-auto flex h-14 max-w-6xl items-center gap-4 px-4">
          <MobileNav />

          <Link to="/" className="flex items-center gap-2 font-semibold">
            <span className="flex size-7 items-center justify-center rounded-md bg-primary text-sm font-bold text-primary-foreground">
              Ω
            </span>
            Omega
          </Link>

          <nav className="ml-2 hidden gap-1 sm:flex">
            <Button asChild variant="ghost" size="sm">
              <Link to="/docs">{t("nav.docs")}</Link>
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
              <Link to={user ? "/dashboard" : "/login"}>
                {user ? t("nav.dashboard") : t("nav.signIn")}
              </Link>
            </Button>
          </div>
        </div>
      </header>

      <main id="content">
        <Outlet />
      </main>
    </div>
  )
}

function DashboardLayout() {
  const { user, loading } = useAuth()
  const location = useLocation()
  const { t } = useTranslation()

  if (loading) {
    return (
      <div className="p-8">
        <Skeleton className="h-64 w-full" />
      </div>
    )
  }
  if (!user) return <Navigate to="/login" replace state={{ from: location }} />

  const heading = location.pathname.startsWith("/users") ? t("nav.users") : t("nav.dashboard")

  return (
    <TooltipProvider delayDuration={0}>
    <SidebarProvider>
      <AppSidebar />
      <SidebarInset>
        <title>{`${heading} · Omega`}</title>
        <header className="flex h-14 shrink-0 items-center gap-2 border-b px-4">
          <SidebarTrigger className="-ml-1" />
          <Separator orientation="vertical" className="mr-2 h-4" />
          <h1 className="text-sm font-medium">{heading}</h1>
          <div className="ml-auto flex items-center gap-1">
            <LanguageToggle />
            <ThemeToggle />
          </div>
        </header>
        <main id="content" className="flex flex-1 flex-col gap-4 p-4 md:p-6">
          <Outlet />
        </main>
      </SidebarInset>
    </SidebarProvider>
    </TooltipProvider>
  )
}
