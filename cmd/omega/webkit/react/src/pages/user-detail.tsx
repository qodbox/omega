import { useQuery } from "@/lib/query"
import { UserXIcon } from "lucide-react"
import { useTranslation } from "@/lib/i18n"
import { Link, useParams } from "react-router"

import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Empty,
  EmptyContent,
  EmptyDescription,
  EmptyHeader,
  EmptyMedia,
  EmptyTitle,
} from "@/components/ui/empty"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/lib/api"

export function UserDetailPage() {
  const { t } = useTranslation()
  const { id } = useParams()

  const query = useQuery({
    queryKey: ["user", id],
    queryFn: () => api.user(id!),
    enabled: Boolean(id),
  })

  if (query.isPending) {
    return (
      <Card>
        <CardHeader>
          <Skeleton className="h-6 w-40" />
        </CardHeader>
        <CardContent className="grid gap-3">
          <Skeleton className="h-5 w-full" />
          <Skeleton className="h-5 w-2/3" />
        </CardContent>
      </Card>
    )
  }

  if (query.isError) {
    return (
      <Empty className="rounded-xl border">
        <EmptyHeader>
          <EmptyMedia variant="icon">
            <UserXIcon />
          </EmptyMedia>
          <EmptyTitle>{t("users.notFound")}</EmptyTitle>
          <EmptyDescription>{t("users.notFoundHint")}</EmptyDescription>
        </EmptyHeader>
        <EmptyContent>
          <Button asChild variant="outline" size="sm">
            <Link to="/users">{t("users.back")}</Link>
          </Button>
        </EmptyContent>
      </Empty>
    )
  }

  const user = query.data?.data
  if (!user) return null
  const initials = user.name
    ?.split(" ")
    .map((word) => word[0])
    .slice(0, 2)
    .join("")
    .toUpperCase()

  return (
    <Card>
      <CardHeader className="flex flex-row items-center gap-4">
        <Avatar className="size-12">
          <AvatarFallback>{initials}</AvatarFallback>
        </Avatar>
        <div className="grid gap-1">
          <CardTitle>{user.name}</CardTitle>
          <span className="text-sm text-muted-foreground">{user.email}</span>
        </div>
        <Badge className="ml-auto" variant={user.role === "admin" ? "default" : "secondary"}>
          {user.role}
        </Badge>
      </CardHeader>

      <CardContent className="grid gap-4">
        <Separator />
        <dl className="grid gap-3 text-sm sm:grid-cols-2">
          <Row label={t("users.identifier")} value={String(user.id)} />
          <Row label={t("users.role")} value={user.role ?? "—"} />
          <Row
            label={t("users.verified")}
            value={user.email_verified_at ? new Date(user.email_verified_at).toLocaleString() : t("users.no")}
          />
          <Row
            label={t("users.created")}
            value={user.created_at ? new Date(user.created_at).toLocaleString() : "—"}
          />
        </dl>
        <div>
          <Button asChild variant="outline" size="sm">
            <Link to="/users">{t("users.back")}</Link>
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="grid gap-0.5">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-medium">{value}</dd>
    </div>
  )
}
