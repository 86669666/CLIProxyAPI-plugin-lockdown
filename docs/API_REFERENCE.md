# API Reference

This document describes the HTTP, Server-Sent Events (SSE), and WebSocket interfaces exposed by CLIProxyAPI. The examples use the default local address:

```text
http://127.0.0.1:8317
```

Unless noted otherwise, JSON request bodies use `Content-Type: application/json` and JSON responses use `Content-Type: application/json`.

## Authentication

### Proxy API authentication

The model proxy routes under `/v1`, `/v1beta`, `/openai/v1`, and `/backend-api/codex` use the configured `api-keys` list. When no access provider is configured, the server preserves legacy behavior and allows requests without a key.

A configured key may be supplied in any of these forms:

```http
Authorization: Bearer <API_KEY>
X-Api-Key: <API_KEY>
X-Goog-Api-Key: <API_KEY>
```

Gemini-compatible clients may alternatively use either query parameter:

```text
?key=<API_KEY>
?auth_token=<API_KEY>
```

Missing and invalid credentials return HTTP `401`:

```json
{"error":"Missing API key"}
```

```json
{"error":"Invalid API key"}
```

### Management API authentication

Management routes are available only when a management secret is configured and Home mode is disabled. Every authenticated Management API request accepts either:

```http
Authorization: Bearer <MANAGEMENT_KEY>
X-Management-Key: <MANAGEMENT_KEY>
```

The management key may come from `remote-management.secret-key`, the `MANAGEMENT_PASSWORD` environment variable, or the runtime-only local management password. Remote clients also require `remote-management.allow-remote: true`.

Management authentication failures use a simple JSON object:

```json
{"error":"missing management key"}
```

Relevant statuses are:

- `401 Unauthorized`: missing or invalid management key.
- `403 Forbidden`: remote management is disabled, no management key is configured, or the source IP is temporarily banned.
- `404 Not Found`: management routes are unavailable, including Home mode or no configured management secret.

After five failed attempts, a client IP is banned for 30 minutes.

### Unauthenticated routes

`/`, `/healthz`, `/management.html`, `/anthropic/callback`, `/codex/callback`, and `/antigravity/callback` do not use proxy API-key authentication. `/keep-alive` has its own optional local-password authentication. The Management OAuth callback route is availability-gated but intentionally does not use the Management API key middleware because it receives browser redirects.

## Common Proxy Conventions

### Model routing

The `model` field selects an available credential and upstream provider. Provider-native requests are translated through the server's canonical protocol pipeline. A model prefix may be required when `force-model-prefix` is enabled.

### Upstream headers

Safe upstream response headers may be forwarded. Hop-by-hop and sensitive headers, including `Authorization`, `Proxy-Authorization`, `Connection`, and `Upgrade`, are filtered.

### Standard JSON error

Most proxy failures use an OpenAI-compatible envelope:

```json
{
  "error": {
    "message": "descriptive error message",
    "type": "invalid_request_error",
    "code": "model_not_found"
  }
}
```

The `code` field may be omitted when no stable mapping exists.

## Public and Utility Endpoints

| Method | Path | Authentication | Response |
|---|---|---|---|
| `GET` | `/` | None | Generic HTML landing page. |
| `GET` | `/healthz` | None | `{"status":"ok"}`. |
| `HEAD` | `/healthz` | None | HTTP `200` with no body. |
| `GET` | `/management.html` | None | Management control-panel HTML, or `404` when disabled/unavailable. |
| `GET` | `/keep-alive` | Optional local password | `{"status":"ok"}`; registered only when process keep-alive mode is enabled. |
| `GET` | `/anthropic/callback` | None | OAuth callback HTML; query: `code`, `state`, `error`, `error_description`. |
| `GET` | `/codex/callback` | None | OAuth callback HTML; same query fields. |
| `GET` | `/antigravity/callback` | None | OAuth callback HTML; same query fields. |

When `/keep-alive` is protected, supply `Authorization: Bearer <LOCAL_PASSWORD>` or `X-Local-Password: <LOCAL_PASSWORD>`. An invalid password returns `401` with `{"error":"invalid password"}`.

## OpenAI-Compatible API

