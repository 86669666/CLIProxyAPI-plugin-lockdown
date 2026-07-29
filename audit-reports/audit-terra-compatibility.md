**审计基线**
- Fork：`v7.2.80-3-ga6bc9b56`，提交 `a6bc9b56`（2026-07-25）；上游：`v7.2.104`，提交 `c9417c8a`（2026-07-28）。
- 仅做只读静态审计；未修改文件、未执行构建或集成测试。

**Breaking Changes**
- **插件能力可被策略硬禁用**：当 `CLIPROXY_DISABLE_PLUGINS=true`，插件商店、安装、启停和配置 Management API 保留路由但改为 `403`，body 含 `error: "plugin_capability_disabled"`。`PUT /v0/management/config.yaml` 若请求启用插件也会被拒绝。`internal/api/handlers/management/handler.go:163`、`internal/api/handlers/management/config_basic.go:121`。
- **插件配置被强制降级**：配置加载会将全局插件开关设为 false，并禁用每个插件实例；依赖插件 provider/model 的 `/v1/chat/completions` 请求因无可用执行器而不兼容。`internal/config/config.go:883`。
- **认证文件的 `weight` 语义移除**：JSON 文件仍可读取，但 fork 不再验证、加载或调度 `weight`；原本 `weight: 0` 排除账号、或加权轮询的部署会变为普通选择策略。`sdk/auth/filestore.go:76`。
- **Codex OAuth 文件名策略变更**：fork 改为以套餐后缀命名，且仅 team/k12 才加入账户 hash；依赖旧文件路径、或同邮箱同套餐多账号的自动化可能产生重复或覆盖风险。`internal/auth/codex/filename.go:13`。

**兼容结论**
- **Management API 静态端点集合兼容**：两边 `/v0/management` 的 method/path 集合完全一致，且仍由管理可用性中间件和管理鉴权保护。fork `internal/api/server.go:818`；上游 `internal/api/server_management.go:14`。
- **`/v1/chat/completions` 标准行为兼容**：两边均在 `/v1` 使用相同认证中间件注册该路由，核心 `openai_handlers.go` 文件字节一致。fork `internal/api/server.go:520`；上游 `internal/api/server_routes.go:60`；handler `sdk/api/handlers/openai/openai_handlers.go:103`。
- **标准错误结构兼容**：两边均为 `{"error":{"message","type","code?"}}`。fork `sdk/api/handlers/handlers.go:36`；上游 `sdk/api/handlers/handlers.go:33`。
- **Responses SSE 错误格式兼容**：`openai_responses_stream_error.go` 内容一致，仍输出顶层 `type: "error"`、`code`、`message`、`sequence_number`。
- **Responses WebSocket 协议表面兼容、实现有分叉**：`response.create`、`response.append`、`response.completed` 等公开消息标签仍存在；但上游已拆分为多个 WebSocket 支持文件，fork 保留单体实现。建议对 `previous_response_id`、断线重放、预热 `generate:false`、工具调用修复做黑盒回归后再判定“完全兼容”。

**迁移指南**
- 若需要上游插件能力，确保未设置 `CLIPROXY_DISABLE_PLUGINS=true`；若 fork 的锁定策略是预期安全控制，则从管理客户端移除插件安装/启停/配置调用，并处理 `403/plugin_capability_disabled`。
- 配置发布前移除插件启用请求；不要依赖“写入后再由服务启动插件”的旧流程。
- 迁移认证目录前先备份；管理端应重新列举 auth files，而非缓存 Codex 文件名或路径。按 fork 命名规则重新认证/去重，重点检查同邮箱、多账号、同套餐的 Codex 凭据。
- 删除基于 `weight` 的容量分配假设；如仍需配额隔离，改用账号拆分、模型/提供商路由或上游版本的加权调度。
- 为 WebSocket 客户端补充升级回归：首包、`response.append`、带/不带 `previous_response_id`、上游失败、流提前关闭、工具调用。

**版本兼容矩阵**

| 接口/能力 | Fork 相对上游 | 兼容级别 |
|---|---|---|
| Management 静态 method/path | 完全相同 | 兼容 |
| Management 插件接口（策略关闭时） | 返回 `403 plugin_capability_disabled` | 破坏性 |
| `config.yaml` 启用插件 | 被拒绝或归一化为禁用 | 破坏性 |
| `/v1/chat/completions`，内建 provider | 路由、认证、核心 handler 相同 | 兼容 |
| `/v1/chat/completions`，插件 provider | 插件锁定时不可用 | 条件性破坏 |
| 认证 JSON 基础字段 | 可读取，未见 schema 破坏 | 兼容 |
| 认证 `weight` 字段 | 保留但不再生效 | 语义破坏 |
| Codex 凭据文件路径 | 新写入名称不同 | 迁移必需 |
| OpenAI HTTP 错误 envelope | 相同 | 兼容 |
| Responses SSE 错误 chunk | 相同 | 兼容 |
| Responses WebSocket | 消息标签保持，内部实现分叉 | 需黑盒验证 |