import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/logs')({ component: Page })

function Page() {
  return <div>logs</div>
}
