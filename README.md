# CLIProxyAPI Plugin Lockdown

English | [中文](README_CN.md) | [日本語](README_JA.md)

An **unofficial security-hardening fork** of CLIProxyAPI. It adds a process-level plugin lockdown policy for deployments that do not need plugins and reduces product fingerprinting on the unauthenticated root path.

This fork retains the upstream OpenAI, Gemini, Claude, Codex, and other compatible APIs, OAuth flows, multi-account operation, and round-robin load balancing. This document does not endorse specific models, subscriptions, or third-party relay services.

> [!IMPORTANT]
> This is not an official CLIProxyAPI release and does not represent the upstream maintainers. When reporting a problem, identify this fork and include its baseline and commit information.

## Contents

- [Version and provenance](#version-and-provenance)
- [Security goals](#security-goals)
- [Plugin lockdown policy](#plugin-lockdown-policy)
- [API behavior](#api-behavior)
- [Root page and security headers](#root-page-and-security-headers)
- [Security boundaries](#security-boundaries)
- [Build from source](#build-from-source)
- [Run locally](#run-locally)
- [Local Docker build and run](#local-docker-build-and-run)
- [Compose risks](#compose-risks)
- [Health and policy verification](#health-and-policy-verification)
- [Configuration validation temporary files](#configuration-validation-temporary-files)
- [Upstream synchronization](#upstream-synchronization)
- [Documentation and development](#documentation-and-development)
- [License and attribution](#license-and-attribution)

## Version and provenance

| Item | Value |
| --- | --- |
| Upstream project | [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) |
| Upstream baseline | `v7.2.80` |
| Baseline commit | `09da52ad509e2c18e7b9540db3b98c2214c280aa` |
| Plugin lockdown commit | `c8b16d049a9d5e546ebc2e6267de8970c82aeca3` |
| Root disguise and temp-file commit | `c20f6e4b5a2b012635464077c50f2eb82ce0e0e8` |
| License | MIT |

The two fork commits have distinct responsibilities:

- `c8b16d04` adds the `CLIPROXY_DISABLE_PLUGINS` process policy, normalizes plugin configuration in the standard CLI startup path, and restricts plugin-store and plugin mutation endpoints.
- `c20f6e4b` changes `GET /` to generic Welcome HTML, adds security headers, and moves Management API configuration-validation files to the system temporary directory.

## Security goals

This fork is intended for deployments where:

- Only the core proxy, OAuth, compatible APIs, and multi-account scheduling are required.
- Operations policy requires plugins to be forced off from process startup.
- Administrators still need to inspect existing plugin state, read configuration, and remove legacy plugins.
- Unauthenticated requests to the root path should not receive the product name and an API route list.

It provides **defense in depth and misconfiguration resistance**. It is not a complete sandbox or a formal proof covering every extension path.

## Plugin lockdown policy

### Inject the variable before process startup

A secure startup requires the real process environment to contain the following value before the program executes:

```bash
export CLIPROXY_DISABLE_PLUGINS=true
./cli-proxy-api --config ./config.yaml
```

The value may instead be injected by the process manager:

- systemd `Environment=` or `EnvironmentFile=`.
- Docker `-e`, `--env-file`, or Compose `environment`.
- Kubernetes, Nomad, or another orchestrator's container environment.
- CI/CD, a launcher, or a host service manager that exports it before `exec`.

> [!WARNING]
> **Do not treat placing `CLIPROXY_DISABLE_PLUGINS=true` only in the working-directory `.env` file as a secure startup method.**
>
> The standard server entry reads configuration and performs plugin bootstrap, including plugin CLI flag registration, before it loads the working-directory `.env`. A value supplied only through that file arrives too late to guarantee that the bootstrap phase was protected by the lockdown policy.

If an environment file is required, load it before starting the executable through the shell, systemd, Docker, or the orchestrator. For example:

```bash
set -a
. /secure/path/cliproxy.env
set +a
exec ./cli-proxy-api --config ./config.yaml
```

### Behavior in the standard CLI path

When `CLIPROXY_DISABLE_PLUGINS` in the real process environment parses as true through Go's `strconv.ParseBool`:

- `plugins.enabled` is forced to `false` during configuration normalization.
- Every `plugins.configs.<id>.enabled` value is forced to `false`.
- The `enabled` value in each raw plugin configuration node is also changed to `false`.
- The plugin capability support flag reports `0`.
- Plugin-store access, installation, and plugin configuration mutations are rejected by policy.
- Attempts to re-enable global or per-plugin settings through the complete YAML configuration endpoint are rejected.

Use the explicit value:

```text
CLIPROXY_DISABLE_PLUGINS=true
```

The policy is not enabled when the variable is absent, empty, or cannot be parsed as a true Boolean value.

### Why some plugin endpoints remain

The lockdown does not remove all plugin code or management routes. It preserves these read-only or cleanup capabilities:

- List discovered or registered plugins.
- Read an individual plugin configuration.
- Read the complete structured configuration or raw `config.yaml`.
- Delete an existing plugin for post-audit cleanup and retirement.

This prevents adding, installing, enabling, or changing plugins while retaining inventory and cleanup paths.

## API behavior

The Management API prefix is `/v0/management`. Remote availability remains controlled by the upstream management configuration and management key.

### Uniform 403 responses under lockdown

The following operations return HTTP `403 Forbidden` while the policy is active:

| Method | Path | Behavior |
| --- | --- | --- |
| `GET` | `/v0/management/plugin-store` | Blocks plugin-store access |
| `POST` | `/v0/management/plugin-store/:id/install` | Blocks plugin installation |
| `PATCH` | `/v0/management/plugins/:id/enabled` | Blocks plugin enable-state changes |
| `PUT` | `/v0/management/plugins/:id/config` | Blocks plugin configuration replacement |
| `PATCH` | `/v0/management/plugins/:id/config` | Blocks plugin configuration mutation |
| `PUT` | `/v0/management/config.yaml` | Rejects YAML that enables plugins globally or individually |

The uniform JSON response is:

```json
{
  "error": "plugin_capability_disabled",
  "message": "plugin capability is disabled by server policy"
}
```

### Retained for audit and cleanup

The fork's plugin-lockdown handlers do not disable these management operations:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/v0/management/plugins` | Plugin inventory |
| `GET` | `/v0/management/plugins/:id/config` | Configuration audit |
| `DELETE` | `/v0/management/plugins/:id` | Removal and cleanup |
| `GET` | `/v0/management/config` | Read structured configuration |
| `GET` | `/v0/management/config.yaml` | Read raw configuration |

Normal management authentication and access controls still apply to these endpoints.

## Root page and security headers

Unauthenticated `GET /` returns minimal HTML:

```html
<h1>Welcome</h1>
```

It no longer returns the product name and example API paths from the upstream root JSON. The response includes:

- `Cache-Control: no-store`
- `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'`
- `Referrer-Policy: no-referrer`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`

The root handler also clears common CORS headers on its own response. The change does not generally disable the core `/v1`, `/healthz`, or Management APIs.

## Security boundaries

Interpret this fork within the following limits:

- Plugin code is **not removed from the binary**.
- Lockdown is **not enabled by default**; the environment variable must be explicitly supplied.
- Not every management endpoint is closed; audit, read, and delete paths remain available.
- A generic root page is not complete de-fingerprinting. Ports, TLS, traffic characteristics, other routes, and error responses may still identify the implementation.
- This project does not claim that every embedded SDK use, custom entry point, modified build, or malicious-local-host scenario is impossible to bypass.
- An actor able to modify the process environment, executable, startup arguments, configuration mounts, or container definition is part of the trusted deployment control plane.
- Management APIs, configuration files, authentication directories, and the container runtime still require least privilege, network isolation, and proper secret management.

## Build from source

Requirements:

- Go `1.26` or later.
- Git.
- The current fork source, not an unrelated image with a similar name.

```bash
git clone https://github.com/86669666/CLIProxyAPI-plugin-lockdown.git
cd CLIProxyAPI-plugin-lockdown
git rev-parse --short HEAD
go build -o cli-proxy-api ./cmd/server
```

After changing Go code, the repository requires:

```bash
gofmt -w .
go build -o test-output ./cmd/server && rm test-output
go test ./...
```

README-only changes do not require reformatting Go files.

## Run locally

Create a local configuration from the template, then follow the upstream documentation for API keys, OAuth credentials, the authentication directory, and management access:

```bash
cp config.example.yaml config.yaml
```

Start with lockdown enabled:

```bash
export CLIPROXY_DISABLE_PLUGINS=true
./cli-proxy-api --config ./config.yaml
```

Common upstream flags include:

- `--config <path>`: select the configuration file.
- `--tui`: start the terminal UI.
- `--standalone`: run the TUI in standalone mode.
- `--local-model`: use only embedded model catalogs and skip remote model catalog updates.
- `--no-browser`: do not automatically open a browser during OAuth.
- `--oauth-callback-port <port>`: select the OAuth callback port.

Do not paste real API keys, management keys, or OAuth tokens into commands committed to source control, documentation, tickets, or logs.

## Local Docker build and run

Build an image explicitly from this fork to ensure the local source is used:

```bash
docker build \
  --build-arg VERSION=v7.2.80-lockdown \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t cli-proxy-api-lockdown:local .
```

Example run command:

```bash
docker run --rm \
  --name cli-proxy-api-lockdown \
  -e CLIPROXY_DISABLE_PLUGINS=true \
  -p 8317:8317 \
  -v "$(pwd)/config.yaml:/CLIProxyAPI/config.yaml:ro" \
  -v "$(pwd)/auths:/root/.cli-proxy-api" \
  cli-proxy-api-lockdown:local
```

If the Management API must write configuration, do not mount `config.yaml` read-only. Use a protected writable persistent volume and restrict host permissions instead.

## Compose risks

The current `docker-compose.yml` and `docker-compose.cluster.yml` contain this default:

```yaml
image: ${CLI_PROXY_IMAGE:-eceasy/cli-proxy-api:latest}
pull_policy: always
```

Running a normal `docker compose up` can therefore pull and run an external `latest` image instead of an image built from this fork's local source.

The existing Compose files also do not pass `CLIPROXY_DISABLE_PLUGINS=true` by default. Their default startup must not be treated as a locked-down deployment.

A safer use must do all of the following:

1. Use a locally built tag or a verified fork image digest.
2. Remove or override `pull_policy: always` so an external image cannot replace the local image.
3. Explicitly inject `CLIPROXY_DISABLE_PLUGINS=true` when the container is created.
4. Inspect the expanded configuration with `docker compose config`.
5. Run health and policy checks after startup.

You can use a per-deployment override without modifying the repository's Compose files:

```yaml
services:
  cli-proxy-api:
    image: cli-proxy-api-lockdown:local
    pull_policy: never
    environment:
      CLIPROXY_DISABLE_PLUGINS: "true"
```

```bash
docker compose -f docker-compose.yml -f compose.lockdown.yml config
docker compose -f docker-compose.yml -f compose.lockdown.yml up -d
```

## Health and policy verification

### Health check

```bash
curl --fail --silent --show-error http://127.0.0.1:8317/healthz
```

Expected response:

```json
{"status":"ok"}
```

### Root page check

```bash
curl --include http://127.0.0.1:8317/
```

Confirm status `200`, a `text/html` content type, only generic Welcome content, and the security headers listed earlier.

### Plugin policy check

Provide the management key safely in the local shell:

```bash
export MANAGEMENT_KEY='<set-locally>'
```

Then request a restricted endpoint:

```bash
curl --silent --show-error \
  -H "Authorization: Bearer ${MANAGEMENT_KEY}" \
  http://127.0.0.1:8317/v0/management/plugin-store
```

The expected HTTP status is `403`, with `plugin_capability_disabled` in the `error` field.

Check the retained inventory endpoint:

```bash
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${MANAGEMENT_KEY}" \
  http://127.0.0.1:8317/v0/management/plugins
```

Proxy API verification example:

```bash
export API_KEY='<set-locally>'
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${API_KEY}" \
  http://127.0.0.1:8317/v1/models
```

Every `Authorization` example uses only `${MANAGEMENT_KEY}` or `${API_KEY}`. Do not replace them with values that will be committed to the repository.

## Configuration validation temporary files

When the Management API receives `PUT /v0/management/config.yaml`, it first writes a temporary file and invokes the configuration loader to validate the content.

This fork uses:

```text
os.CreateTemp("", "cliproxyapi-config-validate-*.yaml")
```

The empty directory argument makes Go use the system temporary directory instead of creating the validation file next to the real `config.yaml`. The handler still attempts to remove the temporary file after success or failure.

This change reduces short-lived validation copies in the configuration directory. It does not replace correct system temporary-directory permissions, disk encryption, or host isolation.

## Configuration file permissions

Configuration updates through `PUT /v0/management/config.yaml` store the configuration file with owner-only permissions (`0600`). The parent directory is created or corrected to owner-only permissions (`0700`), and these permissions are also repaired when updating an existing file. Keep the configuration path on a protected writable volume when using the Management API.

## Upstream synchronization

The documented baseline for this fork is `v7.2.80` / `09da52ad`. Re-audit the security assumptions whenever upstream changes are merged or rebased.

Review at least:

- Plugin bootstrap and `.env` load order in `cmd/server/main.go`.
- Plugin configuration parsing and normalization in `internal/config`.
- Plugin loading, capability reporting, and dynamic route registration in `internal/pluginhost`.
- Plugin-store, enable, configuration, delete, and full-YAML write endpoints under `/v0/management`.
- `GET /`, CORS middleware, and security headers.
- Compose image defaults, pull policy, and environment propagation.
- New entry points, embedded SDK patterns, or code that bypasses the standard CLI startup path.

After synchronization, run at least:

```bash
gofmt -w .
go test ./...
go build -o test-output ./cmd/server && rm test-output
git diff --check
```

Do not assume the policy still covers new upstream execution paths merely because the patches apply cleanly.

## Documentation and development

- Configuration template: [`config.example.yaml`](config.example.yaml)
- SDK usage: [`docs/sdk-usage.md`](docs/sdk-usage.md)
- Advanced SDK usage: [`docs/sdk-advanced.md`](docs/sdk-advanced.md)
- SDK access control: [`docs/sdk-access.md`](docs/sdk-access.md)
- Credential loading and updates: [`docs/sdk-watcher.md`](docs/sdk-watcher.md)
- Custom provider example: [`examples/custom-provider`](examples/custom-provider)

Keep security patches small and verifiable, never log keys or tokens, and add focused tests for policy boundaries.

## License and attribution

This repository remains licensed under the [MIT License](LICENSE).

The original design, main implementation, and upstream maintenance of CLIProxyAPI belong to [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) and its contributors. This fork maintains only the unofficial security changes described here on top of the `v7.2.80` baseline.

Retain the MIT license text and existing copyright notices when using, modifying, or redistributing the software.
