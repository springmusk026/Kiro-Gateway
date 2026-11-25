import { act, renderHook } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { usePagination } from './use-pagination'

const range = (n: number) => Array.from({ length: n }, (_, i) => i + 1)

describe('usePagination', () => {
  it('slices the first page with the default size of 25', () => {
    const { result } = renderHook(() => usePagination(range(60)))
    expect(result.current.pageSize).toBe(25)
    expect(result.current.page).toBe(1)
    expect(result.current.pageCount).toBe(3)
    expect(result.current.total).toBe(60)
    expect(result.current.pageItems).toEqual(range(25))
  })

  it('returns the remainder on the last page', () => {
    const { result } = renderHook(() => usePagination(range(60)))
    act(() => result.current.setPage(3))
    expect(result.current.page).toBe(3)
    expect(result.current.pageItems).toEqual([51, 52, 53, 54, 55, 56, 57, 58, 59, 60])
  })

  it('has one empty page for an empty list', () => {
    const { result } = renderHook(() => usePagination([]))
    expect(result.current.pageCount).toBe(1)
    expect(result.current.page).toBe(1)
    expect(result.current.pageItems).toEqual([])
  })

  it('clamps the page when the list shrinks', () => {
    const { result, rerender } = renderHook(({ items }) => usePagination(items, { pageSize: 10 }), {
      initialProps: { items: range(50) },
    })
    act(() => result.current.setPage(5))
    expect(result.current.pageItems[0]).toBe(41)
    rerender({ items: range(12) })
    expect(result.current.page).toBe(2)
    expect(result.current.pageItems).toEqual([11, 12])
  })

  it('goes back to page 1 when the reset key changes', () => {
    const { result, rerender } = renderHook(({ q }) => usePagination(range(100), { pageSize: 10, resetKey: q }), {
      initialProps: { q: '' },
    })
    act(() => result.current.setPage(4))
    expect(result.current.page).toBe(4)
    rerender({ q: 'abc' })
    expect(result.current.page).toBe(1)
  })

  it('changing the page size resets to page 1 and remembers the size per storage key', () => {
    const { result } = renderHook(() => usePagination(range(100), { storageKey: 'keys' }))
    act(() => result.current.setPage(3))
    act(() => result.current.setPageSize(50))
    expect(result.current.page).toBe(1)
    expect(result.current.pageCount).toBe(2)
    expect(localStorage.getItem('kiro_page_size:keys')).toBe('50')

    const again = renderHook(() => usePagination(range(100), { storageKey: 'keys' }))
    expect(again.result.current.pageSize).toBe(50)
  })

  it('ignores a stored page size that is not one of the choices', () => {
    localStorage.setItem('kiro_page_size:logs', '7')
    const { result } = renderHook(() => usePagination(range(100), { storageKey: 'logs', pageSize: 10 }))
    expect(result.current.pageSize).toBe(10)
  })

  it('does not persist the size without a storage key', () => {
    const { result } = renderHook(() => usePagination(range(100)))
    act(() => result.current.setPageSize(100))
    expect(result.current.pageSize).toBe(100)
    expect(localStorage.length).toBe(0)
  })
})
