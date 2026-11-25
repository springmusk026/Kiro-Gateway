import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { I18nProvider } from '@/lib/i18n'
import { TablePagination, type TablePaginationProps } from './table-pagination'

function renderPager(props: Partial<TablePaginationProps> = {}) {
  const onPage = vi.fn()
  const utils = render(
    <I18nProvider>
      <TablePagination page={1} pageCount={4} pageSize={25} total={100} onPage={onPage} {...props} />
    </I18nProvider>,
  )
  return { ...utils, onPage }
}

const btn = (name: string) => screen.getByRole('button', { name })
const queryBtn = (name: string) => screen.queryByRole('button', { name })

describe('TablePagination', () => {
  it('renders nothing when everything fits on the first page', () => {
    const { container } = renderPager({ total: 10, pageCount: 1 })
    expect(container).toBeEmptyDOMElement()
  })

  it('still renders on a later page even if the total became small', () => {
    renderPager({ page: 2, total: 8, pageCount: 1 })
    expect(screen.getByRole('navigation', { name: 'Pagination' })).toBeInTheDocument()
  })

  it('shows the range of the current page and clamps the last page to the total', () => {
    const { rerender, onPage } = renderPager({ total: 1234, pageCount: 50, page: 2 })
    expect(screen.getByText('26–50 of 1,234')).toBeInTheDocument()
    rerender(
      <I18nProvider>
        <TablePagination page={50} pageCount={50} pageSize={25} total={1234} onPage={onPage} />
      </I18nProvider>,
    )
    // Only the total is locale-formatted; from/to are plain numbers.
    expect(screen.getByText('1226–1234 of 1,234')).toBeInTheDocument()
  })

  it('appends + to a capped total', () => {
    renderPager({ total: 100000, pageCount: 4000, totalCapped: true })
    expect(screen.getByText('1–25 of 100,000+')).toBeInTheDocument()
  })

  it('shows "No rows" for an empty known total on a later page', () => {
    renderPager({ total: 0, page: 2, pageCount: 1 })
    expect(screen.getByText('No rows')).toBeInTheDocument()
  })

  it('disables first/previous on page 1 and next/last on the last page', () => {
    const first = renderPager({ page: 1, pageCount: 4 })
    expect(btn('First page')).toBeDisabled()
    expect(btn('Previous page')).toBeDisabled()
    expect(btn('Next page')).toBeEnabled()
    expect(btn('Last page')).toBeEnabled()
    first.unmount()

    renderPager({ page: 4, pageCount: 4 })
    expect(btn('First page')).toBeEnabled()
    expect(btn('Previous page')).toBeEnabled()
    expect(btn('Next page')).toBeDisabled()
    expect(btn('Last page')).toBeDisabled()
  })

  it('navigates with the arrow buttons and the page list', () => {
    const { onPage } = renderPager({ page: 2, pageCount: 4 })
    fireEvent.click(btn('Next page'))
    fireEvent.click(btn('Previous page'))
    fireEvent.click(btn('First page'))
    fireEvent.click(btn('Last page'))
    fireEvent.click(btn('3'))
    expect(onPage.mock.calls.map((c) => c[0])).toEqual([3, 1, 1, 4, 3])
    expect(btn('2')).toHaveAttribute('aria-current', 'page')
  })

  it('compacts a long page list with gaps', () => {
    renderPager({ page: 10, pageCount: 20, total: 500 })
    const pages = screen
      .getAllByRole('button')
      .map((b) => b.textContent)
      .filter((s) => s && /^\d+$/.test(s))
    expect(pages).toEqual(['1', '9', '10', '11', '20'])
    expect(screen.getAllByText('…')).toHaveLength(2)
  })

  describe('sequential (cursor) mode', () => {
    it('shows an open range and "Page n" without jumping or a last button', () => {
      renderPager({ total: undefined, shown: 25, page: 3, pageCount: 3, sequential: true, hasNext: true })
      expect(screen.getByText('51–75')).toBeInTheDocument()
      expect(screen.getByText('Page 3')).toBeInTheDocument()
      expect(btn('First page')).toBeEnabled()
      expect(queryBtn('Last page')).toBeNull()
      expect(queryBtn('2')).toBeNull()
    })

    it('uses hasNext for the next button', () => {
      const { unmount } = renderPager({ total: undefined, shown: 25, page: 1, pageCount: 1, sequential: true, hasNext: true })
      expect(btn('Next page')).toBeEnabled()
      unmount()
      renderPager({ total: undefined, shown: 7, page: 2, pageCount: 2, sequential: true, hasNext: false })
      expect(btn('Next page')).toBeDisabled()
      expect(screen.getByText('26–32')).toBeInTheDocument()
    })

    it('says "No rows" when the cursor page is empty', () => {
      renderPager({ total: undefined, shown: 0, page: 1, pageCount: 1, sequential: true, hasNext: false })
      expect(screen.getByText('No rows')).toBeInTheDocument()
    })

    it('a known total with sequential still hides the page list', () => {
      renderPager({ total: 300, page: 2, pageCount: 12, sequential: true })
      expect(screen.getByText('26–50 of 300')).toBeInTheDocument()
      expect(screen.getByText('Page 2')).toBeInTheDocument()
      expect(queryBtn('Last page')).toBeNull()
    })
  })

  it('offers the page-size selector only when onPageSize is given', () => {
    const { unmount } = renderPager()
    expect(screen.queryByRole('combobox', { name: 'Rows per page' })).toBeNull()
    unmount()
    renderPager({ onPageSize: vi.fn() })
    expect(screen.getByRole('combobox', { name: 'Rows per page' })).toHaveTextContent('25')
  })

  it('marks the footer busy', () => {
    const { container } = renderPager({ busy: true })
    expect(container.firstElementChild).toHaveClass('opacity-70')
  })
})
