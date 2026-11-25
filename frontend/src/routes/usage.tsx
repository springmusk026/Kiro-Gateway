import { createFileRoute } from '@tanstack/react-router'
import { UsagePage } from '@/components/usage/usage-page'

// Public page for API key holders: outside the console layout and its admin
// sign-in; it authenticates each call with the key itself (GET /v1/usage).
export const Route = createFileRoute('/usage')({
  head: () => ({ meta: [{ title: 'API key usage · Kiro Gateway' }] }),
  component: UsagePage,
})
