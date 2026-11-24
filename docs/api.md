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
| `GET /metrics` | Prometheus | off unless `KIRO_METRICS=1`; see [configuration.md](configuration.md#metrics) |
| `/admin`, `/admin/api/*` | console | session cookie or admin password header; not a public API |

### Authentication

Send a Kiro-Go API key as `Authorization: Bearer <key>` or `x-api-key: <key>` (the scheme is
case-insensitive). Keys are created on the **API & Keys** page and can carry request or token quotas. If no
keys exist and verification is off, the endpoints are open — the console warns about this.

A key can also be restricted ([configuration.md](configuration.md#api-key-restrictions)): to a list of
models (then `/v1/models` lists only those when the key is sent), to a number of requests per minute, and
until an expiry date.

### Headers on every response

`request-id` and `x-request-id` (same value, also shown in Request logs), `retry-after` on 429/529/503, and
CORS headers that echo the browser's requested headers (so the Anthropic and OpenAI browser SDKs work).

## Anthropic Messages

Supported: text, images (base64, or `url` sources fetched by the gateway — see [Images and documents](#images-and-documents)),
`document` blocks (base64 PDF and other formats, `text` and `content` sources, `url` sources; also inside
`tool_result`), tools and `tool_choice` (`auto`, `any`, `tool`, `none`), `tool_result`
including `is_error`, `system` as string or blocks, `stop_sequences`, `temperature`, `top_p`, `max_tokens`,
streaming with the official event order (`message_start` → content blocks → `message_delta` →
`message_stop`) plus `ping` heartbeats, `thinking` and `output_config.effort` (see [Thinking](#thinking)),
and the `web_search` server tool (relayed through Kiro's MCP endpoint).

Usage reports `input_tokens`, `output_tokens` and, when Kiro reports them, `cache_read_input_tokens` /
`cache_creation_input_tokens`. The response `model` is the model name the request sent (aliases are resolved
internally; request logs show the resolved Kiro id).

Thinking blocks carry a `signature`: streams send `content_block_start` with `"signature": ""` and a
`signature_delta` right before the block's `content_block_stop`; non-streaming blocks include the field. Kiro
does not provide a signature, so the value is an **opaque constant marker that cannot be verified**. Replayed
`thinking` and `redacted_thinking` blocks in history are accepted and not forwarded.

Not supported or approximated:
- Assistant prefill (a final `assistant` message) is rejected with 400, as current Claude models do.
- `top_k`, `metadata`, `service_tier`, `container`, `context_management` are accepted and ignored.
- `file` sources (Files API `file_id`) for images and documents are rejected with 400.
- Citations on documents (`citations`) are not returned.

## OpenAI Chat Completions

Supported: `messages` (system, developer, user, assistant, tool), images as data URLs or `http(s)` URLs
(fetched by the gateway), `file` parts with `file_data` (data URL + `filename`; see
[Images and documents](#images-and-documents)), `tools` and
`tool_choice` (`none`, `auto`, `required`, named function), `parallel_tool_calls`, `max_tokens` /
`max_completion_tokens`, `temperature`, `top_p`, `stop` (string or up to 4), `stream` with
`stream_options.include_usage`, `reasoning_effort` / `reasoning` (see [Thinking](#thinking)).

Streams start with a `role: "assistant"` chunk, keep one `created` value, and send `: keep-alive` comments
while waiting. Reasoning is returned as `reasoning_content` by default (configurable to `<think>` tags or a
`thinking` field) and counted in `usage.completion_tokens_details.reasoning_tokens` (estimated from the
reasoning text). Tool calls and text can appear in the same message. `model` echoes the requested name.

Not supported: `n > 1` (400), `logprobs` (returned as `null`), audio input/output, `file` parts that reference
an uploaded `file_id` (400).

## OpenAI Responses

Supported: string or item `input` (messages, function calls and outputs, custom/freeform tools, Codex
`additional_tools` and namespaces, `input_image` as data URL or `http(s)` URL, `input_file` with `file_data`;
replayed `reasoning` items are accepted and dropped), `instructions`, `tools`, `tool_choice`, `parallel_tool_calls`,
`max_output_tokens`, `temperature`, `top_p`, `reasoning.effort`, `previous_response_id`, `store`, `metadata`.

Streaming follows the official sequence (`response.created`, `response.in_progress` once, output item and
content part events with their `.done` counterparts, `response.completed` / `response.incomplete` /
`response.failed`) with a `sequence_number` on every event and no trailing `[DONE]`. Failed responses carry
a full response object with `error.code` (`context_length_exceeded`, `rate_limit_exceeded`,
`insufficient_quota`, `server_error`), which Codex uses to decide whether to compact or retry.

With thinking on, reasoning is returned as a `reasoning` output item (`summary: [{type: "summary_text",
text}]`) before the message, streamed as `response.output_item.added` → `response.reasoning_summary_part.added`
→ `response.reasoning_summary_text.delta`… → `response.reasoning_summary_text.done` →
`response.reasoning_summary_part.done` → `response.output_item.done`. The item is part of the final and the
stored response, and `usage.output_tokens_details.reasoning_tokens` is estimated from its text. `model` echoes
the requested name, also on stored responses.

Responses are stored (unless `store: false`) for 30 days for `previous_response_id`, `GET` and `DELETE`.

Not supported: background mode, hosted tools other than function/custom tools (`file_search`,
`image_generation`, `local_shell` are dropped), `input_file` / `input_image` by `file_id` (400).
`include: ["reasoning.encrypted_content"]` is accepted and ignored: reasoning items carry no
`encrypted_content`.

## Images and documents

**Images** are forwarded as base64 (png, jpeg, gif, webp). `http(s)` image URLs (Anthropic `source.type: "url"`,
OpenAI `image_url.url`, Responses `input_image.image_url`) are downloaded by the gateway:

- `https` only (`KIRO_FETCH_ALLOW_HTTP=1` also allows `http`); at most 3 redirects; 10 s timeout; 5 MB per
  image; at most 20 URLs per request.
- The host must resolve to public addresses only — loopback, private, link-local (including cloud metadata),
  multicast, CGNAT and reserved ranges are refused, on every redirect hop and again when connecting. Through
  the configured outbound proxy the hostname is checked before the request is handed to the proxy.
- The `Content-Type` must be `image/png|jpeg|gif|webp` and the bytes must match one of those formats.
- Nothing is cached. `KIRO_FETCH_IMAGE_URLS=0` disables fetching; such requests get a 400 that says so.

**Documents** — Anthropic `document` blocks, OpenAI Chat `file` parts (`file.file_data` data URL +
`filename`) and Responses `input_file` items — are sent to Kiro as documents (`input_file` on the
`/v1/responses` transport, `documents` on `generateAssistantResponse`). Formats: pdf, csv, doc, docx, html, md,
txt, xls, xlsx (from the media type, or the file extension when the type is generic). Anthropic `text` and
`content` sources are inlined into the prompt as `<document name="…">…</document>`. Document `url` sources
(Anthropic) and `file_url` (Responses) are fetched under the same rules as images (PDF or UTF-8 text).

Limits: 5 documents per message and 4.5 MB per document. Across a conversation, only the 5 newest documents
are sent upstream (Kiro's per-request limit); older ones are replaced in history by a short note, so long
conversations with many attachments keep working. Names are reduced to letters,
digits, spaces, hyphens, parentheses and brackets and made unique. An unsupported format, invalid base64, an
oversize or surplus document is rejected with `400 invalid_request_error` naming the problem — nothing is
dropped silently.

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
| API key expired | 401 | `authentication_error` ("API key expired") | `authentication_error` |
| Model not allowed for the API key | 403 | `permission_error` | `invalid_request_error` / `model_not_allowed` |
| API key over its requests-per-minute limit | 429 + `retry-after` | `rate_limit_error` | `rate_limit_error` / `rate_limit_exceeded` |
| Unknown or disabled model | 404 / 400 | `not_found_error` | `model_not_found` |
| Body too large | 413 | `request_too_large` | `invalid_request_error` |
| Upstream throttling | 429 + `retry-after` | `rate_limit_error` | `rate_limit_exceeded` |
| Account quota exhausted | 429 | `rate_limit_error` | `insufficient_quota` |
| No account available / upstream overloaded | 529 (Anthropic) / 503 (OpenAI) + `retry-after` | `overloaded_error` | `server_error` |
| Anything else upstream | 500 | `api_error` | `server_error` |

A request that Kiro rejects as invalid is not retried on other accounts; transient failures are retried
within the request's attempt budget (see [architecture.md](architecture.md#retries-and-account-health)).
