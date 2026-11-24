import { apiFetch } from '@/lib/api'

export type Protocol = 'openai' | 'anthropic'
export interface ChatMessage {
  role: 'user' | 'assistant'
  content: string
}
export interface StreamEvents {
  onText: (delta: string) => void
  onThinking: (delta: string) => void
  onUsage: (u: { input?: number; output?: number }) => void
}
export interface ChatParams {
  protocol: Protocol
  model: string
  system: string
  temperature: number
  maxTokens: number
  stream: boolean
  messages: ChatMessage[]
  /** Ask for reasoning through the request itself (no model suffix). */
  thinking?: boolean
  /** Reasoning effort; "auto" leaves it to the model. */
  effort?: string
}

export function buildRequest(p: ChatParams) {
  if (p.protocol === 'anthropic') {
    const body: Record<string, unknown> = {
      model: p.model,
      max_tokens: p.maxTokens,
      temperature: p.temperature,
      stream: p.stream,
      messages: p.messages,
    }
    if (p.system) body.system = p.system
    if (p.thinking) {
      body.thinking = { type: 'adaptive' }
      if (p.effort && p.effort !== 'auto') body.output_config = { effort: p.effort }
    }
    return { adminPath: '/playground/messages', publicPath: '/v1/messages', body }
  }
  const messages = p.system ? [{ role: 'system', content: p.system }, ...p.messages] : p.messages
  const body: Record<string, unknown> = { model: p.model, temperature: p.temperature, max_tokens: p.maxTokens, stream: p.stream, messages }
  if (p.thinking) body.reasoning_effort = p.effort && p.effort !== 'auto' ? p.effort : 'medium'
  return { adminPath: '/playground/chat/completions', publicPath: '/v1/chat/completions', body }
}

function errorFrom(body: unknown, status: number): Error {
  const b = body as { error?: { message?: string } | string; message?: string } | undefined
  const msg = (typeof b?.error === 'string' ? b.error : b?.error?.message) || b?.message || `HTTP ${status}`
  return new Error(msg)
}

/** Sends one chat request through the gateway and reports deltas as they arrive. */
export async function runChat(p: ChatParams, ev: StreamEvents, signal: AbortSignal): Promise<void> {
  const req = buildRequest(p)
  const res = await apiFetch(req.adminPath, { method: 'POST', body: JSON.stringify(req.body), signal })
  if (!res.ok) {
    let body: unknown
    try {
      body = await res.json()
    } catch {
      /* not JSON */
    }
    throw errorFrom(body, res.status)
  }
  if (!p.stream) {
    const j = await res.json()
    if (p.protocol === 'anthropic') {
      for (const b of j.content ?? []) {
        if (b.type === 'text') ev.onText(b.text ?? '')
        if (b.type === 'thinking') ev.onThinking(b.thinking ?? '')
      }
      ev.onUsage({ input: j.usage?.input_tokens, output: j.usage?.output_tokens })
    } else {
      const m = j.choices?.[0]?.message ?? {}
      if (m.reasoning_content) ev.onThinking(m.reasoning_content)
      ev.onText(m.content ?? '')
      ev.onUsage({ input: j.usage?.prompt_tokens, output: j.usage?.completion_tokens })
    }
    return
  }
  const reader = res.body!.getReader()
  const dec = new TextDecoder()
  let buf = ''
  for (;;) {
    const { value, done } = await reader.read()
    if (done) break
    buf += dec.decode(value, { stream: true })
    let i: number
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i).trim()
      buf = buf.slice(i + 1)
      if (!line.startsWith('data:')) continue
      const data = line.slice(5).trim()
      if (!data || data === '[DONE]') continue
      let e: Record<string, any> // eslint-disable-line @typescript-eslint/no-explicit-any
      try {
        e = JSON.parse(data)
      } catch {
        continue
      }
      if (e.error) throw errorFrom(e, 500)
      if (p.protocol === 'anthropic') {
        if (e.type === 'content_block_delta') {
          if (e.delta?.type === 'text_delta') ev.onText(e.delta.text ?? '')
          if (e.delta?.type === 'thinking_delta') ev.onThinking(e.delta.thinking ?? '')
        } else if (e.type === 'message_start') {
          ev.onUsage({ input: e.message?.usage?.input_tokens })
        } else if (e.type === 'message_delta' && e.usage) {
          ev.onUsage({ output: e.usage.output_tokens })
        }
      } else {
        const d = e.choices?.[0]?.delta
        if (d?.reasoning_content) ev.onThinking(d.reasoning_content)
        if (d?.content) ev.onText(d.content)
        if (e.usage) ev.onUsage({ input: e.usage.prompt_tokens, output: e.usage.completion_tokens })
      }
    }
  }
}
