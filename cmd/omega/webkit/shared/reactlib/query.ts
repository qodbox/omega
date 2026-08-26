"use client"

import { useCallback, useEffect, useRef, useState } from "react"

import { notifyError } from "@/lib/notify"

type Options<T> = {
  queryKey: unknown[]
  queryFn: () => Promise<T>
  enabled?: boolean
  placeholderData?: unknown
}

type Result<T> = {
  data: T | undefined
  error: unknown
  isError: boolean
  isPending: boolean
  isLoading: boolean
  isFetching: boolean
  isSuccess: boolean
  refetch: () => void
}

// Just what the pages actually used from TanStack Query: a key, a promise,
// and the states that go with them.
export function useQuery<T>({ queryKey, queryFn, enabled = true }: Options<T>): Result<T> {
  const [data, setData] = useState<T | undefined>(undefined)
  const [error, setError] = useState<unknown>(undefined)
  const [isFetching, setFetching] = useState(false)

  const fn = useRef(queryFn)
  fn.current = queryFn

  const key = JSON.stringify(queryKey)
  const generation = useRef(0)

  const run = useCallback(() => {
    if (!enabled) return

    const mine = ++generation.current
    setFetching(true)

    fn.current()
      .then((value) => {
        if (mine !== generation.current) return
        setData(value)
        setError(undefined)
      })
      .catch((failure: unknown) => {
        if (mine !== generation.current) return
        setError(failure)
        notifyError(failure)
      })
      .finally(() => {
        if (mine === generation.current) setFetching(false)
      })
  }, [enabled])

  useEffect(run, [key, run])

  const isPending = data === undefined && isFetching

  return {
    data,
    error,
    isError: error !== undefined,
    isPending,
    isLoading: isPending,
    isFetching,
    isSuccess: data !== undefined && error === undefined,
    refetch: run,
  }
}

export const keepPreviousData = undefined