All endpoints in this section require proxy authentication when `api-keys` is configured.

### Endpoint list

| Method | Path | Request | Response |
|---|---|---|---|
| `GET` | `/v1/models` | Optional `client_version` query. Anthropic clients may send `Anthropic-Version` or a `claude-cli` user agent. | OpenAI model list, Anthropic model list, or Codex client catalog. |
| `POST` | `/v1/chat/completions` | OpenAI Chat Completions JSON; `stream: true` enables SSE. Responses-format `input`/`instructions` payloads are also accepted and converted. | Chat completion object or chat completion chunks. |
| `POST` | `/v1/completions` | Legacy OpenAI Completions JSON; `stream: true` enables SSE. | Completion object or completion chunks. |
| `POST` | `/v1/responses` | OpenAI Responses JSON; `stream: true` enables SSE. | Response object or Responses events. |
| `GET` | `/v1/responses` | WebSocket upgrade, then `response.create`/`response.append` JSON messages. | Responses events as WebSocket text messages. |
| `POST` | `/v1/responses/compact` | Responses compaction JSON. Streaming is rejected. | Compacted response object. |
| `POST` | `/v1/images/generations` | JSON image generation request. | OpenAI image response or SSE when supported and `stream: true`. |
| `POST` | `/v1/images/edits` | `multipart/form-data` or JSON image edit request. | OpenAI image response or SSE when supported and `stream: true`. |
| `POST` | `/openai/v1/videos` | OpenAI-style video creation JSON. | Video job object. |
| `GET` | `/openai/v1/videos/{video_id}` | Path parameter `video_id`. | Video job/status object. |
| `GET` | `/openai/v1/videos/{video_id}/content` | Optional `variant=video`; other variants are rejected. | Video bytes or upstream download response. |
| `POST` | `/v1/videos` | xAI-native video request. | xAI-native job response. |
| `POST` | `/v1/videos/generations` | Alias of `/v1/videos`. | xAI-native job response. |
| `POST` | `/v1/videos/edits` | xAI-native edit request. | xAI-native job response. |
| `POST` | `/v1/videos/extensions` | xAI-native extension request. | xAI-native job response. |
| `GET` | `/v1/videos/{request_id}` | Path parameter `request_id`. | xAI-native job/status response. |
| `POST` | `/v1/alpha/search` | Codex search JSON, maximum 16 MiB. | Upstream Codex search response. |

The Codex direct aliases have identical request and response behavior:

| Method | Path | Equivalent route |
|---|---|---|
| `POST` | `/backend-api/codex/responses` | `POST /v1/responses` |
| `GET` | `/backend-api/codex/responses` | `GET /v1/responses` WebSocket |
| `POST` | `/backend-api/codex/responses/compact` | `POST /v1/responses/compact` |
| `POST` | `/backend-api/codex/alpha/search` | `POST /v1/alpha/search` |

### List models

```bash
curl -sS http://127.0.0.1:8317/v1/models \
  -H 'Authorization: Bearer YOUR_API_KEY'
```

```json
{
  "object": "list",
  "data": [
    {
      "id": "gpt-5",
      "object": "model",
      "created": 1754000000,
      "owned_by": "openai"
    }
  ]
}
```

### Chat completion

```bash
curl -sS http://127.0.0.1:8317/v1/chat/completions \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-5",
    "messages": [{"role":"user","content":"Hello"}],
    "stream": false
  }'
```

```json
{
  "id": "chatcmpl_example",
  "object": "chat.completion",
  "model": "gpt-5",
  "choices": [
    {
      "index": 0,
      "message": {"role": "assistant", "content": "Hello!"},
      "finish_reason": "stop"
    }
  ],
  "usage": {"prompt_tokens": 8, "completion_tokens": 3, "total_tokens": 11}
}
```

### Responses API

```bash
curl -sS http://127.0.0.1:8317/v1/responses \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-5",
    "input": "Summarize this repository in one sentence.",
    "stream": false
  }'
```

```json
{
  "id": "resp_example",
  "object": "response",
  "status": "completed",
  "model": "gpt-5",
  "output": [
    {
      "type": "message",
      "role": "assistant",
      "content": [{"type":"output_text","text":"A multi-provider AI API proxy."}]
    }
  ]
}
```

