import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/playground')({ component: Page })

function Page() {
  return <div>playground</div>
}
