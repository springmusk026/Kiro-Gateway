import { Outlet, createFileRoute, redirect } from '@tanstack/react-router'
import { AppShell } from '@/components/app/app-shell'
import { ensureSession } from '@/lib/api'

export const Route = createFileRoute('/_app')({
  beforeLoad: async () => {
    if (typeof window !== 'undefined' && !(await ensureSession())) throw redirect({ to: '/login' })
  },
  component: () => (
    <AppShell>
      <Outlet />
    </AppShell>
  ),
})
