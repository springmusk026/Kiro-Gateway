import { useQueryClient } from '@tanstack/react-query'
import { createContext, useCallback, useContext, useMemo, useState, type ReactNode } from 'react'
import { toast } from 'sonner'
import { useConfirm } from '@/components/app/confirm'
import { copyToClipboard, maskEmail } from '@/lib/format'
import { useI18n } from '@/lib/i18n'
import { qk } from '@/lib/queries'
import { accountApi, invalidateAccounts, useUpdateAccount } from '@/lib/queries.accounts'
import type { Account } from '@/lib/types'
import { credentialsFromFull, displayName, errMsg, usePrivacyMode } from './lib'

export type DetailTab = 'overview' | 'settings' | 'models'

interface Ctx {
  busy: ReadonlySet<string>
  openDetail: (id: string, tab?: DetailTab) => void
  openTest: (id: string) => void
  refresh: (a: Account) => Promise<void>
  setEnabled: (a: Account, enabled: boolean) => Promise<void>
  reenable: (a: Account) => Promise<void>
  remove: (a: Account) => Promise<boolean>
  copyCredentials: (a: Account) => Promise<void>
  refreshModels: (a: Account) => Promise<void>
  name: (a: Account) => string
}

const AccountActionsCtx = createContext<Ctx | null>(null)

export function useAccountActions() {
  const c = useContext(AccountActionsCtx)
  if (!c) throw new Error('useAccountActions outside provider')
  return c
}

export function AccountActionsProvider({
  children,
  openDetail,
  openTest,
}: {
  children: ReactNode
  openDetail: Ctx['openDetail']
  openTest: Ctx['openTest']
}) {
  const { t } = useI18n()
  const qc = useQueryClient()
  const confirm = useConfirm()
  const [privacy] = usePrivacyMode()
  const update = useUpdateAccount()
  const [busy, setBusy] = useState<ReadonlySet<string>>(new Set())

  const name = useCallback((a: Account) => displayName(a, privacy, maskEmail), [privacy])

  const withBusy = useCallback(async <T,>(id: string, fn: () => Promise<T>) => {
    setBusy((s) => new Set(s).add(id))
    try {
      return await fn()
    } finally {
      setBusy((s) => {
        const n = new Set(s)
        n.delete(id)
        return n
      })
    }
  }, [])

  const refresh = useCallback(
    (a: Account) =>
      withBusy(a.id, async () => {
        try {
          const r = await accountApi.refresh(a.id)
          if (r.message) toast.warning(t('accounts.toast.refreshSuspended', { name: name(a) }))
          else toast.success(t('accounts.toast.refreshed', { name: name(a) }))
        } catch (e) {
          toast.error(t('accounts.toast.refreshFailed', { error: errMsg(e) }))
        } finally {
          await invalidateAccounts(qc)
        }
      }),
    [withBusy, qc, t, name],
  )

  const setEnabled = useCallback(
    (a: Account, enabled: boolean) =>
      withBusy(a.id, async () => {
        try {
          await update.mutateAsync({ id: a.id, patch: { enabled } })
          toast.success(t(enabled ? 'accounts.toast.enabled' : 'accounts.toast.disabled', { name: name(a) }))
        } catch (e) {
          toast.error(errMsg(e))
        }
      }),
    [withBusy, update, t, name],
  )

  // PUT {enabled:true} does not clear a ban; batch enable does (handler.go:2951).
  const reenable = useCallback(
    async (a: Account) => {
      const ok = await confirm({
        title: t('accounts.confirm.reenableTitle'),
        description: t('accounts.confirm.reenableText', { name: name(a) }),
        confirmLabel: t('accounts.action.reenable'),
      })
      if (!ok) return
      await withBusy(a.id, async () => {
        try {
          await accountApi.batch([a.id], 'enable')
          toast.success(t('accounts.toast.reenabled', { name: name(a) }))
        } catch (e) {
          toast.error(errMsg(e))
        } finally {
          await invalidateAccounts(qc)
        }
      })
    },
    [confirm, withBusy, qc, t, name],
  )

  const remove = useCallback(
    async (a: Account) => {
      const ok = await confirm({
        title: t('accounts.confirm.deleteTitle'),
        description: t('accounts.confirm.deleteText', { name: name(a) }),
        confirmLabel: t('common.delete'),
        destructive: true,
      })
      if (!ok) return false
      return withBusy(a.id, async () => {
        try {
          await accountApi.remove(a.id)
          qc.setQueryData<Account[]>(qk.accounts, (prev) => prev?.filter((x) => x.id !== a.id))
          toast.success(t('accounts.toast.deleted', { name: name(a) }))
          return true
        } catch (e) {
          toast.error(errMsg(e))
          return false
        } finally {
          await invalidateAccounts(qc)
        }
      })
    },
    [confirm, withBusy, qc, t, name],
  )

  const copyCredentials = useCallback(
    (a: Account) =>
      withBusy(a.id, async () => {
        try {
          // Safari needs the clipboard write to start inside the user gesture; pass a promise.
          const textPromise = accountApi.full(a.id).then((f) => JSON.stringify(credentialsFromFull(f), null, 2))
          let done = false
          if (typeof ClipboardItem !== 'undefined' && navigator.clipboard?.write) {
            try {
              await navigator.clipboard.write([
                new ClipboardItem({ 'text/plain': textPromise.then((s) => new Blob([s], { type: 'text/plain' })) }),
              ])
              done = true
            } catch {
              /* fall back below */
            }
          }
          if (!done) await copyToClipboard(await textPromise)
          toast.success(t('accounts.toast.credentialsCopied'), { description: t('accounts.toast.credentialsCopiedHint') })
        } catch (e) {
          toast.error(errMsg(e))
        }
      }),
    [withBusy, t],
  )

  const refreshModels = useCallback(
    (a: Account) =>
      withBusy(a.id, async () => {
        try {
          const r = await accountApi.refreshModels(a.id)
          toast.success(t('accounts.toast.modelsRefreshed', { n: r.count }))
          qc.invalidateQueries({ queryKey: qk.models })
          qc.invalidateQueries({ queryKey: ['accounts', a.id] })
        } catch (e) {
          toast.error(errMsg(e))
        }
      }),
    [withBusy, qc, t],
  )

  const value = useMemo<Ctx>(
    () => ({ busy, openDetail, openTest, refresh, setEnabled, reenable, remove, copyCredentials, refreshModels, name }),
    [busy, openDetail, openTest, refresh, setEnabled, reenable, remove, copyCredentials, refreshModels, name],
  )
  return <AccountActionsCtx.Provider value={value}>{children}</AccountActionsCtx.Provider>
}
