import { Brain, Plug } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { CodeBlock } from '@/components/app/code-block'
import { Card } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { useI18n } from '@/lib/i18n'
import { useModels } from '@/lib/queries'
import { useSettings, useThinking } from '@/lib/queries.settings'
import { origin } from './endpoints-card'

const KEY_PLACEHOLDER = 'sk-your-kiro-go-key'

function pick(ids: string[], ...prefs: string[]) {
  for (const p of prefs) {
    const hit = ids.find((id) => id.includes(p))
    if (hit) return hit
  }
  return ids[0]
}

function Sub({ value, onChange, options }: { value: string; onChange: (v: string) => void; options: { id: string; label: string }[] }) {
  return (
    <ToggleGroup
      type="single"
      size="sm"
      variant="outline"
      value={value}
      onValueChange={(v) => v && onChange(v)}
      className="mb-2"
    >
      {options.map((o) => (
        <ToggleGroupItem key={o.id} value={o.id} className="px-3 text-xs">
          {o.label}
        </ToggleGroupItem>
      ))}
    </ToggleGroup>
  )
}

export function ConnectCard({ sessionKey }: { sessionKey: string }) {
  const { t } = useI18n()
  const models = useModels()
  const thinking = useThinking()
  const settings = useSettings()
  const ids = useMemo(() => (models.data?.models ?? []).filter((m) => m.enabled).map((m) => m.id), [models.data])
  const fallbackIds = ids.length ? ids : ['claude-sonnet-4.5', 'claude-haiku-4.5']
  const [model, setModel] = useState('')
  const [fast, setFast] = useState('')
  const [think, setThink] = useState(false)
  const [key, setKey] = useState('')
  const [ccVariant, setCcVariant] = useState('shell')
  const [sdkLang, setSdkLang] = useState('python')
  const [curlApi, setCurlApi] = useState('openai')

  useEffect(() => {
    if (sessionKey) setKey(sessionKey)
  }, [sessionKey])
  useEffect(() => {
    if (!model || !fallbackIds.includes(model)) setModel(pick(fallbackIds, 'sonnet', 'claude') ?? '')
    if (!fast || !fallbackIds.includes(fast)) setFast(pick(fallbackIds, 'haiku', 'sonnet') ?? '')
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [fallbackIds.join('|')])

  const base = origin()
  const suffix = thinking.data?.suffix || '-thinking'
  const m = model + (think ? suffix : '')
  const k = key.trim() || KEY_PLACEHOLDER

  const snippets = {
    ccShell: `export ANTHROPIC_BASE_URL="${base}"
export ANTHROPIC_AUTH_TOKEN="${k}"
export ANTHROPIC_MODEL="${m}"
export ANTHROPIC_SMALL_FAST_MODEL="${fast}"
claude`,
    ccJson: JSON.stringify(
      {
        env: {
          ANTHROPIC_BASE_URL: base,
          ANTHROPIC_AUTH_TOKEN: k,
          ANTHROPIC_MODEL: m,
          ANTHROPIC_SMALL_FAST_MODEL: fast,
        },
      },
      null,
      2,
    ),
    codexToml: `# ~/.codex/config.toml
model = "${m}"
model_provider = "kiro"

[model_providers.kiro]
name = "Kiro-Go"
base_url = "${base}/v1"
env_key = "KIRO_API_KEY"
wire_api = "responses"`,
    codexEnv: `export KIRO_API_KEY="${k}"
codex`,
    py: `from openai import OpenAI

client = OpenAI(base_url="${base}/v1", api_key="${k}")

resp = client.chat.completions.create(
    model="${m}",
    messages=[{"role": "user", "content": "Hello!"}],
)
print(resp.choices[0].message.content)`,
    ts: `import OpenAI from 'openai'

const client = new OpenAI({ baseURL: '${base}/v1', apiKey: '${k}' })

const resp = await client.chat.completions.create({
  model: '${m}',
  messages: [{ role: 'user', content: 'Hello!' }],
})
console.log(resp.choices[0].message.content)`,
    curlOpenai: `curl ${base}/v1/chat/completions \\
  -H "Authorization: Bearer ${k}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${m}",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'`,
    curlAnthropic: `curl ${base}/v1/messages \\
  -H "x-api-key: ${k}" \\
  -H "anthropic-version: 2023-06-01" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${m}",
    "max_tokens": 1024,
    "messages": [{"role": "user", "content": "Hello!"}]
  }'`,
    curlResponses: `curl ${base}/v1/responses \\
  -H "Authorization: Bearer ${k}" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${m}",
    "input": "Hello!"
  }'`,
  }

  return (
    <Card className="gap-0 py-0 shadow-xs">
      <div className="flex items-start gap-3 px-5 pt-5 sm:px-6">
        <div className="grid size-9 shrink-0 place-items-center rounded-lg bg-accent text-accent-foreground">
          <Plug className="size-[18px]" />
        </div>
        <div>
          <h3 className="font-semibold leading-tight">{t('keys.connect')}</h3>
          <p className="mt-1 text-sm text-muted-foreground">{t('keys.connectDesc')}</p>
        </div>
      </div>
      <div className="space-y-4 px-5 py-5 sm:px-6">
        <div className="grid gap-3 sm:grid-cols-[1fr_1fr_auto]">
          <div className="space-y-1.5 sm:col-span-3">
            <Label htmlFor="cx-key" className="text-xs text-muted-foreground">
              {t('keys.snippetKey')}
            </Label>
            <Input
              id="cx-key"
              className="font-mono text-[13px]"
              placeholder={KEY_PLACEHOLDER}
              value={key}
              autoComplete="off"
              spellCheck={false}
              onChange={(e) => setKey(e.target.value)}
            />
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="cx-model" className="text-xs text-muted-foreground">
              {t('keys.snippetModel')}
            </Label>
            <Select value={model} onValueChange={setModel}>
              <SelectTrigger id="cx-model" className="w-full font-mono text-[13px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {fallbackIds.map((id) => (
                  <SelectItem key={id} value={id} className="font-mono text-[13px]">
                    {id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="cx-fast" className="text-xs text-muted-foreground">
              {t('keys.snippetFast')}
            </Label>
            <Select value={fast} onValueChange={setFast}>
              <SelectTrigger id="cx-fast" className="w-full font-mono text-[13px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {fallbackIds.map((id) => (
                  <SelectItem key={id} value={id} className="font-mono text-[13px]">
                    {id}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="flex items-end gap-2 pb-2">
            <Switch id="cx-think" checked={think} onCheckedChange={setThink} />
            <Label htmlFor="cx-think" className="gap-1.5 text-sm">
              <Brain className="size-4 text-muted-foreground" />
              {t('keys.snippetThinking')}
            </Label>
          </div>
        </div>
        <p className="text-xs text-muted-foreground">
          {settings.data && !settings.data.requireApiKey ? t('keys.snippetKeyOpen') : t('keys.snippetKeyHint')}{' '}
          {think && t('keys.snippetThinkingHint', { suffix })}
        </p>

        <Tabs defaultValue="claude-code">
          <TabsList className="flex-wrap">
            <TabsTrigger value="claude-code">Claude Code</TabsTrigger>
            <TabsTrigger value="codex">Codex CLI</TabsTrigger>
            <TabsTrigger value="sdk">OpenAI SDK</TabsTrigger>
            <TabsTrigger value="curl">curl</TabsTrigger>
          </TabsList>
          <TabsContent value="claude-code" className="mt-3">
            <Sub
              value={ccVariant}
              onChange={setCcVariant}
              options={[
                { id: 'shell', label: t('keys.ccShell') },
                { id: 'json', label: '~/.claude/settings.json' },
              ]}
            />
            <CodeBlock code={ccVariant === 'shell' ? snippets.ccShell : snippets.ccJson} />
            <p className="mt-2 text-xs text-muted-foreground">{t('keys.ccHint')}</p>
          </TabsContent>
          <TabsContent value="codex" className="mt-3 space-y-2">
            <CodeBlock code={snippets.codexToml} />
            <CodeBlock code={snippets.codexEnv} />
            <p className="text-xs text-muted-foreground">{t('keys.codexHint')}</p>
          </TabsContent>
          <TabsContent value="sdk" className="mt-3">
            <Sub
              value={sdkLang}
              onChange={setSdkLang}
              options={[
                { id: 'python', label: 'Python' },
                { id: 'ts', label: 'TypeScript' },
              ]}
            />
            <CodeBlock code={sdkLang === 'python' ? snippets.py : snippets.ts} />
            <p className="mt-2 text-xs text-muted-foreground">
              {sdkLang === 'python' ? 'pip install openai' : 'npm install openai'}
            </p>
          </TabsContent>
          <TabsContent value="curl" className="mt-3">
            <Sub
              value={curlApi}
              onChange={setCurlApi}
              options={[
                { id: 'openai', label: '/v1/chat/completions' },
                { id: 'anthropic', label: '/v1/messages' },
                { id: 'responses', label: '/v1/responses' },
              ]}
            />
            <CodeBlock
              code={
                curlApi === 'openai' ? snippets.curlOpenai : curlApi === 'anthropic' ? snippets.curlAnthropic : snippets.curlResponses
              }
            />
          </TabsContent>
        </Tabs>
      </div>
    </Card>
  )
}