### Image generation

```bash
curl -sS http://127.0.0.1:8317/v1/images/generations \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "gpt-image-1",
    "prompt": "A small robot reading API documentation",
    "size": "1024x1024",
    "response_format": "b64_json"
  }'
```

```json
{
  "created": 1754000000,
  "data": [{"b64_json":"<base64-image-data>"}]
}
```

For `/v1/images/edits`, multipart requests normally contain `image`, `prompt`, and optionally `mask`, `model`, `size`, `quality`, `response_format`, and related provider-supported fields.

## Anthropic-Compatible API

| Method | Path | Request | Response |
|---|---|---|---|
| `POST` | `/v1/messages` | Anthropic Messages JSON; omitted/false `stream` is non-streaming, true is SSE. | Anthropic message object or message events. |
| `POST` | `/v1/messages/count_tokens` | Anthropic token-count request. | Token count response. |
| `GET` | `/v1/models` | Send `Anthropic-Version` or a `claude-cli` user agent to select Anthropic formatting. | Anthropic model list. |

```bash
curl -sS http://127.0.0.1:8317/v1/messages \
  -H 'X-Api-Key: YOUR_API_KEY' \
  -H 'Anthropic-Version: 2023-06-01' \
  -H 'Content-Type: application/json' \
  -d '{
    "model": "claude-sonnet-4-5",
    "max_tokens": 128,
    "messages": [{"role":"user","content":"Hello"}]
  }'
```

```json
{
  "id": "msg_example",
  "type": "message",
  "role": "assistant",
  "model": "claude-sonnet-4-5",
  "content": [{"type":"text","text":"Hello!"}],
  "stop_reason": "end_turn",
  "usage": {"input_tokens": 8, "output_tokens": 3}
}
```

## Gemini-Compatible API

| Method | Path | Request | Response |
|---|---|---|---|
| `GET` | `/v1beta/models` | Optional Gemini API-key query authentication. | Gemini `models` list. |
| `GET` | `/v1beta/models/{model}` | Model name in path. | One Gemini model object or `404`. |
| `POST` | `/v1beta/models/{model}:generateContent` | Gemini `GenerateContentRequest`. | `GenerateContentResponse`. |
| `POST` | `/v1beta/models/{model}:streamGenerateContent` | Gemini `GenerateContentRequest`; optional `alt=sse`. | SSE by default; raw translated stream when `alt` is non-empty. |
| `POST` | `/v1beta/models/{model}:countTokens` | Gemini token-count request. | Gemini token count response. |
| `POST` | `/v1beta/interactions` | Interactions request with model/agent target and optional `stream`. | JSON interaction or SSE events. |

```bash
curl -sS 'http://127.0.0.1:8317/v1beta/models/gemini-2.5-pro:generateContent?key=YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{
    "contents": [{"role":"user","parts":[{"text":"Hello"}]}]
  }'
```

```json
{
  "candidates": [
    {
      "content": {
        "role": "model",
        "parts": [{"text":"Hello!"}]
      },
      "finishReason": "STOP"
    }
  ],
  "usageMetadata": {"promptTokenCount": 4, "candidatesTokenCount": 2, "totalTokenCount": 6}
}
```

Unknown POST actions under `/v1beta/models/*action` are not dispatched and normally produce an empty successful response; clients should use only the three documented actions.

## Server-Sent Events

### Starting a stream

- OpenAI Chat/Completions, Responses, Anthropic Messages, images, and Interactions use a JSON `"stream": true` field.
- Gemini uses the `:streamGenerateContent` action.
- Streaming responses use `Content-Type: text/event-stream` unless a Gemini non-empty `alt` mode requests direct translated chunks.
- The server sets `Cache-Control: no-cache` and keeps the connection open until completion, client cancellation, or a terminal error.
- If configured, periodic SSE comments are emitted as keep-alives. They carry no model output and should be ignored by clients.

OpenAI-style stream example:

```bash
curl -N http://127.0.0.1:8317/v1/chat/completions \
  -H 'Authorization: Bearer YOUR_API_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"model":"gpt-5","messages":[{"role":"user","content":"Count to three"}],"stream":true}'
```

