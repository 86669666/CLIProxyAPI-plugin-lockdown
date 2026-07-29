# Configuration Guide

CLIProxyAPI reads `config.yaml` by default. Use `--config <path>` to select a
file. Start from [`config.example.yaml`](../config.example.yaml), then restrict
its permissions because it can contain API and management keys.

```bash
cp config.example.yaml config.yaml
chmod 600 config.yaml
./cli-proxy-api --config ./config.yaml
```

The working-directory `.env` file is loaded early in startup. Use it for
storage credentials and deployment secrets; do not commit it. YAML defines
server behavior and provider entries, while environment variables are intended
for deployment-specific secrets and store selection.

## Contents

- [Secure minimal configuration](#secure-minimal-configuration)
- [Loading and precedence](#loading-and-precedence)
- [Server settings](#server-settings)
- [Authentication](#authentication)
- [Provider credentials](#provider-credentials)
- [Payload rules](#payload-rules)
- [Plugins](#plugins)
- [Environment variables](#environment-variables)
- [Storage backends](#storage-backends)
- [OAuth authentication files](#oauth-authentication-files)
- [Security best practices](#security-best-practices)

## Secure minimal configuration

This local-only configuration requires an API key and disables the Management
API and plugins.

```yaml
host: "127.0.0.1"
port: 8317

auth-dir: "/var/lib/cli-proxy-api/auths"
api-keys:
  - "replace-with-a-long-random-client-key"

remote-management:
  allow-remote: false
  secret-key: ""

plugins:
  enabled: false

debug: false
pprof:
  enable: false
```

For reverse-proxy deployments, retain this loopback binding and terminate TLS
at the trusted proxy. If the server listens on a network interface, configure
TLS and firewall rules for trusted clients only.

## Loading and precedence

1. The default configuration file is `config.yaml`; `--config <path>` overrides
   the path.
2. `.env` in the process working directory is loaded early. Existing process
   environment values are not replaced by `.env` values.
3. `HOME_JWT` or `--home-jwt` enables Home control-plane mode. Configuration is
   fetched from Home; Postgres, Git, and object stores are not initialized.
4. Otherwise, `PGSTORE_DSN` selects Postgres. If unset, `OBJECTSTORE_ENDPOINT`
   selects object storage. If neither is set, `GITSTORE_GIT_URL` selects Git.
   File storage is the fallback.
5. `DEPLOY=cloud` allows an initially empty remote-store configuration. It does
   not select a storage backend.

> **Plugin lockdown:** This repository recognizes `CLIPROXY_DISABLE_PLUGINS`
> before plugin bootstrap. Define it in the actual service environment or in the
> working-directory `.env` so the startup policy is in effect from the start.

## Server settings

### Listener, TLS, diagnostics, and logs

| YAML key | Default | Description |
| --- | --- | --- |
| `host` | `""` | API listener address. Empty binds all IPv4 and IPv6 interfaces; use `127.0.0.1` or `localhost` for local-only service. |
| `port` | Template: `8317` | API listener port. |
| `tls.enable` | `false` | Enables direct HTTPS serving. |
| `tls.cert` | empty | Certificate path for direct TLS. |
| `tls.key` | empty | Private-key path for direct TLS; restrict it to the service account. |
| `debug` | `false` | Enables debug logging. Keep off in production. |
| `pprof.enable` | `false` | Starts the Go pprof HTTP server. |
| `pprof.addr` | `127.0.0.1:8316` | pprof listener. Keep it on loopback because it has no application authentication. |
| `logging-to-file` | `false` | Writes application logs to rotating files instead of stdout. |
| `logs-max-total-size-mb` | `0` | Total log-file limit in MB. A positive value removes oldest files; `0` disables cleanup. |
| `error-logs-max-files` | `10` | Maximum retained error logs when request logging is off. `0` disables cleanup. |
| `commercial-mode` | `false` | Disables high-overhead request logging and middleware features to lower per-request memory use. |
| `request-log` | unset / false | Enables detailed request logging. Enable only with an appropriate retention policy. |
| `usage-statistics-enabled` | `false` | Enables in-memory usage aggregation for Management API consumers. |
| `redis-usage-queue-retention-seconds` | `60`, max `3600` | In-memory retention of usage queue items; it does not configure an external Redis instance. |

### Upstream requests, retries, and streaming

| YAML key | Default | Description |
| --- | --- | --- |
| `proxy-url` | empty | Global outbound proxy. Supports `socks5`, `http`, and `https`. Per-entry `proxy-url` overrides it; `direct` or `none` explicitly bypasses global/environment proxies. |
| `request-retry` | `3` | Retries upstream `403`, `408`, `500`, `502`, `503`, and `504` responses. |
| `max-retry-credentials` | `0` | Number of other credentials attempted after failure. `0` or negative retains legacy behavior and tries all available credentials. |
| `max-retry-interval` | `30` seconds | Maximum wait before retrying a credential in cooldown. |
| `disable-cooling` | `false` | Globally disables quota cooldown scheduling. A compatible static credential can also disable cooling individually. |
| `save-cooldown-status` | `false` | Persists runtime cooldown state beside auth files. |
| `transient-error-cooldown-seconds` | `0` | Cooldown after temporary upstream errors. `0` uses legacy behavior; negative disables this cooldown. |
| `auth-auto-refresh-workers` | built-in default | OAuth refresh worker count. Values `<= 0` use the default. |
| `streaming.keepalive-seconds` | `0` | SSE heartbeat interval. Values `<= 0` disable heartbeats. |
| `streaming.bootstrap-retries` | `0` | Streaming retries before any response byte is sent. Values `<= 0` disable this. |
| `nonstream-keepalive-interval` | `0` | Blank-line keepalive interval for non-streaming responses. Values `<= 0` disable it. |
| `passthrough-headers` | `false` | Forwards filtered upstream response headers to clients. Enable only when the disclosure is intended. |

### Models, failover, and WebSockets

| YAML key | Default | Description |
| --- | --- | --- |
| `force-model-prefix` | `false` | Requires explicit prefixes to use prefixed credentials, except when prefix equals model name. |
| `disable-image-generation` | `false` | `false` enables image generation; `true` disables image tools and image endpoints; `chat` disables injection on non-image endpoints; `passthrough` preserves a client-sent tool list but does not inject one. |
| `gpt-image-2-base-model` | `gpt-5.4-mini` when empty/invalid | Legacy hosted image-generation base model. It must begin with `gpt-`. |
| `video-result-auth-cache-ttl` | `3h` | Duration to pin a video result ID to the credential that created it, e.g. `30m` or `3h`. |
| `quota-exceeded.switch-project` | `true` | Switches projects automatically when quota is exhausted. |
| `quota-exceeded.switch-preview-model` | `true` | Uses a preview-model fallback on quota exhaustion. |
| `quota-exceeded.antigravity-credits` | `true` | Uses a credit-backed Antigravity auth as last-resort fallback for Claude after free-tier auth is exhausted. |
| `routing.strategy` | `round-robin` | Credential strategy: `round-robin` or `fill-first`. |
| `routing.session-affinity` | `false` | Retains a session-to-credential binding where possible; unavailable credentials still fail over. |
| `routing.session-affinity-ttl` | `1h` | Session binding duration, e.g. `30m` or `2h30m`. |
| `ws-auth` | `true` | Requires API-key authentication on WebSocket routes. Disable only in a protected local/trusted environment. |
| `antigravity-signature-cache-enabled` | `true` when omitted | Enables normalized thinking-signature cache validation. |
| `antigravity-signature-bypass-strict` | unset | Strict bypass behavior for Antigravity signature handling. Leave unset unless needed by an integration. |
| `codex.identity-confuse` | `false` | Provider-wide Codex identity-confusion behavior. Use only when the upstream integration requires it. |

## Authentication

### Client API keys

`api-keys` authorizes clients to call the proxy. Every exposed deployment should
use long random keys and ideally assign one key per client or environment.

```yaml
api-keys:
  - "client-key-for-service-a"
  - "client-key-for-service-b"
```

Keep `ws-auth: true` unless a separate, isolated front end authenticates all
WebSocket traffic.

### Management API

All Management API calls require a management secret, including loopback calls.
Leaving `remote-management.secret-key` empty disables `/v0/management` and
returns `404` for those routes.

```yaml
remote-management:
  allow-remote: false
  secret-key: "replace-with-a-separate-long-random-management-key"
  disable-control-panel: true
  disable-auto-update-panel: true
  panel-github-repository: "https://github.com/router-for-me/Cli-Proxy-API-Management-Center"
```

| YAML key | Description |
| --- | --- |
| `remote-management.allow-remote` | Permits management access from non-loopback addresses. Default is false; use only behind TLS and administrative network controls. |
| `remote-management.secret-key` | Management secret. A plaintext value is bcrypt-hashed at startup; an existing bcrypt hash is also accepted. Empty disables management routes. |
| `remote-management.disable-control-panel` | Disables the bundled management UI route and panel download. Recommended for hardened/headless service. |
| `remote-management.disable-auto-update-panel` | Disables periodic GitHub panel updates. A missing panel can still be downloaded on first access unless the panel itself is disabled. |
| `remote-management.panel-github-repository` | Panel repository URL or releases API endpoint. Use the default trusted repository or disable the panel. |

`MANAGEMENT_PASSWORD` is an environment-only management credential. A non-empty
value is accepted by the Management API and permits remote management regardless
of `allow-remote`. Treat it as a privileged break-glass secret and never put it
in a broadly readable `.env` file.

### Direct TLS example

```yaml
host: "0.0.0.0"
port: 443
tls:
  enable: true
  cert: "/etc/cli-proxy-api/tls/fullchain.pem"
  key: "/etc/cli-proxy-api/tls/privkey.pem"

api-keys:
  - "replace-with-a-random-client-key"
remote-management:
  allow-remote: false
  secret-key: "replace-with-a-separate-management-key"
  disable-control-panel: true
```

## Provider credentials

Static provider keys live in YAML; OAuth-based credentials are stored in
`auth-dir` or the selected store mirror. Prefer model prefixes to isolate
provider accounts or tenants.

### Shared credential and model fields

The following fields recur across static provider entries:

| Field | Description |
| --- | --- |
| `api-key` | Provider secret. Keep YAML outside source control or use a protected remote config store. |
| `priority` | Higher preference when several credentials can serve the same request. |
| `prefix` | Optional model namespace, e.g. `team-a/gpt-4.1`. |
| `base-url` | Upstream endpoint override. Use trusted HTTPS URLs. |
| `proxy-url` | Entry-specific outbound proxy. It overrides the global proxy. |
| `headers` | Additional request-header map. Do not use it for version-controlled secrets. |
| `excluded-models` | Model IDs excluded for this credential. |
| `disable-cooling` | Disables cooldown for this credential; use sparingly. |
| `models` | Upstream-to-client mappings with `name`, `alias`, optional `display-name`, and optional `force-mapping`. |

`force-mapping: true` replaces the upstream response model ID with the client
alias. `display-name` affects model presentation, not routing.

### Gemini and Google Interactions

`gemini-api-key` and `interactions-api-key` share the same structure.

```yaml
gemini-api-key:
  - api-key: "replace-with-gemini-key"
    priority: 10
    prefix: "google"
    base-url: "https://generativelanguage.googleapis.com"
    models:
      - name: "gemini-2.5-pro"
        alias: "gemini-2.5-pro"
        display-name: "Gemini 2.5 Pro"
        force-mapping: true
    excluded-models: ["gemini-experimental"]
```

Available entry fields: `api-key`, `priority`, `prefix`, `base-url`,
`proxy-url`, `models`, `headers`, `excluded-models`, and `disable-cooling`.

### Codex and xAI

`codex-api-key` and `xai-api-key` share a structure. In addition to the common
fields, `websockets` enables static-key WebSocket use.

```yaml
codex-api-key:
  - api-key: "replace-with-codex-key"
    base-url: "https://api.openai.com"
    websockets: true
    models:
      - name: "gpt-5.4"
        alias: "gpt-5.4"
```

`codex-header-defaults` supplies OAuth/file-backed Codex request fallbacks only
when the client omitted the header:

```yaml
codex-header-defaults:
  user-agent: "my-trusted-client/1.0"
  beta-features: "feature-a,feature-b"
```

`user-agent` applies to HTTP and WebSocket traffic; `beta-features` applies to
WebSockets.

### Claude and cloaking

```yaml
claude-api-key:
  - api-key: "replace-with-claude-key"
    base-url: "https://api.anthropic.com"
    rebuild-mid-system-message: true
    cloak:
      mode: "never"
    models:
      - name: "claude-sonnet-4"
        alias: "claude-sonnet-4"
```

Claude entries support common fields and these additional keys:

| Field | Description |
| --- | --- |
| `rebuild-mid-system-message` | Moves Claude messages with role `system` to the top-level system field. |
| `cloak.mode` | `auto` (default), `always`, or `never`. Cloaking makes non-Claude-Code requests resemble Claude Code. |
| `cloak.strict-mode` | Removes user system messages instead of prepending the Claude Code prompt. |
| `cloak.sensitive-words` | Words to obfuscate with zero-width characters; compatibility only, not a security control. |
| `cloak.cache-user-id` | Caches Claude `user_id` values per API key. |
| `disable-claude-cloak-mode` | Globally defaults Claude credentials to `never`; a credential or OAuth record may override it. |
| `experimental-cch-signing` | Enables experimental Claude Code header-signing behavior for this credential. Leave disabled unless required and validated with the upstream. |

`claude-header-defaults` accepts `user-agent`, `package-version`,
`runtime-version`, `os`, `arch`, `timeout`, and `stabilize-device-profile`.

### OpenAI-compatible providers

```yaml
openai-compatibility:
  - name: "internal-gateway"
    prefix: "internal"
    base-url: "https://gateway.example.internal/v1"
    api-key-entries:
      - api-key: "replace-with-gateway-key"
        proxy-url: "direct"
    headers:
      X-Tenant: "production"
    models:
      - name: "upstream-model-id"
        alias: "internal-chat"
        image: false
        input-modalities: ["text"]
        output-modalities: ["text"]
```

Provider fields are `name`, `priority`, `disabled`, `prefix`, `base-url`,
`api-key-entries`, `models`, `headers`, and `disable-cooling`.
`api-key-entries` contains `api-key` and optional `proxy-url`. Model entries
accept `name`, `alias`, `display-name`, `force-mapping`, `image`,
`input-modalities`, `output-modalities`, and `thinking` (a model capability
override; leave unset unless required by the provider).

### Vertex-compatible keys

`vertex-api-key` supports Vertex-style paths with API-key authentication.

```yaml
vertex-api-key:
  - api-key: "replace-with-vertex-compatible-key"
    prefix: "vertex"
    base-url: "https://vertex-provider.example/v1"
    models:
      - name: "provider-model"
        alias: "vertex-chat"
```

Each entry supports `api-key`, `priority`, `prefix`, `base-url`, `proxy-url`,
`headers`, `models`, and `excluded-models`; mappings use `name`, `alias`,
`display-name`, and `force-mapping`.

### OAuth model filters and aliases

These affect OAuth/file-backed channels (`vertex`, `aistudio`, `antigravity`,
`claude`, `codex`, `kimi`, and `xai`), not static-key provider blocks.

```yaml
oauth-excluded-models:
  codex: ["gpt-experimental"]
oauth-model-alias:
  codex:
    - name: "gpt-5.4"
      alias: "gpt-production"
      fork: true
      force-mapping: true
```

`oauth-excluded-models` maps a channel to disallowed model IDs.
`oauth-model-alias` maps upstream `name` to client-visible `alias`; `fork: true`
keeps the original model and adds the alias, while `force-mapping` rewrites
response model names.

## Payload rules

`payload` changes translated JSON parameters using gjson/sjson paths. Keep rules
narrow and test each protocol because broad overrides can silently alter client
behavior.

```yaml
payload:
  default:
    - models:
        - name: "gemini-*"
          protocol: "gemini"
      params:
        "generationConfig.temperature": 0.2
  override:
    - models:
        - name: "gpt-*"
          protocol: "codex"
          headers:
            X-Client-Tier: "production-*"
          match:
            - "metadata.client": "trusted-app"
      params:
        "reasoning.effort": "high"
  filter:
    - models:
        - name: "*"
          protocol: "openai"
      params: ["metadata.internal_debug"]
```

| Key | Behavior |
| --- | --- |
| `payload.default` | Sets parameters only when a path is missing. |
| `payload.default-raw` | Same as `default`, but values must be valid raw JSON fragments. |
| `payload.override` | Always sets parameters, replacing client values. |
| `payload.override-raw` | Same as `override`, with raw JSON fragments. |
| `payload.filter` | Removes the listed JSON paths. |

Each rule uses `models` and `params`. A model selector supports wildcard `name`,
`protocol`, `from-protocol`, `headers` (all patterns match), `match`,
`not-match`, `exist`, and `not-exist`. `params` is a path/value map except for
`filter`, where it is a path list.

## Plugins

Plugins are trusted in-process native code, not sandboxed extensions. They are
disabled by default.

```yaml
plugins:
  enabled: false
  dir: "/var/lib/cli-proxy-api/plugins"
  store-sources:
    - "https://plugins.example.internal/registry.json"
  store-auth:
    - match: "https://plugins.example.internal/"
      apply-to: ["registry", "artifact"]
      type: "bearer"
      token-env: "CLIPROXY_PLUGIN_STORE_TOKEN"
  configs:
    sample-plugin:
      enabled: false
      priority: 10
      mode: "safe"
```

| YAML key | Description |
| --- | --- |
| `plugins.enabled` | Enables dynamic plugin discovery/loading. Keep false unless every plugin is trusted and reviewed. |
| `plugins.dir` | Plugin discovery directory. |
| `plugins.store-sources` | Additional registry URLs; the built-in official registry remains available. |
| `plugins.configs.<plugin-id>.enabled` | Enables one plugin. Missing values normalize to false. |
| `plugins.configs.<plugin-id>.priority` | Startup and routing order. |
| `plugins.configs.<plugin-id>.*` | Plugin-owned settings; the installed plugin defines their schema. |

`plugins.store-auth` takes `match`, `apply-to` (`registry`, `metadata`, or
`artifact`), `type` (`none`, `bearer`, `github-token`, `basic`, or `header`),
`token-env`, `username-env`, `password-env`, `header-name`, `header-value-env`,
and `allow-insecure`. Credentials always come from the named environment
variables. Avoid `allow-insecure`.

With a truthy `CLIPROXY_DISABLE_PLUGINS`, global and per-plugin enablement is
forced off and plugin-store/mutation operations are blocked. It is recommended
when plugins are not an intentional, controlled production dependency.

## Environment variables

Use uppercase names. Storage variables also accept lowercase compatibility names
(e.g. `pgstore_dsn`).

| Variable | Purpose |
| --- | --- |
| `MANAGEMENT_PASSWORD` | Environment-only management secret. A non-empty value permits remote management and is accepted in addition to the YAML management key. |
| `MANAGEMENT_STATIC_PATH` | Local override path for the management panel static asset. |
| `CLIPROXY_DISABLE_PLUGINS` | Truthy process policy disabling plugin loading, store access, and mutation paths. |
| `PGSTORE_DSN` | Enables Postgres config/auth storage. |
| `PGSTORE_SCHEMA` | Optional Postgres schema. |
| `PGSTORE_LOCAL_PATH` | Parent for the local `pgstore` mirror. |
| `GITSTORE_GIT_URL` | Enables Git-backed config/auth storage. |
| `GITSTORE_GIT_USERNAME` | Git remote username when required. |
| `GITSTORE_GIT_TOKEN` | Git credential/personal access token. |
| `GITSTORE_GIT_BRANCH` | Branch for the Git store. |
| `GITSTORE_LOCAL_PATH` | Local Git working-copy path. |
| `OBJECTSTORE_ENDPOINT` | Enables S3-compatible storage; accepts `https://host`, `http://host`, or no scheme. |
| `OBJECTSTORE_BUCKET` | Bucket containing config and auth data. |
| `OBJECTSTORE_ACCESS_KEY` | Object-store access key. |
| `OBJECTSTORE_SECRET_KEY` | Object-store secret key. |
| `OBJECTSTORE_LOCAL_PATH` | Parent for the local `objectstore` mirror. |
| `HOME_JWT` | Enables Home control-plane mode if `--home-jwt` was not supplied. |
| `DEPLOY` | Set to `cloud` to permit initially empty remote-store configuration. |

For a plugin-store auth rule, use the exact variable named by its `token-env`,
`username-env`, `password-env`, or `header-value-env` field. The server does not
read object-store region, prefix, or addressing mode from environment variables:
it uses path-style access and derives TLS from the endpoint scheme.

```dotenv
# Never commit this file.
PGSTORE_DSN=postgresql://cliproxy:replace-me@db.internal:5432/cliproxy?sslmode=require
PGSTORE_SCHEMA=cliproxy
PGSTORE_LOCAL_PATH=/var/lib/cli-proxy-api
# MANAGEMENT_PASSWORD=replace-with-a-long-random-management-secret
CLIPROXY_DISABLE_PLUGINS=true
```

## Storage backends

Remote stores mirror configuration and auth records locally. Place mirrors on a
persistent owner-only volume. Home mode takes precedence over all three stores.

### File storage (default)

Without a store-selection variable, the selected YAML file and its `auth-dir`
are used directly. This suits single-node deployments.

```yaml
auth-dir: "/var/lib/cli-proxy-api/auths"
```

```bash
install -d -m 700 /var/lib/cli-proxy-api/auths
chmod 600 config.yaml
```

### Postgres

`PGSTORE_DSN` has highest storage priority. It stores configuration and auth
metadata in the selected schema and maintains a local `pgstore` mirror below
`PGSTORE_LOCAL_PATH` (or the writable application path).

```dotenv
PGSTORE_DSN=postgresql://cliproxy:replace-me@postgres.internal:5432/cliproxy?sslmode=require
PGSTORE_SCHEMA=cliproxy
PGSTORE_LOCAL_PATH=/var/lib/cli-proxy-api
```

Use a database role limited to its database/schema and require TLS for non-local
connections.

### Git

`GITSTORE_GIT_URL` selects Git when Postgres is not set. The repository contains
configuration and authentication records, so repository read access is
production-secret access.

```dotenv
GITSTORE_GIT_URL=https://github.com/your-org/cli-proxy-config.git
GITSTORE_GIT_USERNAME=cliproxy-bot
GITSTORE_GIT_TOKEN=replace-with-a-narrowly-scoped-token
GITSTORE_GIT_BRANCH=main
GITSTORE_LOCAL_PATH=/var/lib/cli-proxy-api/gitstore
```

Use a private repository, protected branches, secret scanning, and a deploy
token scoped only to this repository.

### S3-compatible object storage

`OBJECTSTORE_ENDPOINT` selects object storage when Postgres is not configured.
The server uses path-style S3 access, stores YAML at `config/config.yaml`, and
stores auth records under `auths/` in the selected bucket.

```dotenv
OBJECTSTORE_ENDPOINT=https://s3.internal.example
OBJECTSTORE_BUCKET=cliproxy-production
OBJECTSTORE_ACCESS_KEY=replace-with-access-key
OBJECTSTORE_SECRET_KEY=replace-with-secret-key
OBJECTSTORE_LOCAL_PATH=/var/lib/cli-proxy-api
```

All four endpoint/bucket/access/secret values are required. Use HTTPS, a
separate bucket, least-privilege credentials, encryption at rest, and a
protected local mirror.

### Home control plane

Pass `--home-jwt <jwt>` or define `HOME_JWT`. The server fetches configuration
from Home and bypasses local Postgres, Git, and object-store initialization.
`--home-disable-cluster-discovery` retains the configured Home target instead of
discovered cluster nodes. Treat the JWT as a cluster credential.

## OAuth authentication files

OAuth credentials reside under `auth-dir` for file storage or in the selected
store's local mirror. Use the provider login flow to create or refresh them:

```bash
./cli-proxy-api --config ./config.yaml --codex-login
./cli-proxy-api --config ./config.yaml --codex-device-login --no-browser
./cli-proxy-api --config ./config.yaml --claude-login
./cli-proxy-api --config ./config.yaml --antigravity-login
./cli-proxy-api --config ./config.yaml --kimi-login
./cli-proxy-api --config ./config.yaml --xai-login
```

Use `--oauth-callback-port <port>` to force a callback port. Import a Vertex
service account with `--vertex-import <service-account-json>` and optionally
`--vertex-import-prefix <prefix>`. Do not add auth records to source control,
container images, or logs; revoke and rotate them when access changes.

## Security best practices

1. **Bind narrowly.** Prefer `127.0.0.1` behind a reverse proxy. If listening
   publicly, use TLS plus firewall/security-group restrictions.
2. **Use independent secrets.** Separate client keys, management keys, provider
   keys, Git tokens, and storage credentials so each can be rotated safely.
3. **Disable management by default.** Leave `secret-key` empty when management
   is unnecessary. Otherwise keep `allow-remote: false`, use a separate key,
   and disable the panel where practical.
4. **Protect volumes.** Use `0600` for configuration/private keys and `0700` for
   parent/auth directories. Run as a dedicated non-root account.
5. **Keep diagnostic endpoints private.** Do not expose pprof. Enable debug or
   detailed request logs only temporarily because logs may contain sensitive
   metadata.
6. **Inject secrets safely.** Use a secret manager or protected environment;
   never commit real `.env`, configuration, OAuth, or TLS-private-key files.
7. **Harden remote stores.** Require database TLS, private Git repositories and
   narrow tokens, plus HTTPS/encryption/least privilege for object stores.
8. **Treat plugins as production code.** Keep plugins off unless reviewed and
   version-pinned; set `CLIPROXY_DISABLE_PLUGINS=true` when they are not needed.
9. **Minimize disclosure.** Leave `passthrough-headers` disabled and avoid
   putting secrets in custom headers or payload rules.
10. **Audit and rotate.** Use per-consumer API keys, review management/store
    access, revoke unused OAuth records, and monitor failures without logging
    tokens.

## Validation checklist

```bash
# Verify the binary can start with the intended configuration.
./cli-proxy-api --config ./config.yaml

# Required compile verification after repository changes.
go build -o test-output ./cmd/server && rm test-output
```

- Confirm listener exposure, firewall rules, and TLS match the intended access.
- Confirm every external consumer has a unique entry in `api-keys`.
- Confirm the Management API is disabled or protected by a separate secret.
- Confirm config, auth files, TLS files, and local mirrors are owner-only.
- Confirm only the intended storage-selection backend is configured.
- Test enabled providers, model aliases, prefixes, and OAuth auth records.
- Verify `CLIPROXY_DISABLE_PLUGINS=true` when native plugins are not required.
