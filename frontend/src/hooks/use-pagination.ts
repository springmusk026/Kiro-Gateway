import { useEffect, useMemo, useState } from 'react'

export const PAGE_SIZES = [10, 25, 50, 100] as const

function readSize(key: string | undefined, fallback: number): number {
  if (!key) return fallback
  try {
    const v = Number(localStorage.getItem('kiro_page_size:' + key))
    return (PAGE_SIZES as readonly number[]).includes(v) ? v : fallback
  } catch {
    return fallback
  }
}

/**
 * Client-side pagination over an in-memory list. The page resets when `resetKey`
 * changes (e.g. a filter or search) and is clamped when the list shrinks. The
 * chosen page size is remembered per `storageKey`.
 */
export function usePagination<T>(items: T[], opts: { pageSize?: number; storageKey?: string; resetKey?: unknown } = {}) {
  const { storageKey, resetKey } = opts
  const [pageSize, setPageSizeState] = useState(() => (typeof window === 'undefined' ? (opts.pageSize ?? 25) : readSize(storageKey, opts.pageSize ?? 25)))
  const [page, setPage] = useState(1)
  const total = items.length
  const pageCount = Math.max(1, Math.ceil(total / pageSize))

  useEffect(() => setPage(1), [resetKey])
  useEffect(() => {
    if (page > pageCount) setPage(pageCount)
  }, [page, pageCount])

  const setPageSize = (n: number) => {
    setPageSizeState(n)
    setPage(1)
    if (storageKey) {
      try {
        localStorage.setItem('kiro_page_size:' + storageKey, String(n))
      } catch {
        /* storage unavailable */
      }
    }
  }

  const current = Math.min(page, pageCount)
  const pageItems = useMemo(() => items.slice((current - 1) * pageSize, current * pageSize), [items, current, pageSize])
  return { page: current, setPage, pageSize, setPageSize, pageCount, total, pageItems }
}

export type Pagination = ReturnType<typeof usePagination<unknown>>
