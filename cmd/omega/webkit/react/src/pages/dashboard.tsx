import { useQuery } from "@/lib/query"
import { BadgeCheck, ShieldCheck, Users } from "lucide-react"
import { useTranslation } from "@/lib/i18n"
import { Link } from "react-router"

import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Empty, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import {
  Item,
  ItemActions,
  ItemContent,
  ItemDescription,
  ItemGroup,
  ItemMedia,
  ItemTitle,
} from "@/components/ui/item"
import { Progress } from "@/components/ui/progress"
import { Skeleton } from "@/components/ui/skeleton"
import { api } from "@/lib/api"

export function DashboardPage() {
  const { t } = useTranslation()

  const all = useQuery({ queryKey: ["stats"], queryFn: () => api.users({ per_page: 200 }) })
  const recent = useQuery({
    queryKey: ["recent"],
    queryFn: () => api.users({ per_page: 5, sort: "-created_at" }),
  })

  const users = all.data?.data ?? []
  const total = all.data?.meta.total ?? 0
  const admins = users.filter((user) => user.role === "admin").length
  const verified = users.filter((user) => user.email_verified_at).length

  const cards = [
    { key: "totalUsers", value: total, icon: Users, ratio: 100 },
    { key: "admins", value: admins, icon: ShieldCheck, ratio: total ? (admins / total) * 100 : 0 },
    { key: "verified", value: verified, icon: BadgeCheck, ratio: total ? (verified / total) * 100 : 0 },
  ]

  return (
    <div className="flex flex-col gap-6">
      <div className="grid gap-4 sm:grid-cols-3">
        {cards.map((card) => (
          <Card key={card.key}>
            <CardHeader className="flex flex-row items-center justify-between pb-2">
              <CardDescription>{t(`dashboard.${card.key}`)}</CardDescription>
              <card.icon className="size-4 text-muted-foreground" />
            </CardHeader>
            <CardContent className="grid gap-3">
              {all.isPending ? (
                <Skeleton className="h-8 w-16" />
              ) : (
                <span className="text-3xl font-semibold tabular-nums">{card.value}</span>
              )}
              <Progress value={card.ratio} className="h-1.5" />
            </CardContent>
          </Card>
        ))}
      </div>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">{t("dashboard.recent")}</CardTitle>
          <CardDescription>
            <code className="text-xs">GET /api/users?sort=-created_at&per_page=5</code>
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ItemGroup>
            {recent.isPending &&
              Array.from({ length: 5 }, (_, index) => (
                <Skeleton key={index} className="h-14 w-full" />
              ))}

            {recent.data?.data.map((user) => (
              <Item key={user.id} asChild>
                <Link to={`/users/${user.id}`}>
                  <ItemMedia>
                    <Avatar className="size-9">
                      <AvatarFallback className="text-xs">
                        {user.name?.slice(0, 2).toUpperCase()}
                      </AvatarFallback>
                    </Avatar>
                  </ItemMedia>
                  <ItemContent>
                    <ItemTitle>{user.name}</ItemTitle>
                    <ItemDescription>{user.email}</ItemDescription>
                  </ItemContent>
                  <ItemActions>
                    <Badge variant={user.role === "admin" ? "default" : "secondary"}>
                      {user.role}
                    </Badge>
                  </ItemActions>
                </Link>
              </Item>
            ))}

            {recent.data?.data.length === 0 && (
              <Empty>
                <EmptyHeader>
                  <EmptyMedia variant="icon">
                    <Users />
                  </EmptyMedia>
                  <EmptyTitle>{t("users.empty")}</EmptyTitle>
                </EmptyHeader>
              </Empty>
            )}
          </ItemGroup>
        </CardContent>
      </Card>
    </div>
  )
}
