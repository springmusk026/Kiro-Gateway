import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/accounts')({ component: Page })

function Page() {
  return <div>accounts</div>
}
