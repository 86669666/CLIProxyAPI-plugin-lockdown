# CLIProxyAPI Plugin Lockdown

[English](README.md) | 中文 | [日本語](README_JA.md)

一个基于 CLIProxyAPI 的**非官方安全加固 fork**，重点为不需要插件能力的部署提供进程级插件锁定策略，并降低未认证根路径暴露的产品指纹。

本 fork 保留上游的 OpenAI、Gemini、Claude、Codex 等兼容 API、OAuth、多账户与轮询负载均衡能力。本文不对具体模型、订阅服务或第三方中转服务作推荐。

> [!IMPORTANT]
> 这不是 CLIProxyAPI 官方发行版，也不代表上游项目维护者。问题排查时请先说明正在使用本 fork，并附上基线和提交信息。

## 目录

- [版本与来源](#版本与来源)
- [安全目标](#安全目标)
- [插件锁定策略](#插件锁定策略)
- [接口行为](#接口行为)
- [根路径与安全响应头](#根路径与安全响应头)
- [安全边界](#安全边界)
- [源码构建](#源码构建)
- [本地运行](#本地运行)
- [Docker 本地构建与运行](#docker-本地构建与运行)
- [Compose 风险](#compose-风险)
- [健康与策略验证](#健康与策略验证)
- [配置验证临时文件](#配置验证临时文件)
- [上游同步](#上游同步)
- [文档与开发](#文档与开发)
- [许可证与归属](#许可证与归属)

## 版本与来源

| 项目 | 值 |
| --- | --- |
| 上游项目 | [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) |
| 上游基线 | `v7.2.80` |
| 基线提交 | `09da52ad509e2c18e7b9540db3b98c2214c280aa` |
| 插件锁定提交 | `c8b16d049a9d5e546ebc2e6267de8970c82aeca3` |
| 根路径伪装与临时文件提交 | `c20f6e4b5a2b012635464077c50f2eb82ce0e0e8` |
| 许可证 | MIT |

两项 fork 提交的职责如下：

- `c8b16d04`：增加 `CLIPROXY_DISABLE_PLUGINS` 进程策略，标准 CLI 启动路径中规范化插件配置，并限制插件商店和插件变更接口。
- `c20f6e4b`：将 `GET /` 改为通用 Welcome HTML，增加安全响应头，并让管理端配置验证临时文件使用系统临时目录。

## 安全目标

本 fork 适合以下场景：

- 部署只需要核心代理、OAuth、兼容 API 和多账户调度能力。
- 运维策略要求插件在进程启动时即被强制关闭。
- 管理端仍需查看现存插件状态、读取配置并删除遗留插件。
- 未认证访问根路径时，不希望直接返回产品名称和 API 路由清单。

它提供的是**纵深防御和误配置防护**，不是完整沙箱，也不是对所有扩展路径的形式化安全证明。

## 插件锁定策略

### 必须在进程启动前注入

安全启动必须让真实进程环境在程序执行前包含：

```bash
export CLIPROXY_DISABLE_PLUGINS=true
./cli-proxy-api --config ./config.yaml
```

也可以由进程管理器直接注入：

- systemd 的 `Environment=` 或 `EnvironmentFile=`。
- Docker 的 `-e`、`--env-file` 或 Compose `environment`。
- Kubernetes、Nomad 或其他编排器的容器环境变量。
- CI/CD、启动脚本或主机服务管理器在 `exec` 前导出的环境。

> [!WARNING]
> **不要把“仅在工作目录 `.env` 中写入 `CLIPROXY_DISABLE_PLUGINS=true`”当作安全启动方式。**
>
> 标准服务器入口会先读取配置并执行插件 bootstrap、注册插件命令行标志，然后才加载工作目录 `.env`。因此 `.env` 中的该变量到达得太晚，不能保证 bootstrap 阶段已经受锁定策略保护。

如果希望使用环境文件，请在启动程序之前由 shell、systemd、Docker 或编排器加载它。例如：

```bash
set -a
. /secure/path/cliproxy.env
set +a
exec ./cli-proxy-api --config ./config.yaml
```

### 标准 CLI 路径中的行为

当真实进程环境中的 `CLIPROXY_DISABLE_PLUGINS` 可由 Go `strconv.ParseBool` 解析为真值时：

- `plugins.enabled` 在配置规范化阶段被强制设为 `false`。
- 每个 `plugins.configs.<id>.enabled` 被强制设为 `false`。
- 插件原始配置节点中的 `enabled` 同步改为 `false`。
- 插件能力支持标志返回 `0`。
- 插件商店访问、安装和插件配置变更请求被策略拒绝。
- 通过完整 YAML 配置接口重新启用全局插件或单个插件会被拒绝。

建议只使用明确值：

```text
CLIPROXY_DISABLE_PLUGINS=true
```

变量缺失、为空或无法解析为布尔真值时，不会启用锁定策略。

### 为什么保留部分插件接口

锁定策略没有删除所有插件相关代码或管理路由。以下只读或清理能力仍保留：

- 列出已发现或已注册插件。
- 读取单个插件配置。
- 读取完整服务器配置或原始 `config.yaml`。
- 删除现存插件，用于审计后的清理和退役。

这样可以在禁止新增、安装、启用和修改插件的同时，保留盘点与清理路径。

## 接口行为

管理 API 的前缀为 `/v0/management`。是否可远程访问仍由上游管理配置和管理密钥控制。

### 策略启用时返回统一 403

以下操作在锁定策略下返回 HTTP `403 Forbidden`：

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| `GET` | `/v0/management/plugin-store` | 禁止访问插件商店 |
| `POST` | `/v0/management/plugin-store/:id/install` | 禁止安装插件 |
| `PATCH` | `/v0/management/plugins/:id/enabled` | 禁止切换插件启用状态 |
| `PUT` | `/v0/management/plugins/:id/config` | 禁止替换插件配置 |
| `PATCH` | `/v0/management/plugins/:id/config` | 禁止修改插件配置 |
| `PUT` | `/v0/management/config.yaml` | 当 YAML 请求启用全局或任一插件时拒绝 |

统一 JSON 响应为：

```json
{
  "error": "plugin_capability_disabled",
  "message": "plugin capability is disabled by server policy"
}
```

### 为审计与清理保留

以下管理操作没有被本 fork 的插件锁定处理器关闭：

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/v0/management/plugins` | 插件盘点 |
| `GET` | `/v0/management/plugins/:id/config` | 配置审计 |
| `DELETE` | `/v0/management/plugins/:id` | 删除和清理 |
| `GET` | `/v0/management/config` | 读取结构化配置 |
| `GET` | `/v0/management/config.yaml` | 读取原始配置 |

这些接口仍要求正常的管理认证与访问控制。

## 根路径与安全响应头

未认证的 `GET /` 返回精简的 HTML 页面：

```html
<h1>Welcome</h1>
```

它不再返回原上游根 JSON 中的产品名称和示例 API 路径。响应包含：

- `Cache-Control: no-store`
- `Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'; base-uri 'none'`
- `Referrer-Policy: no-referrer`
- `X-Content-Type-Options: nosniff`
- `X-Frame-Options: DENY`

根处理器同时清除其响应上的常见 CORS 头。核心 `/v1`、`/healthz` 和管理 API 行为不因该提交而整体关闭。

## 安全边界

请按以下边界理解本 fork：

- 插件代码**没有从二进制中删除**。
- 锁定策略**不是默认开启**；必须显式提供环境变量。
- 不是所有管理接口都被关闭；审计、读取和删除路径仍保留。
- 根页面通用化不是“完全去指纹”；端口、TLS、流量特征、其他路由和错误响应仍可能暴露实现信息。
- 本项目不声称在所有嵌入式 SDK、自定义入口、被修改构建或恶意本地主机条件下不可绕过。
- 能修改进程环境、二进制、启动参数、配置挂载或容器定义的主体，本质上属于受信任的部署控制面。
- 管理 API、配置文件、认证目录和容器运行时仍需最小权限、网络隔离和密钥管理。

## 源码构建

要求：

- Go `1.26` 或更高版本。
- Git。
- 使用当前仓库源码，而不是同名外部镜像。

```bash
git clone https://github.com/86669666/CLIProxyAPI-plugin-lockdown.git
cd CLIProxyAPI-plugin-lockdown
git rev-parse --short HEAD
go build -o cli-proxy-api ./cmd/server
```

修改 Go 代码后，仓库要求执行：

```bash
gofmt -w .
go build -o test-output ./cmd/server && rm test-output
go test ./...
```

本 README 修改不需要重新格式化 Go 文件。

## 本地运行

先从模板创建本地配置，并按上游文档配置 API 密钥、OAuth 凭据、认证目录和管理访问：

```bash
cp config.example.yaml config.yaml
```

以锁定策略启动：

```bash
export CLIPROXY_DISABLE_PLUGINS=true
./cli-proxy-api --config ./config.yaml
```

常用上游参数包括：

- `--config <path>`：指定配置文件。
- `--tui`：启动终端界面。
- `--standalone`：以独立模式运行 TUI。
- `--local-model`：仅使用内置模型目录，不拉取远程模型目录。
- `--no-browser`：OAuth 流程不自动打开浏览器。
- `--oauth-callback-port <port>`：指定 OAuth 回调端口。

不要在命令行、README、工单或日志中粘贴真实 API 密钥、管理密钥或 OAuth 令牌。

## Docker 本地构建与运行

为确保使用本 fork 的代码，应明确本地构建镜像：

```bash
docker build \
  --build-arg VERSION=v7.2.80-lockdown \
  --build-arg COMMIT="$(git rev-parse --short HEAD)" \
  --build-arg BUILD_DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -t cli-proxy-api-lockdown:local .
```

运行示例：

```bash
docker run --rm \
  --name cli-proxy-api-lockdown \
  -e CLIPROXY_DISABLE_PLUGINS=true \
  -p 8317:8317 \
  -v "$(pwd)/config.yaml:/CLIProxyAPI/config.yaml:ro" \
  -v "$(pwd)/auths:/root/.cli-proxy-api" \
  cli-proxy-api-lockdown:local
```

如需通过管理 API 写入配置，不要以只读方式挂载 `config.yaml`；应改用受保护的可写持久卷，并限制宿主机权限。

## Compose 风险

仓库当前 `docker-compose.yml` 和 `docker-compose.cluster.yml` 默认包含：

```yaml
image: ${CLI_PROXY_IMAGE:-eceasy/cli-proxy-api:latest}
pull_policy: always
```

这意味着直接执行普通 `docker compose up` 可能拉取并运行外部 `latest` 镜像，而不是当前 fork 的本地源码构建结果。

此外，现有 Compose 文件默认没有向容器传入 `CLIPROXY_DISABLE_PLUGINS=true`。因此不能把默认 Compose 启动视为已锁定部署。

安全使用时应同时做到：

1. 使用本地构建标签或固定到经验证的 fork 镜像摘要。
2. 删除或覆盖 `pull_policy: always`，避免本地镜像被外部镜像替换。
3. 在容器创建时显式注入 `CLIPROXY_DISABLE_PLUGINS=true`。
4. 用 `docker compose config` 检查最终展开结果。
5. 启动后执行健康检查和策略检查。

可使用只针对本次启动的覆盖文件，不必修改仓库 Compose 文件：

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

## 健康与策略验证

### 健康检查

```bash
curl --fail --silent --show-error http://127.0.0.1:8317/healthz
```

预期响应：

```json
{"status":"ok"}
```

### 根页面检查

```bash
curl --include http://127.0.0.1:8317/
```

确认状态为 `200`、内容类型为 `text/html`、页面只显示通用 Welcome 内容，并存在前述安全响应头。

### 插件策略检查

先在 shell 中安全提供管理密钥：

```bash
export MANAGEMENT_KEY='<set-locally>'
```

然后请求受限接口：

```bash
curl --silent --show-error \
  -H "Authorization: Bearer ${MANAGEMENT_KEY}" \
  http://127.0.0.1:8317/v0/management/plugin-store
```

预期 HTTP 状态为 `403`，响应中的 `error` 为 `plugin_capability_disabled`。

检查保留的盘点接口：

```bash
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${MANAGEMENT_KEY}" \
  http://127.0.0.1:8317/v0/management/plugins
```

代理 API 验证示例：

```bash
export API_KEY='<set-locally>'
curl --fail --silent --show-error \
  -H "Authorization: Bearer ${API_KEY}" \
  http://127.0.0.1:8317/v1/models
```

所有 `Authorization` 示例只使用 `${MANAGEMENT_KEY}` 或 `${API_KEY}`，不要替换为需要提交到版本库的真实值。

## 配置验证临时文件

管理端接收 `PUT /v0/management/config.yaml` 时，会先写入临时文件并调用配置加载逻辑验证内容。

本 fork 使用：

```text
os.CreateTemp("", "cliproxyapi-config-validate-*.yaml")
```

空目录参数让 Go 使用系统临时目录，而不是在实际 `config.yaml` 旁创建验证文件。成功或失败后仍会尝试删除该临时文件。

这项调整降低了配置目录中出现短生命周期验证副本的概率，但不替代操作系统临时目录权限、磁盘加密和主机隔离。

## 配置文件权限

通过 `PUT /v0/management/config.yaml` 更新配置时，配置文件会使用仅所有者可读写的 `0600` 权限。父目录会创建或修正为仅所有者可访问的 `0700` 权限；更新已有文件时也会重新修正这些权限。使用管理 API 时，请将配置路径放在受保护且可写的持久卷中。

## 上游同步

本 fork 固定说明的基线是 `v7.2.80` / `09da52ad`。后续合并或变基上游时，需要重新审查安全假设。

重点检查：

- `cmd/server/main.go` 中插件 bootstrap 与 `.env` 加载顺序。
- `internal/config` 中插件配置解析和规范化路径。
- `internal/pluginhost` 中插件加载、能力声明和动态路由注册。
- `/v0/management` 的插件商店、启用、配置、删除和完整 YAML 写入接口。
- `GET /`、CORS 中间件和安全响应头。
- Compose 默认镜像、拉取策略和环境变量传递。
- 新增入口、SDK 嵌入方式或绕过标准 CLI 启动流程的代码。

同步后至少运行：

```bash
gofmt -w .
go test ./...
go build -o test-output ./cmd/server && rm test-output
git diff --check
```

不要仅因补丁能够干净应用，就假设锁定策略仍覆盖新的上游执行路径。

## 文档与开发

- 配置模板：[`config.example.yaml`](config.example.yaml)
- SDK 使用：[`docs/sdk-usage_CN.md`](docs/sdk-usage_CN.md)
- SDK 高级用法：[`docs/sdk-advanced_CN.md`](docs/sdk-advanced_CN.md)
- SDK 认证：[`docs/sdk-access_CN.md`](docs/sdk-access_CN.md)
- 凭据加载与更新：[`docs/sdk-watcher_CN.md`](docs/sdk-watcher_CN.md)
- 自定义 Provider 示例：[`examples/custom-provider`](examples/custom-provider)

贡献安全改动时，请保持补丁小而可验证，不要记录密钥或令牌，并为策略边界添加针对性测试。

## 许可证与归属

本仓库继续使用 [MIT License](LICENSE)。

CLIProxyAPI 的原始设计、主体实现和上游维护归属于 [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) 及其贡献者。本 fork 仅在 `v7.2.80` 基线上维护本文所述的非官方安全调整。

使用、修改或再分发时，请保留 MIT 许可证文本和原有版权声明。
