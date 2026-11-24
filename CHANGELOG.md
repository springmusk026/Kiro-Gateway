# Changelog

## Unreleased

### APIs
- Documents: Anthropic `document` blocks (base64, `text`, `content` and `url` sources, also inside
  `tool_result`), OpenAI Chat `file` parts and Responses `input_file` items are forwarded to Kiro as documents
  (pdf, csv, doc, docx, html, md, txt, xls, xlsx). Unsupported formats, files over 4.5 MB and more than 5
  documents per request are rejected with a clear 400 instead of being dropped.
- Image URLs: `http(s)` images (Anthropic `url` sources, OpenAI `image_url`, Responses `input_image`) are
  downloaded by the gateway with SSRF protection (https only, public addresses only on every redirect, 10 s,
  5 MB, image types checked by header and bytes, outbound proxy honoured). `KIRO_FETCH_IMAGE_URLS=0` turns
  this off; `KIRO_FETCH_ALLOW_HTTP=1` allows plain http.
- Responses: `reasoning` output items with the official `reasoning_summary_*` stream events, in the final
  and stored response, and `usage.output_tokens_details.reasoning_tokens`; replayed `reasoning` input items
  and `include: ["reasoning.encrypted_content"]` are accepted.
- Anthropic: thinking blocks carry a `signature` (`signature_delta` in streams). It is an opaque marker,
  not a verifiable signature.
- OpenAI Chat: `usage.completion_tokens_details.reasoning_tokens` when reasoning is returned.
- Responses echo the model name the client sent (Anthropic, OpenAI and Responses bodies, stream events and
  stored responses); request logs keep the resolved Kiro model id.
- Inline images and documents no longer count against the text payload budget, so a large attachment no
  longer truncates the conversation history.

## 2.0.0 — independent project

Kiro-Go began as a fork of [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go) at version 1.1.5. Since
then the gateway, its storage and its console have been rebuilt, so it is now maintained as its own project.
Configuration from the original (`data/config.json`) is still read and can be migrated into PostgreSQL from
the console.

### APIs
- Upstream errors mapped to the official status codes and types: "prompt is too long" (Claude Code
  auto-compaction), `context_length_exceeded` (Codex), `model_not_found`, `rate_limit_exceeded`,
  `insufficient_quota`, `overloaded_error` with `retry-after`; client errors are no longer retried on other
  accounts.
- SSE heartbeats (`ping` / `: keep-alive`) so proxies don't cut long requests.
- OpenAI Chat: `role` in the first chunk, `tool_choice`, `parallel_tool_calls`, `stop`,
  `max_completion_tokens`, `stream_options.include_usage`, `n > 1` rejected, text kept next to tool calls.
- Anthropic: `stop_sequences`, `tool_result.is_error`.
- Responses: official event sequence with `sequence_number` and `.done` events, full response objects
  (including on `response.failed` with `error.code`), `GET` / `DELETE /v1/responses/{id}`, Codex multi-agent
  tools, `additional_tools` and namespaces.
- `/v1/models` with `created`, Anthropic fields and `GET /v1/models/{id}`; request ids on every response;
  CORS for browser SDKs; JSON 404/405 and 413 errors.
- Long tool names shortened for Kiro and restored in responses on every API.

### Thinking
- Thinking follows the request (Anthropic `thinking` / `output_config.effort`, OpenAI `reasoning_effort`,
  Responses `reasoning`) instead of requiring a `-thinking` model; the suffix still works.
- Effort forwarded natively to models that declare it, clamped to their supported levels.
- *When to think* policy (follow request / always / never) and a configurable default thinking length.

### Upstream and accounts
- `/v1/responses` transport alongside `generateAssistantResponse`, chosen per account with fallback.
- Request shapes, headers and conversation ids aligned with the official Kiro CLI.
- Per-request upstream attempt budget, Kiro `retry-after` hints honoured, exponential back-off, throttle
  cooldowns, quota parking, forced token refresh before marking an account unhealthy, optional per-account
  concurrency limit.
- Conversation affinity for prompt-cache reuse, real cache-token accounting, Claude Code start-up probe
  answered locally.
- Live model catalogue from Kiro with enable/disable per model and configurable aliases.

### Storage and logging
- PostgreSQL backend for settings, accounts, keys and stored responses, with automatic schema migrations.
- Automatic first-start import of `config.json` and a console prompt to verify the transfer and delete the
  file; `kiro-go migrate` / `kiro-go export` commands.
- Full request history (timing, routing, tokens, cache, credits, attempts, optional bodies) and an admin
  audit log, written asynchronously so inference is never slowed down.

### Console
- New admin console (TanStack Start + shadcn/ui): Overview with traffic and usage breakdowns, Accounts,
  Models, Playground, API & Keys with client setup snippets, Request logs, Settings; English, 中文, Tiếng
  Việt; light/dark; mobile layouts.

### Security
- Generated admin password instead of a default, login rate limiting, constant-time comparisons, security
  headers, request size limits, validated upstream URLs, atomic config writes.

## 1.1.5 and earlier

See the history of [Quorinex/Kiro-Go](https://github.com/Quorinex/Kiro-Go).
