import { useState } from 'react'
import { PageHeader } from '@/components/app/page-header'
import { useI18n } from '@/lib/i18n'
import { ApiKeysCard } from './api-keys-card'
import { ConnectCard } from './connect-card'
import { EndpointsCard } from './endpoints-card'

export function KeysPage() {
  const { t } = useI18n()
  // Plaintext of a key created in this visit, used to pre-fill the snippets. Never persisted.
  const [sessionKey, setSessionKey] = useState('')
  return (
    <div className="space-y-6">
      <PageHeader title={t('nav.keys')} description={t('page.keys')} />
      <div className="stagger space-y-6">
        <div className="grid gap-6 xl:grid-cols-[minmax(0,5fr)_minmax(0,7fr)]">
          <EndpointsCard />
          <ConnectCard sessionKey={sessionKey} />
        </div>
        <ApiKeysCard onKeyCreated={setSessionKey} />
      </div>
    </div>
  )
}
