# API reference

Kiro-Go exposes the request and response shapes of the official Anthropic and OpenAI APIs. This page lists
what is supported, where behaviour differs, and how gateway-specific features (thinking, models, errors)
work.

## Endpoints

| Method & path | API | Notes |
|---|---|---|
| `POST /v1/messages` | Anthropic Messages | also `/messages` and `/anthropic/v1/messages` |
| `POST /v1/messages/count_tokens` | Anthropic | computed locally, no upstream call |
| `POST /v1/chat/completions` | OpenAI Chat Completions | also `/chat/completions` |
| `POST /v1/responses` | OpenAI Responses | streaming and non-streaming; `previous_response_id` |
| `GET /v1/responses/{id}` / `DELETE /v1/responses/{id}` | OpenAI Responses | stored responses; `POST …/cancel` answers 400 (no background mode) |
| `GET /v1/models` / `GET /v1/models/{id}` | both | OpenAI list shape with Anthropic fields added |
| `GET /v1/stats` | gateway | pool and request counters |
| `/admin`, `/admin/api/*` | console | admin password header; not a public API |

### Authentication

Send a Kiro-Go API key as `Authorization: Bearer <key>` or `x-api-key: <key>` (the scheme is
case-insensitive). Keys are created on the **API & Keys** page and can carry request or token quotas. If no
keys exist and verification is off, the endpoints are open — the console warns about this.

### Headers on every response

`request-id` and `x-request-id` (same value, also shown in Request logs), `retry-after` on 429/529/503, and
CORS headers that echo the browser's requested headers (so the Anthropic and OpenAI browser SDKs work).

## Anthropic Messages

