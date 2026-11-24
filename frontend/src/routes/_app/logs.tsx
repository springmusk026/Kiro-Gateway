import { createFileRoute } from '@tanstack/react-router'
import { LogsPage } from '@/components/logs/logs-page'

export const Route = createFileRoute('/_app/logs')({ component: LogsPage })
