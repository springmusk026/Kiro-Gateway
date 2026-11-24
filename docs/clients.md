# Connecting clients

Replace `http://localhost:8080` with your server and `<key>` with an API key from the **API & Keys** page
(that page generates these snippets for you). Model ids come from the **Models** page — for example
`claude-sonnet-4.5`, `claude-opus-4.5`, `claude-haiku-4.5`.

## Claude Code

```bash
export ANTHROPIC_BASE_URL=http://localhost:8080
export ANTHROPIC_AUTH_TOKEN=<key>
export ANTHROPIC_MODEL=claude-sonnet-4.5
export ANTHROPIC_SMALL_FAST_MODEL=claude-haiku-4.5
claude
```

Or put the same variables under `"env"` in `~/.claude/settings.json`.

- **Thinking**: Claude Code sends its own `thinking` setting and Kiro-Go follows it. To force a thinking
  length, add `MAX_THINKING_TOKENS=16000`; to think on every request regardless, set *When to think* to
  *Always* in Settings.
- **Compaction**: when a conversation outgrows the model's window Kiro-Go answers with Anthropic's
  "prompt is too long" error, which triggers Claude Code's automatic compaction.
- **Start-up probe**: Claude Code's quota probe is answered locally, so launching it costs no request.
- Keep **conversation affinity** on (the default) so each session stays on one account and reuses Kiro's
  prompt cache.

## Codex CLI

`~/.codex/config.toml`:

```toml
model = "claude-sonnet-4.5"
model_provider = "kiro"
model_reasoning_effort = "medium"   # optional: low | medium | high

[model_providers.kiro]
name = "Kiro-Go"
base_url = "http://localhost:8080/v1"
env_key = "KIRO_API_KEY"
wire_api = "responses"
```

```bash
export KIRO_API_KEY=<key>
codex
```

Codex uses the Responses API, including freeform/custom tools, namespaces and `additional_tools`.

## OpenAI SDK

```python
from openai import OpenAI

client = OpenAI(base_url="http://localhost:8080/v1", api_key="<key>")
resp = client.chat.completions.create(
    model="claude-sonnet-4.5",
    messages=[{"role": "user", "content": "Hello!"}],
    reasoning_effort="medium",          # optional; returned as message.reasoning_content
)
print(resp.choices[0].message.content)
```

```ts
import OpenAI from 'openai'

const client = new OpenAI({ baseURL: 'http://localhost:8080/v1', apiKey: '<key>' })
const resp = await client.responses.create({ model: 'claude-sonnet-4.5', input: 'Hello!' })
console.log(resp.output_text)
```

## Anthropic SDK

```python
import anthropic

client = anthropic.Anthropic(base_url="http://localhost:8080", api_key="<key>")
msg = client.messages.create(
    model="claude-sonnet-4.5",
    max_tokens=16000,
    thinking={"type": "enabled", "budget_tokens": 8000},   # optional
    messages=[{"role": "user", "content": "Hello!"}],
)
```

## Cline, Roo Code, Continue, Cursor and other IDE agents

Choose an **OpenAI-compatible** provider (or **Anthropic** where the tool supports a custom base URL):

| Field | Value |
|---|---|
| Base URL | `http://localhost:8080/v1` (OpenAI) or `http://localhost:8080` (Anthropic) |
| API key | `<key>` |
| Model | a model id from the Models page |

Model metadata (context window, max output) is available from `GET /v1/models/{id}`.

## LiteLLM

```yaml
model_list:
  - model_name: kiro-sonnet
    litellm_params:
      model: anthropic/claude-sonnet-4.5
      api_base: http://localhost:8080
      api_key: <key>
```

## cURL

```bash
curl http://localhost:8080/v1/messages \
  -H "x-api-key: <key>" -H "anthropic-version: 2023-06-01" -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4.5","max_tokens":1024,"messages":[{"role":"user","content":"Hello!"}]}'

curl http://localhost:8080/v1/chat/completions \
  -H "Authorization: Bearer <key>" -H "Content-Type: application/json" \
  -d '{"model":"claude-sonnet-4.5","messages":[{"role":"user","content":"Hello!"}]}'
```

## Legacy thinking suffix

Clients that can't send thinking parameters can append the suffix (default `-thinking`) to the model name,
e.g. `claude-sonnet-4.5-thinking`. The suffix is configurable in Settings → Models & thinking.
