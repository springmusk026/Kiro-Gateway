import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { TooltipProvider } from '@/components/ui/tooltip'
import { I18nProvider } from '@/lib/i18n'
import type { TFn } from '@/lib/i18n'
import type { ListPrice } from '@/lib/types'
import { en } from '@/locales/en'
import { ListPriceCell, priceLines, priceText } from './list-price'

const t: TFn = (key, vars) => {
  let s: string = en[key]
  for (const [k, v] of Object.entries(vars ?? {})) s = s.replaceAll(`{${k}}`, String(v))
  return s
}

const sonnet: ListPrice = {
  input: 3,
  output: 15,
  cacheRead: 0.3,
  cacheWrite: 3.75,
  tiers: [{ above: 200_000, input: 6, output: 22.5, cacheRead: 0.6, cacheWrite: 7.5 }],
}

describe('list price', () => {
  it('shows input / output per million tokens', () => {
    expect(priceText(sonnet)).toBe('$3 / $15')
    expect(priceText({ input: 0.5, output: 2.5 })).toBe('$0.50 / $2.50')
  })

  it('lists cache prices, context tiers and the source', () => {
    expect(priceLines(t, sonnet, 'anthropic/claude-sonnet-4-5')).toEqual([
      'Input $3 · output $15 per 1M tokens',
      'Cache read $0.30 · Cache write $3.75',
      'Above 200K tokens: $6 / $22.50',
      'models.dev: anthropic/claude-sonnet-4-5',
    ])
  })

  it('leaves out cache prices and tiers a model does not have', () => {
    expect(priceLines(t, { input: 1, output: 5 })).toEqual(['Input $1 · output $5 per 1M tokens'])
  })

  it('renders the price cell, or a dash without a price', () => {
    const { rerender } = render(
      <I18nProvider>
        <TooltipProvider>
          <ListPriceCell price={sonnet} source="anthropic/claude-sonnet-4-5" />
        </TooltipProvider>
      </I18nProvider>,
    )
    expect(screen.getByText('$3 / $15')).toBeInTheDocument()
    rerender(
      <I18nProvider>
        <TooltipProvider>
          <ListPriceCell />
        </TooltipProvider>
      </I18nProvider>,
    )
    expect(screen.getByTitle('No list price on models.dev')).toHaveTextContent('–')
  })
})
