import { createFileRoute } from '@tanstack/react-router'
import { LogsPage } from '@/components/logs/logs-page'

export const Route = createFileRoute('/_app/logs')({
  // ?conversation=<id> opens that conversation's timeline in the Conversations tab.
  validateSearch: (s: Record<string, unknown>): { conversation?: string } =>
    typeof s.conversation === 'string' && s.conversation ? { conversation: s.conversation } : {},
  component: LogsPage,
})
