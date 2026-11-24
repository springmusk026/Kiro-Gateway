import { Outlet, createFileRoute, redirect } from '@tanstack/react-router'
import { AppShell } from '@/components/app/app-shell'
import { getPassword } from '@/lib/api'

export const Route = createFileRoute('/_app')({
  beforeLoad: () => {
    if (typeof window !== 'undefined' && !getPassword()) throw redirect({ to: '/login' })
  },
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
})
