import { createFileRoute } from '@tanstack/react-router'
import { KeysPage } from '@/components/keys/keys-page'

export const Route = createFileRoute('/_app/keys')({ component: KeysPage })