```text
data: {"id":"chatcmpl_example","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"One"}}]}

data: {"id":"chatcmpl_example","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":", two, three"},"finish_reason":"stop"}]}

data: [DONE]

```

### Streaming errors

If an error occurs before the first stream payload, the server can still return a normal HTTP status and JSON error body. After headers or payloads have been sent, the HTTP status cannot change; the error is delivered in-band.

Responses streams use a top-level Responses event:

```text
data: {"type":"error","code":"rate_limit_exceeded","message":"upstream quota exceeded","sequence_number":4}

```

Gemini and Interactions SSE streams use an explicit error event:

```text
event: error
data: {"error":{"message":"upstream unavailable","type":"server_error","code":"internal_server_error"}}

```

Clients must handle both non-`2xx` bootstrap errors and terminal in-stream error events.

## WebSocket APIs

### Responses WebSocket

Upgrade either route:

```text
GET /v1/responses
GET /backend-api/codex/responses
```

Use the normal proxy API key during the HTTP upgrade. For example:

```http
GET /v1/responses HTTP/1.1
Host: 127.0.0.1:8317
Upgrade: websocket
Connection: Upgrade
Authorization: Bearer YOUR_API_KEY
Sec-WebSocket-Key: <generated-key>
Sec-WebSocket-Version: 13
```

The first request must be a `response.create` message and must include a model:

```json
{
  "type": "response.create",
  "model": "gpt-5",
  "input": [{"role":"user","content":"Hello"}],
  "stream": true
}
```

Subsequent turns may use `response.create` again or `response.append` to extend the active transcript. Depending on upstream WebSocket support, `previous_response_id` may be passed through or the server may replay a merged transcript.

Each server event is one JSON WebSocket text message, for example:

```json
{"type":"response.created","sequence_number":0,"response":{"id":"resp_example","status":"in_progress","output":[]}}
```

```json
{"type":"response.completed","sequence_number":7,"response":{"id":"resp_example","status":"completed","output":[],"usage":{"input_tokens":8,"output_tokens":3,"total_tokens":11}}}
```

Protocol errors are sent as `{"type":"error", ...}` events. Unsupported message types, a missing model on the first `response.create`, or `response.append` before `response.create` are rejected in-band.

### AI Studio provider relay

`GET /v1/ws` is a provider-side relay used to attach an AI Studio/Gemini execution channel; it is not a normal end-user generation endpoint. Proxy authentication is enforced only when `ws-auth` is enabled.

Messages use this envelope:

```json
{
  "id": "request-unique-id",
  "type": "http_response",
  "payload": {}
}
```

Supported types are:

- `http_request`: server-to-provider HTTP-style request.
- `http_response`: provider-to-server non-streaming terminal response.
- `stream_start`, `stream_chunk`, `stream_end`: streaming response lifecycle.
- `error`: terminal request error.
- `ping`, `pong`: application-level heartbeat messages.

The relay also uses WebSocket ping frames every 30 seconds, has a 60-second liveness read deadline, a 10-second write deadline, and a 64 MiB inbound message limit. A new connection with the same provider identity replaces the previous connection.

## Management API

Base path: `/v0/management`. Except for `/oauth-callback`, every route below requires Management API authentication.

Successful mutation endpoints generally return:

```json
{"status":"ok"}
```

### Configuration and runtime settings

Simple setting updates use the same body for `PUT` and `PATCH`:

```json
{"value":true}
```

