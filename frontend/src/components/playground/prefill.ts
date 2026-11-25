/**
 * "Open in Playground": turns a captured request body into the playground's
 * model, system prompt and text messages. The hand-over goes through a
 * short-lived sessionStorage entry (never the URL), read once by the page.
 */
import type { ChatMessage, Protocol } from './stream'

export interface PlaygroundPrefill {
  model: string
  protocol: Protocol
  system: string
  messages: ChatMessage[]
  /** Non-text parts left out (images, documents, tool calls and results…). */
  stripped: number
  requestId: string
  savedAt: number
}

const KEY = 'kiro_playground_prefill'
const TTL_MS = 5 * 60_000

type Json = Record<string, unknown>
const isObj = (v: unknown): v is Json => !!v && typeof v === 'object' && !Array.isArray(v)

/** Text of a content value (string or array of parts); counts the parts that are not text. */
function textOf(content: unknown, count: { stripped: number }): string {
  if (typeof content === 'string') return content
  if (!Array.isArray(content)) {
    if (content != null) count.stripped++
    return ''
  }
  const out: string[] = []
  for (const part of content) {
    if (typeof part === 'string') out.push(part)
    else if (isObj(part) && typeof part.text === 'string' && ['text', 'input_text', 'output_text'].includes(String(part.type ?? 'text'))) out.push(part.text)
    else if (isObj(part) && (part.type === 'thinking' || part.type === 'redacted_thinking' || part.type === 'reasoning')) continue
    else count.stripped++
  }
  return out.join('\n\n')
}

/**
 * Parses a captured request body (Anthropic Messages, OpenAI Chat Completions
 * or Responses). Returns null when the body is missing, truncated or has no
 * text messages.
 */
export function prefillFromBody(body: string | undefined, endpoint: string, fallbackModel: string, requestId: string): PlaygroundPrefill | null {
  if (!body) return null
  let req: unknown
  try {
    req = JSON.parse(body)
  } catch {
    return null // truncated by the capture limit, or not JSON
  }
  if (!isObj(req)) return null
  const count = { stripped: 0 }
  const system: string[] = []
  const messages: ChatMessage[] = []
  const push = (role: unknown, content: unknown) => {
    const text = textOf(content, count).trim()
    if (role === 'system' || role === 'developer') {
      if (text) system.push(text)
      return
    }
    if (role !== 'user' && role !== 'assistant') {
      count.stripped++ // tool / function messages
      return
    }
    if (text) messages.push({ role, content: text })
  }

  if (endpoint === 'responses') {
    if (typeof req.instructions === 'string' && req.instructions.trim()) system.push(req.instructions.trim())
    if (typeof req.input === 'string') push('user', req.input)
    else if (Array.isArray(req.input)) {
      for (const item of req.input) {
        if (isObj(item) && (item.type === 'message' || (item.type === undefined && 'role' in item))) push(item.role, item.content)
        else if (isObj(item) && item.type === 'reasoning') continue
        else count.stripped++
      }
    }
  } else {
    if (req.system !== undefined) {
      const s = textOf(req.system, count).trim()
      if (s) system.push(s)
    }
    if (!Array.isArray(req.messages)) return null
    for (const m of req.messages) if (isObj(m)) push(m.role, m.content)
  }
  if (!messages.length) return null
  return {
    model: fallbackModel || (typeof req.model === 'string' ? req.model : ''),
    protocol: endpoint === 'claude' ? 'anthropic' : 'openai',
    system: system.join('\n\n'),
    messages,
    stripped: count.stripped,
    requestId,
    savedAt: Date.now(),
  }
}

export function stashPrefill(p: PlaygroundPrefill) {
  try {
    sessionStorage.setItem(KEY, JSON.stringify(p))
    return true
  } catch {
    return false // storage full or unavailable
  }
}

/** Reads and removes the hand-over entry; stale entries are ignored. */
export function takePrefill(): PlaygroundPrefill | null {
  try {
    const raw = sessionStorage.getItem(KEY)
    sessionStorage.removeItem(KEY)
    if (!raw) return null
    const p = JSON.parse(raw) as PlaygroundPrefill
    if (!p || !Array.isArray(p.messages) || Date.now() - p.savedAt > TTL_MS) return null
    return p
  } catch {
    return null
  }
}
