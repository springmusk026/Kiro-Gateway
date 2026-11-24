import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/keys')({ component: Page })

function Page() {
  return <div>keys</div>
}