| Methods | Path | Body / response |
|---|---|---|
| `GET` | `/config` | Full effective configuration as JSON. |
| `GET` | `/config.yaml` | Raw YAML with comments preserved. |
| `PUT` | `/config.yaml` | Raw YAML document; validates and replaces configuration. |
| `GET` | `/latest-version` | `{"latest-version":"vX.Y.Z"}` or a GitHub lookup error. |
| `GET`, `PUT`, `PATCH` | `/debug` | `{"debug":bool}`; update with `{"value":bool}`. |
| `GET`, `PUT`, `PATCH` | `/logging-to-file` | `{"logging-to-file":bool}`. |
| `GET`, `PUT`, `PATCH` | `/logs-max-total-size-mb` | Integer setting; negative updates clamp to `0`. |
| `GET`, `PUT`, `PATCH` | `/error-logs-max-files` | Integer setting; negative updates become `10`. |
| `GET`, `PUT`, `PATCH` | `/usage-statistics-enabled` | Boolean setting. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/proxy-url` | String setting; `DELETE` clears it. |
| `GET`, `PUT`, `PATCH` | `/quota-exceeded/switch-project` | Boolean setting. |
| `GET`, `PUT`, `PATCH` | `/quota-exceeded/switch-preview-model` | Boolean setting. |
| `POST` | `/reset-quota` | `{"auth_index":"..."}`; returns status and affected models. |
| `GET`, `PUT`, `PATCH` | `/request-log` | Boolean request-log setting. |
| `GET`, `PUT`, `PATCH` | `/ws-auth` | Boolean `/v1/ws` authentication setting. |
| `GET`, `PUT`, `PATCH` | `/request-retry` | Integer retry count. |
| `GET`, `PUT`, `PATCH` | `/max-retry-interval` | Integer retry interval. |
| `GET`, `PUT`, `PATCH` | `/force-model-prefix` | Boolean model-prefix setting. |
| `GET`, `PUT`, `PATCH` | `/routing/strategy` | `{"strategy":"round-robin"}`; update with `{"value":"round-robin"}` or `{"value":"fill-first"}`. |

Example:

```bash
curl -sS -X PATCH http://127.0.0.1:8317/v0/management/debug \
  -H 'Authorization: Bearer YOUR_MANAGEMENT_KEY' \
  -H 'Content-Type: application/json' \
  -d '{"value":true}'
