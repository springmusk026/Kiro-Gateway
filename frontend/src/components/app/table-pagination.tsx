import { ChevronLeft, ChevronRight, ChevronsLeft, ChevronsRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { PAGE_SIZES } from '@/hooks/use-pagination'
import { useI18n } from '@/lib/i18n'
import { cn } from '@/lib/utils'

/** Compact page list: 1 … 4 5 6 … 20 */
function pageList(page: number, count: number): (number | 'gap')[] {
  if (count <= 7) return Array.from({ length: count }, (_, i) => i + 1)
  const out: (number | 'gap')[] = [1]
  const from = Math.max(2, page - 1)
  const to = Math.min(count - 1, page + 1)
  if (from > 2) out.push('gap')
  for (let p = from; p <= to; p++) out.push(p)
  if (to < count - 1) out.push('gap')
  out.push(count)
  return out
}

export interface TablePaginationProps {
  page: number
  pageCount: number
  pageSize: number
  /** Total rows; omit when unknown (cursor paging), then "of N" is hidden. */
  total?: number
  /** Rows on the current page, used when total is unknown. */
  shown?: number
  onPage: (p: number) => void
  onPageSize?: (n: number) => void
  /** Cursor paging: next page exists even though pageCount is unknown. */
  hasNext?: boolean
  busy?: boolean
  /** Cursor paging: only first/previous/next navigation (no jumping). */
  sequential?: boolean
  /** The total is a lower bound ("of 100,000+"). */
  totalCapped?: boolean
  className?: string
  sizes?: readonly number[]
}

/** Footer for paginated tables and lists: range, page size and navigation. */
export function TablePagination({
  page,
  pageCount,
  pageSize,
  total,
  shown,
  onPage,
  onPageSize,
  hasNext,
  busy,
  sequential,
  totalCapped,
  className,
  sizes = PAGE_SIZES,
}: TablePaginationProps) {
  const { t } = useI18n()
  const known = total !== undefined
  if (known && total <= Math.min(...sizes) && page === 1) return null
  const from = (page - 1) * pageSize + 1
  const to = known ? Math.min(total, page * pageSize) : from + (shown ?? pageSize) - 1
  const canNext = hasNext ?? page < pageCount
  const jump = known && !sequential
  const empty = known ? total === 0 : (shown ?? 0) === 0

  return (
    <div className={cn('flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-t px-4 py-2.5 text-xs text-muted-foreground', busy && 'opacity-70', className)}>
      <div className="flex items-center gap-3">
        <span className="tabular" aria-live="polite">
          {empty
            ? t('pager.none')
            : known
              ? t('pager.range', { from, to, total: total.toLocaleString() + (totalCapped ? '+' : '') })
              : t('pager.rangeOpen', { from, to })}
        </span>
        {onPageSize && (
          <label className="hidden items-center gap-1.5 sm:flex">
            {t('pager.perPage')}
            <Select value={String(pageSize)} onValueChange={(v) => onPageSize(Number(v))}>
              <SelectTrigger size="sm" className="h-7 w-[72px] text-xs" aria-label={t('pager.perPage')}>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {sizes.map((n) => (
                  <SelectItem key={n} value={String(n)} className="text-xs">
                    {n}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </label>
        )}
      </div>
      <nav className="flex items-center gap-1" aria-label={t('pager.nav')}>
        {(known || sequential) && (
          <Button variant="ghost" size="icon" className="size-7" disabled={page <= 1} onClick={() => onPage(1)} aria-label={t('pager.first')}>
            <ChevronsLeft className="size-4" />
          </Button>
        )}
        <Button variant="ghost" size="icon" className="size-7" disabled={page <= 1} onClick={() => onPage(page - 1)} aria-label={t('pager.prev')}>
          <ChevronLeft className="size-4" />
        </Button>
        {jump ? (
          <span className="hidden items-center gap-1 sm:flex">
            {pageList(page, pageCount).map((p, i) =>
              p === 'gap' ? (
                <span key={'g' + i} className="px-1">
                  …
                </span>
              ) : (
                <Button
                  key={p}
                  variant={p === page ? 'secondary' : 'ghost'}
                  size="sm"
                  className="h-7 min-w-7 px-2 text-xs tabular"
                  aria-current={p === page ? 'page' : undefined}
                  onClick={() => onPage(p)}
                >
                  {p}
                </Button>
              ),
            )}
          </span>
        ) : (
          <span className="px-2 tabular">{t('pager.page', { n: page })}</span>
        )}
        {jump && <span className="px-2 tabular sm:hidden">{t('pager.pageOf', { n: page, total: pageCount })}</span>}
        <Button variant="ghost" size="icon" className="size-7" disabled={!canNext} onClick={() => onPage(page + 1)} aria-label={t('pager.next')}>
          <ChevronRight className="size-4" />
        </Button>
        {jump && (
          <Button variant="ghost" size="icon" className="size-7" disabled={page >= pageCount} onClick={() => onPage(pageCount)} aria-label={t('pager.last')}>
            <ChevronsRight className="size-4" />
          </Button>
        )}
      </nav>
    </div>
  )
}
