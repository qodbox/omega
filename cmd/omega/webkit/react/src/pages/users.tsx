import { useState } from "react"
import { useTranslation } from "@/lib/i18n"
import { keepPreviousData, useQuery } from "@/lib/query"
import { ChevronLeftIcon, ChevronRightIcon, SearchIcon, UsersIcon, XIcon } from "lucide-react"
import { Link } from "react-router"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { ButtonGroup } from "@/components/ui/button-group"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from "@/components/ui/empty"
import {
  InputGroup,
  InputGroupAddon,
  InputGroupButton,
  InputGroupInput,
} from "@/components/ui/input-group"
import { Skeleton } from "@/components/ui/skeleton"
import { Spinner } from "@/components/ui/spinner"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { api } from "@/lib/api"
import { useDebounced } from "@/lib/use-debounced"

export function UsersPage() {
  const { t } = useTranslation()
  const [term, setTerm] = useState("")
  const [page, setPage] = useState(1)
  const search = useDebounced(term, 250)

  const query = useQuery({
    queryKey: ["users", search, page],
    queryFn: () =>
      api.users({
        page,
        per_page: 10,
        sort: "name",
        ...(search ? { name__like: `%${search}%` } : {}),
      }),
    placeholderData: keepPreviousData,
  })

  const meta = query.data?.meta
  const rows = query.data?.data

  return (
    <Card>
      <CardHeader>
        <CardTitle>{t("users.title")}</CardTitle>
        <CardDescription>{t("users.description")}</CardDescription>
      </CardHeader>

      <CardContent className="grid gap-4">
        <InputGroup className="max-w-sm">
          <InputGroupAddon>
            {query.isFetching ? <Spinner /> : <SearchIcon />}
          </InputGroupAddon>
          <InputGroupInput
            placeholder={t("users.filter")}
            value={term}
            onChange={(event) => {
              setTerm(event.target.value)
              setPage(1)
            }}
          />
          {term && (
            <InputGroupAddon align="inline-end">
              <InputGroupButton
                size="icon-xs"
                aria-label={t("users.clear")}
                onClick={() => {
                  setTerm("")
                  setPage(1)
                }}
              >
                <XIcon />
              </InputGroupButton>
            </InputGroupAddon>
          )}
        </InputGroup>

        {rows?.length === 0 ? (
          <Empty className="rounded-md border">
            <EmptyHeader>
              <EmptyMedia variant="icon">
                <UsersIcon />
              </EmptyMedia>
              <EmptyTitle>{t("users.empty")}</EmptyTitle>
              <EmptyDescription>{t("users.emptyHint")}</EmptyDescription>
            </EmptyHeader>
          </Empty>
        ) : (
          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("users.name")}</TableHead>
                  <TableHead>{t("users.email")}</TableHead>
                  <TableHead>{t("users.role")}</TableHead>
                  <TableHead className="text-right">{t("users.created")}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {query.isPending &&
                  Array.from({ length: 5 }, (_, index) => (
                    <TableRow key={index}>
                      <TableCell colSpan={4}>
                        <Skeleton className="h-5 w-full" />
                      </TableCell>
                    </TableRow>
                  ))}

                {rows?.map((user) => (
                  <TableRow key={user.id}>
                    <TableCell className="font-medium">
                      <Link to={`/users/${user.id}`} className="hover:underline">
                        {user.name}
                      </Link>
                    </TableCell>
                    <TableCell className="text-muted-foreground">{user.email}</TableCell>
                    <TableCell>
                      <Badge variant={user.role === "admin" ? "default" : "secondary"}>
                        {user.role}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-right text-muted-foreground">
                      {user.created_at ? new Date(user.created_at).toLocaleDateString() : "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}

        {meta && meta.total > 0 && (
          <div className="flex items-center justify-between text-sm text-muted-foreground">
            <span>
              {t("users.count", { total: meta.total, page: meta.page, last: meta.last_page })}
            </span>
            <ButtonGroup>
              <Button
                variant="outline"
                size="sm"
                disabled={meta.page <= 1}
                aria-label={t("users.previous")}
                onClick={() => setPage((current) => current - 1)}
              >
                <ChevronLeftIcon />
                {t("users.previous")}
              </Button>
              <Button
                variant="outline"
                size="sm"
                disabled={meta.page >= meta.last_page}
                aria-label={t("users.next")}
                onClick={() => setPage((current) => current + 1)}
              >
                {t("users.next")}
                <ChevronRightIcon />
              </Button>
            </ButtonGroup>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
