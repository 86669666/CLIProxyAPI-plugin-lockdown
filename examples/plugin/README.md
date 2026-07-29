# Standard Dynamic Library Plugin Examples

This directory contains examples for the CLIProxyAPI plugin C ABI. The
examples cover provider capabilities, request and response processing,
management resources, host callbacks, and Go-only extensions.

## Directory Layout

### Provider and protocol capabilities

- `simple/`: complete mixed-capability skeleton in Go, C, and Rust. It
  declares the provider-native surface supported by the example plugin.
- `model/`: model provider capability only.
- `auth/`: authentication provider capability only.
- `frontend-auth/`: frontend authentication provider capability only.
- `frontend-auth-exclusive/`: frontend authentication provider that becomes
  the only request authentication provider when selected.
- `executor/`: executor capability only.
- `protocol-format/`: minimal executor focused on input and output format
  declarations.
- `request-translator/`: request translation capability only.
- `request-normalizer/`: request normalization capability only.
- `response-translator/`: response translation capability only.
- `response-normalizer/`: response normalization capability only.
- `thinking/`: thinking applier capability only.
- `usage/`: usage observer capability only.

### Go-only extensions

- `codex-service-tier/`: request normalizer that can set the Codex
  `gpt-5.5` service tier to `priority`.
- `scheduler/`: scheduler that can select a configured auth ID, delegate to a
  built-in scheduler, or deny a pick.
- `claude-web-search-router/`: ModelRouter and executor for Claude Code's
  built-in `web_search`, with antigravity, Codex, xAI, and Tavily backends.
  See [`claude-web-search-router/README.md`](claude-web-search-router/README.md).
- `cli/`: command-line capability only.
- `management-api/`: Management API and resource capability only.
- `host-callback/`: minimal plugin resource that demonstrates host callbacks.
- `host-callback-auth-files/`: plugin resource that calls host auth-file
  callbacks.
- `host-model-callback/`: plugin resource that calls host model-execution
  callbacks.

Most provider and protocol examples contain `go/`, `c/`, and `rust/`
implementations. Specialized examples provide only the implementation
language needed by that example.

## Building the Examples

The Makefile builds the standard examples for the current platform. The
dynamic library extension is selected automatically:

- `.dylib` on macOS
- `.so` on Linux
- `.dll` on Windows

From the repository root, run:

```bash
make -C examples/plugin list
make -C examples/plugin build
```

Build artifacts are written to `examples/plugin/bin`. Remove them with:

```bash
make -C examples/plugin clean
```

The Makefile covers the examples listed in its `EXAMPLES` variable. The
standalone `scheduler` example can be built directly:

```bash
cd examples/plugin/scheduler/go
go build -buildmode=c-shared -o /tmp/cliproxy-scheduler-plugin.so .
rm -f /tmp/cliproxy-scheduler-plugin.so /tmp/cliproxy-scheduler-plugin.h
```

For a single standard example, enter its implementation directory and use
the toolchain appropriate to that language. For example:

```bash
cd examples/plugin/model/go
go build -buildmode=c-shared -o /tmp/cliproxy-model-plugin.so .
rm -f /tmp/cliproxy-model-plugin.so /tmp/cliproxy-model-plugin.h
```

## Loading a Plugin

Build the dynamic library, place it under the configured plugin directory,
and add its path to the server configuration. The basename normally matches
the plugin ID used under `plugins.configs`.

```yaml
plugins:
  enabled: true
  dir: "plugins"
  path:
    - /absolute/path/to/examples/plugin/bin/model-go.so
  configs:
    model-go:
      enabled: true
      priority: 1
```

Use the plugin's own README when it defines additional configuration fields
or request requirements. The complete mixed-capability example also
documents the plugin registration and configuration protocol in
[`simple/README.md`](simple/README.md).

## Codex Service Tier

`codex-service-tier` declares the request normalization capability. When
`fast` is `true`, it sets `service_tier` to `priority` for requests where
`req.ToFormat` is `codex` and `req.Model` is `gpt-5.5`.

```yaml
plugins:
  configs:
    codex-service-tier:
      enabled: true
      priority: 1
      fast: false
```

## Host Auth Files Callback

`host-callback-auth-files` declares the Management API capability and exposes
a browser resource named `Host Auth Files`. The resource demonstrates
`host.auth.list`, `host.auth.get` (the physical JSON file),
`host.auth.get_runtime`, and `host.auth.save`.

```yaml
plugins:
  configs:
    host-callback-auth-files:
      enabled: true
      priority: 1
```

See [`host-callback-auth-files/README.md`](host-callback-auth-files/README.md)
for resource URLs and query parameters.

## Host Model Callback

`host-model-callback` declares the Management API capability and exposes a
browser resource named `Host Model Callback`. The resource calls
`host.model.execute` for non-streaming requests and
`host.model.execute_stream` plus `host.model.stream_read` for streaming
requests. It demonstrates explicit stream cleanup with
`host.model.stream_close` and an `implicit_close=true` option for RPC-scope
host cleanup.

When the resource forwards its `host_callback_id`, CPA identifies the plugin
that initiated the host model callback and skips that plugin's interceptors
for the nested execution. This prevents recursive interception by the caller
while allowing other enabled plugins to process the nested request.

```yaml
plugins:
  configs:
    host-model-callback:
      enabled: true
      priority: 1
```

The default example model is `gpt-5.5`. The request succeeds only when the
current CPA model and authentication configuration can route that model.

## Scheduler

The `scheduler` example declares the scheduler capability. It can select a
configured auth ID from the candidate list, delegate to the built-in
`fill-first` or `round-robin` scheduler, or reject picks when `deny` is true.

```yaml
plugins:
  configs:
    scheduler:
      enabled: true
      priority: 1
      auth_id: ""
      delegate: ""
      deny: false
```

- `auth_id` selects a matching candidate when `delegate` is empty.
- `delegate` accepts `""`, `fill-first`, or `round-robin`; another non-empty
  value leaves the pick unhandled.
- `deny` returns a scheduler error.

## Notes

`protocol-format` uses a minimal executor because format declarations belong
to executor capabilities.

`host-callback` uses a minimal plugin resource because host callbacks are
invoked from plugin methods and are not standalone capabilities.

Menu resources returned by `management.register` through the `resources`
field are exposed by CPA under
`/v0/resource/plugins/<pluginID>/...`. Authenticated plugin Management API
routes remain under `/v0/management/...`.