Supported: text, images (base64), tools and `tool_choice` (`auto`, `any`, `tool`, `none`), `tool_result`
including `is_error`, `system` as string or blocks, `stop_sequences`, `temperature`, `top_p`, `max_tokens`,
streaming with the official event order (`message_start` → content blocks → `message_delta` →
`message_stop`) plus `ping` heartbeats, `thinking` and `output_config.effort` (see [Thinking](#thinking)),
and the `web_search` server tool (relayed through Kiro's MCP endpoint).

Usage reports `input_tokens`, `output_tokens` and, when Kiro reports them, `cache_read_input_tokens` /
`cache_creation_input_tokens`.

Not supported or approximated:
- Assistant prefill (a final `assistant` message) is rejected with 400, as current Claude models do.
- `top_k`, `metadata`, `service_tier`, `container`, `context_management` are accepted and ignored.
- `document` / PDF blocks and `url` image sources are not forwarded; send base64 images and extracted text.
- Thinking blocks carry no `signature` (Kiro does not provide one); clients that replay thinking blocks
  should not need it to be verified.

## OpenAI Chat Completions

Supported: `messages` (system, developer, user, assistant, tool), images as data URLs, `tools` and
`tool_choice` (`none`, `auto`, `required`, named function), `parallel_tool_calls`, `max_tokens` /
`max_completion_tokens`, `temperature`, `top_p`, `stop` (string or up to 4), `stream` with
`stream_options.include_usage`, `reasoning_effort` / `reasoning` (see [Thinking](#thinking)).

Streams start with a `role: "assistant"` chunk, keep one `created` value, and send `: keep-alive` comments
while waiting. Reasoning is returned as `reasoning_content` by default (configurable to `<think>` tags or a
`thinking` field). Tool calls and text can appear in the same message.

Not supported: `n > 1` (400), `logprobs` (returned as `null`), audio input/output, non-image `file` parts,
`http(s)` image URLs.

## OpenAI Responses

Supported: string or item `input` (messages, function calls and outputs, custom/freeform tools, Codex
`additional_tools` and namespaces), `instructions`, `tools`, `tool_choice`, `parallel_tool_calls`,
`max_output_tokens`, `temperature`, `top_p`, `reasoning.effort`, `previous_response_id`, `store`, `metadata`.

Streaming follows the official sequence (`response.created`, `response.in_progress` once, output item and
content part events with their `.done` counterparts, `response.completed` / `response.incomplete` /
`response.failed`) with a `sequence_number` on every event and no trailing `[DONE]`. Failed responses carry
a full response object with `error.code` (`context_length_exceeded`, `rate_limit_exceeded`,
`insufficient_quota`, `server_error`), which Codex uses to decide whether to compact or retry.

Responses are stored (unless `store: false`) for 30 days for `previous_response_id`, `GET` and `DELETE`.

Not supported: background mode, hosted tools other than function/custom tools (`file_search`,
`image_generation`, `local_shell` are dropped), `include: ["reasoning.encrypted_content"]`.

## Thinking

Thinking is decided per request:

| Client sends | Result |
|---|---|
| Anthropic `thinking: {type: "enabled", budget_tokens: N}` | on, length N |
| Anthropic `thinking: {type: "adaptive"}` (+ `output_config.effort`) | on, length from effort |
| Anthropic `thinking: {type: "disabled"}` or nothing | off |
| OpenAI `reasoning_effort: "low" \| "medium" \| "high" …` or `reasoning: {effort}` | on, length from effort |
| OpenAI `reasoning_effort: "none"` | off |
| Responses `reasoning: {effort}` | on, length from effort |
| model name ending in the suffix (default `-thinking`) | on — the suffix always wins, for older setups |

The **When to think** setting (Models → Thinking) can force thinking on (`always`) or off (`never`) for every request.

Effort maps to the thinking length the model is given: `minimal` 2K, `low` 8K, `medium` 24K, `high` 64K,
`xhigh` 128K, `max` 200K tokens; without an effort or budget the default length applies (200K, configurable).

**Native effort.** Kiro's model catalogue declares, per model, which effort levels it accepts (Claude models
`low`…`max` under `output_config.effort`; GPT models `low`…`high` under `reasoning.effort`). Kiro-Go reads
that declaration, picks the closest accepted level, and forwards it the same way the official Kiro client
does — only on the `/v1/responses` upstream transport and only for models that declare it. The Models page
and `/v1/models` (`supported_reasoning_efforts`) show the levels per model.

## Models

`/v1/models` lists the live catalogue fetched from Kiro (refreshed every 30 minutes), configured aliases, and
— for older clients — each model's thinking-suffix variant. Entries carry `id`, `object`, `created`,
`owned_by`, `type`, `display_name`, `created_at`, `context_length`, `max_output_tokens`, `rate_multiplier`
and, where available, `supported_reasoning_efforts`. Disabled models are hidden. Model names are normalised
(`claude-sonnet-4-5`, dated snapshots and common aliases resolve to Kiro's ids).

## Errors

Errors use each API's own envelope — Anthropic `{"type":"error","error":{"type","message"}}`, OpenAI
`{"error":{"message","type","param","code"}}` — for HTTP errors and for errors inside a stream.

| Situation | HTTP | Anthropic type | OpenAI type / code |
|---|---|---|---|
| Malformed request, unsupported parameter | 400 | `invalid_request_error` | `invalid_request_error` |
| Prompt longer than the model's window | 400 | `invalid_request_error` ("prompt is too long: N tokens > M maximum") | `context_length_exceeded` |
| Missing or wrong API key | 401 | `authentication_error` | `authentication_error` |
| Unknown or disabled model | 404 / 400 | `not_found_error` | `model_not_found` |
| Body too large | 413 | `request_too_large` | `invalid_request_error` |
| Upstream throttling | 429 + `retry-after` | `rate_limit_error` | `rate_limit_exceeded` |
| Account quota exhausted | 429 | `rate_limit_error` | `insufficient_quota` |
| No account available / upstream overloaded | 529 (Anthropic) / 503 (OpenAI) + `retry-after` | `overloaded_error` | `server_error` |
| Anything else upstream | 500 | `api_error` | `server_error` |

A request that Kiro rejects as invalid is not retried on other accounts; transient failures are retried
within the request's attempt budget (see [architecture.md](architecture.md#retries-and-account-health)).
