import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/settings')({ component: Page })

function Page() {
  return <div>settings</div>
}
