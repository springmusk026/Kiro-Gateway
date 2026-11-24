import { createFileRoute } from '@tanstack/react-router'

export const Route = createFileRoute('/_app/models')({ component: Page })

function Page() {
  return <div>models</div>
}
