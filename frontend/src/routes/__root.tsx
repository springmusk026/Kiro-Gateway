import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { HeadContent, Outlet, Scripts, createRootRoute } from '@tanstack/react-router'
import { useState } from 'react'
import { ConfirmProvider } from '@/components/app/confirm'
import { Toaster } from '@/components/ui/sonner'
import { TooltipProvider } from '@/components/ui/tooltip'
import { AuthProvider } from '@/lib/auth'
import { I18nProvider } from '@/lib/i18n'
import { ThemeProvider } from '@/lib/theme'
import appCss from '@/styles.css?url'

// Applied before React loads, so the first paint already uses the right theme.
const themeBoot = `(function(){try{var t=localStorage.getItem('kiro_theme')||'system';var d=t==='dark'||(t==='system'&&matchMedia('(prefers-color-scheme: dark)').matches);if(d)document.documentElement.classList.add('dark')}catch(e){}})()`

export const Route = createRootRoute({
  head: () => ({
    meta: [
      { charSet: 'utf-8' },
      { name: 'viewport', content: 'width=device-width, initial-scale=1' },
      { name: 'color-scheme', content: 'light dark' },
      { title: 'Kiro Gateway Console' },
    ],
    links: [
      { rel: 'stylesheet', href: appCss },
      { rel: 'icon', type: 'image/svg+xml', href: '/admin/favicon.svg' },
    ],
    scripts: [{ children: themeBoot }],
  }),
  shellComponent: ({ children }) => (
    <html lang="en" suppressHydrationWarning>
      <head>
        <HeadContent />
      </head>
      <body>
        {children}
        <Scripts />
      </body>
    </html>
  ),
  component: Root,
})

function Root() {
  const [qc] = useState(
    () =>
      new QueryClient({
        defaultOptions: {
          queries: { retry: (n, err) => n < 2 && !(err as { status?: number })?.status, refetchOnWindowFocus: true },
        },
      }),
  )
  return (
    <QueryClientProvider client={qc}>
      <ThemeProvider>
        <I18nProvider>
          <TooltipProvider delayDuration={250}>
            <AuthProvider>
              <ConfirmProvider>
                <Outlet />
              </ConfirmProvider>
            </AuthProvider>
            <Toaster richColors closeButton position="bottom-right" />
          </TooltipProvider>
        </I18nProvider>
      </ThemeProvider>
    </QueryClientProvider>
  )
}