```

### API keys and provider configuration

`PUT` list endpoints accept either a bare JSON array or `{"items":[...]}`. String-list `PATCH` accepts `{"index":0,"value":"new"}` or `{"old":"old","new":"new"}`. String-list `DELETE` uses `?index=N` or `?value=VALUE`.

Structured provider-list `PATCH` requests use an index or match key plus a partial value:

```json
{
  "index": 0,
  "value": {
    "proxy-url": "http://127.0.0.1:7890",
    "excluded-models": ["model-to-skip"]
  }
}
```

| Methods | Path | Configuration type |
|---|---|---|
| `GET`, `PUT`, `PATCH`, `DELETE` | `/api-keys` | Client-facing proxy API keys. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/gemini-api-key` | Gemini API-key entries. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/interactions-api-key` | Gemini Interactions API-key entries. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/claude-api-key` | Claude API-key entries. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/codex-api-key` | Codex-compatible API-key entries. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/xai-api-key` | xAI API-key entries. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/openai-compatibility` | Named OpenAI-compatible upstreams. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/vertex-api-key` | Vertex-compatible credential entries. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/oauth-excluded-models` | Provider/channel to excluded-model patterns. |
| `GET`, `PUT`, `PATCH`, `DELETE` | `/oauth-model-alias` | Provider/channel to model alias entries. |
| `GET` | `/api-key-usage` | Usage grouped by provider and `base_url|api_key`. |
| `GET` | `/usage-queue?count=N` | Pops the oldest queued usage records; default `count=1`. |

Provider entry fields mirror the corresponding `config.yaml` sections. Common fields include `api-key`, `base-url`, `proxy-url`, `prefix`, `headers`, `models`, `excluded-models`, `priority`, `websockets`, and `disable-cooling`, where supported by that provider.

### Logs

| Method | Path | Parameters / response |
|---|---|---|
| `GET` | `/logs` | Query: `cursor`, `after`, `limit`; returns log lines and pagination metadata. |
| `DELETE` | `/logs` | Clears the main log. |
| `GET` | `/request-error-logs` | Lists captured request-error log files. |
| `GET` | `/request-error-logs/{name}` | Downloads one safe, named error log. |
| `GET` | `/request-log-by-id/{id}` | Returns one request log by request ID. |

### Authentication files and model metadata

| Method | Path | Parameters / body / response |
|---|---|---|
| `GET` | `/auth-files` | Lists runtime auth records and their `auth_index` values. |
| `GET` | `/auth-files/models?name=FILE` | Models available to one auth file. |
| `GET` | `/model-definitions/{channel}` | Static model metadata for a provider channel. |
| `GET` | `/auth-files/download?name=FILE.json` | Downloads one auth JSON file. |
| `POST` | `/auth-files` | Multipart `file`/`files` upload, or raw JSON with `?name=FILE.json`. |
| `DELETE` | `/auth-files?name=FILE.json` | Deletes one file; `all=true` deletes all JSON auth files. Batch names are also accepted by the handler. |
| `PATCH` | `/auth-files/status` | `{"name":"file-or-auth-id","disabled":true}`. |
| `PATCH` | `/auth-files/fields` | `{"name":"file-or-auth-id","field.path":value}`; updates arbitrary metadata paths. |
| `POST` | `/vertex/import` | Multipart `file` containing a Vertex credential; optional `location` form/query value. |

Multipart batch uploads may return `207 Multi-Status`:

```json
{
  "status": "partial",
  "uploaded": 1,
  "files": ["good.json"],
  "failed": [{"name":"bad.txt","error":"file must be .json"}]
}
```

### OAuth sessions

The callback endpoint's complete path is `GET|POST /v0/management/oauth-callback`; the remaining paths in this section are relative to `/v0/management`.

| Method | Path | Parameters / response |
|---|---|---|
| `GET` | `/anthropic-auth-url` | Starts Anthropic OAuth and returns authorization data including `url` and `state`. |
| `GET` | `/codex-auth-url` | Starts Codex OAuth. |
| `GET` | `/antigravity-auth-url` | Starts Antigravity OAuth. |
| `GET` | `/kimi-auth-url` | Starts Kimi device/OAuth flow. |
| `GET` | `/xai-auth-url` | Starts xAI device/OAuth flow. |
| `GET` | `/get-auth-status?state=STATE` | Returns the current OAuth session status. |
| `DELETE` | `/oauth-session?state=STATE` | Cancels a pending OAuth session. |
| `GET` | `/oauth-callback` | Availability-gated callback: `provider`, `code`, `state`, `error`, `error_description`. |
| `POST` | `/oauth-callback` | Availability-gated callback JSON shown below. |

The callback JSON accepts either discrete values or a complete redirect URL:

```json
{
  "provider": "codex",
  "redirect_url": "http://localhost/callback?code=...&state=...",
  "code": "optional-when-in-redirect-url",
  "state": "optional-when-in-redirect-url",
  "error": "optional-error"
}
```

A successful callback returns `{"status":"ok"}`. Invalid, expired, mismatched, or already-completed states return `400`, `404`, or `409` with `{"status":"error","error":"..."}`.

### Plugins

| Method | Path | Body / response |
|---|---|---|
| `GET` | `/plugins` | Plugin discovery, configured/registered/enabled state, metadata, config fields, and menus. |
| `GET` | `/plugin-store` | Aggregated plugin-store catalog. |
| `POST` | `/plugin-store/{id}/install` | Optional `source` and `version` query parameters; installs and enables a plugin. |
| `DELETE` | `/plugins/{id}` | Removes/disables a plugin according to runtime lock rules. |
| `PATCH` | `/plugins/{id}/enabled` | `{"enabled":true}`. |
| `GET` | `/plugins/{id}/config` | Current plugin configuration. |
| `PUT` | `/plugins/{id}/config` | Replaces plugin configuration with a JSON object. |
| `PATCH` | `/plugins/{id}/config` | Merges a JSON object into plugin configuration. |

Loaded plugin binaries may be locked against overwrite/removal. Such operations return `409 Conflict`, commonly with `restart_required: true`.

Enabled plugins may dynamically register additional authenticated routes directly under `/v0/management/<plugin-path>`. They may also expose unauthenticated static/resource GET routes under `/plugins/{plugin_id}/<resource-path>`. Dynamic paths cannot contain Gin parameters (`:` or `*`), whitespace, or traversal components, and conflicts with built-in routes are rejected.

### Management API call utility

`POST /v0/management/api-call` performs a restricted outbound HTTP request:

```json
{
  "auth_index": "AUTH_INDEX_FROM_AUTH_FILES",
  "method": "GET",
  "url": "https://api.example.com/v1/ping",
  "header": {"Authorization":"Bearer $TOKEN$"},
  "data": ""
}
```

`authIndex` and `AuthIndex` are accepted aliases for `auth_index`. `$TOKEN$` is replaced from the selected credential when possible. The endpoint blocks unsafe target addresses and uses the credential proxy, then global proxy, then direct connection.

Successful execution returns HTTP `200` even when the upstream status is an error:

```json
{
  "status_code": 200,
  "header": {"Content-Type":["application/json"]},
  "body": "{\"status\":\"ok\"}"
}
```

## Optional pprof Server

When `pprof.enable` is true, a separate HTTP server listens on `pprof.addr`. It does not use proxy or management authentication, so it should be bound to a trusted interface only.

| Methods | Path | Description |
|---|---|---|
| `GET` | `/debug/pprof/` | Profile index. |
| `GET` | `/debug/pprof/cmdline` | Process command line. |
| `GET`, `POST` | `/debug/pprof/symbol` | Symbol lookup. |
| `GET` | `/debug/pprof/profile` | CPU profile. |
| `GET` | `/debug/pprof/trace` | Execution trace. |
| `GET` | `/debug/pprof/allocs` | Allocation profile. |
| `GET` | `/debug/pprof/block` | Blocking profile. |
| `GET` | `/debug/pprof/goroutine` | Goroutine profile. |
| `GET` | `/debug/pprof/heap` | Heap profile. |
| `GET` | `/debug/pprof/mutex` | Mutex profile. |
| `GET` | `/debug/pprof/threadcreate` | Thread-creation profile. |

## Error Codes and Handling

### HTTP status codes

| Status | Meaning in this server |
|---|---|
| `200 OK` | Successful request; Management `api-call` also wraps upstream non-`2xx` statuses inside its JSON response. |
| `207 Multi-Status` | Partial success for batch auth-file upload or deletion. |
| `400 Bad Request` | Invalid JSON, unsupported request shape, missing required field, invalid model action, or invalid query parameter. |
| `401 Unauthorized` | Missing/invalid proxy API key, missing/invalid management key, or invalid keep-alive password. |
| `402 Payment Required` | Upstream billing/quota state; may trigger credential rotation. |
| `403 Forbidden` | Management access policy, insufficient upstream permission/quota, or temporary management IP ban. |
| `404 Not Found` | Route/model/file/auth not found, disabled image endpoint, or unavailable management routes. |
| `409 Conflict` | OAuth state already used, plugin update requires restart, or immutable virtual auth operation. |
| `413 Content Too Large` | Upload or request body exceeds a handler limit. |
| `429 Too Many Requests` | Upstream or local rate limit. |
| `500 Internal Server Error` | Internal execution, persistence, or plugin failure. |
| `502 Bad Gateway` | Invalid/unavailable upstream response or management outbound request failure. |
| `503 Service Unavailable` | Auth manager/provider unavailable or no usable credential. |
| `504 Gateway Timeout` | Upstream gateway timeout. |

### OpenAI-compatible error mapping

| HTTP status | `error.type` | `error.code` |
|---|---|---|
| `401` | `authentication_error` | `invalid_api_key` |
| `403` | `permission_error` | `insufficient_quota` |
| `404` | `invalid_request_error` | `model_not_found` |
| `429` | `rate_limit_error` | `rate_limit_exceeded` |
| `5xx` | `server_error` | `internal_server_error` |

Responses streaming additionally maps `408` to `request_timeout`; other `4xx` statuses use `invalid_request_error`.

### Client guidance

1. Check the HTTP status before parsing a non-streaming body.
2. Preserve upstream JSON errors; the proxy intentionally forwards already-valid upstream error payloads.
3. For SSE, handle both bootstrap HTTP errors and in-band terminal error events.
4. For WebSockets, treat a `type: "error"` message or connection close as terminal for the current request.
5. Retry `429`, `502`, `503`, and `504` only with bounded backoff. Do not blindly retry validation or authentication errors.
6. Never log API keys, management keys, OAuth codes, auth-file contents, or substituted `$TOKEN$` values.
