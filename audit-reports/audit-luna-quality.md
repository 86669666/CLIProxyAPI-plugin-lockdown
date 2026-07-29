**审计范围**
- 只读审计完成，未修改文件；基线为 `HEAD=a6bc9b56`，上游为 `upstream/main=a14dfc77`。
- 当前 fork 相对上游 **领先 3 个提交、落后 96 个提交**；因此质量差异主要是“安全策略补丁 + 大量上游未同步变更”的综合结果。

**质量指标对比**
- Fork：781 个 Go 文件、307 个测试文件、2,442 个测试函数、9,091 个生产函数。
- 上游：875 / 374 / 3,009 / 10,732；上游多 **136 个测试文件、567 个测试函数**。
- 测试文件占比：Fork `39.3%`，上游 `42.7%`，低约 `3.4pp`。
- 测试函数/生产函数仅是结构性指标，不等同真实覆盖率；仓库无 `coverprofile`，且 `go list ./...` 已因只读环境无法写 Go 模块缓存而失败，实际 `go test -cover ./...` 无法执行。
- Fork 新增 9 个策略测试，覆盖管理 API、配置归一化、根路径和响应头，但未覆盖完整插件执行链路。
- 上游新增的并发、资源释放、日志和插件流测试未同步，例如 `internal/config/credential_concurrency_test.go`、`internal/home/concurrency_release_test.go`、`internal/pluginhost/stream_bridge_test.go`、`internal/logging/*_test.go`。

**高风险技术债务**
- **P0：插件策略存在路由级旁路。** 锁定仅加在插件商店和部分配置变更接口；`ListPlugins`、`GetPluginConfig`、`DeletePlugin` 未统一拒绝，见 `internal/api/handlers/management/plugins.go:210`、`internal/api/handlers/management/plugins.go:327`。更重要的是动态管理/资源路由经 `NoRoute` 分发，`internal/api/server.go:1032`、`internal/api/server.go:1069` 未检查 `PluginsDisabledByPolicy()`。
- **P0：禁用插件不会主动卸载已加载插件。** `Host.ApplyConfig` 在 `!rc.Enabled` 时只清空路由、快照和运行时映射，没有调用卸载或关闭逻辑，见 `internal/pluginhost/host.go:198-206`；可能留下动态库、RPC 客户端、插件 goroutine 和外部资源。
- **P1：策略配置失败时静默 fail-open。** `CLIPROXY_DISABLE_PLUGINS=typo` 会被视为未启用，见 `internal/config/config.go:894-899`；安全策略更适合“非法值报警并 fail-closed”，至少应有启动日志和健康检查信号。
- **P1：后台生命周期不完整。** 每次 `NewHandler` 启动一个永久 ticker goroutine，未提供停止机制，见 `internal/api/handlers/management/handler.go:88-98`；测试和重复初始化会积累 goroutine。
- **P1：异步配置 reload 无 shutdown 管理。** `reloadConfigAfterManagementSaveAsync` 使用 `context.WithoutCancel` 并直接创建 goroutine，见 `internal/api/handlers/management/handler.go:227-245`；服务停止时无法等待或取消，插件加载阻塞时可能拖尾。
- **P1：并发字段访问不一致。** `SetLocalPassword`、`SetLogDirectory`、`SetPostAuthHook` 等直接写字段，未复用 `h.mu`，见 `internal/api/handlers/management/handler.go:247-260`；若与请求并发读，`go test -race` 存在潜在数据竞争。
- **P2：错误被吞掉，影响故障定位。** OAuth 回调持久化错误直接丢弃，见 `internal/api/server.go:605-627`；临时文件删除、关闭错误也被忽略，见 `internal/api/handlers/management/config_basic.go:132-145`。
- **P2：配置写入非原子。** `WriteConfig` 直接 `O_TRUNC` 后写文件，进程崩溃或磁盘异常可能留下空/半写配置，见 `internal/api/handlers/management/config_basic.go:93-107`。

**日志与可观测性**
- 新增策略拒绝只返回 HTTP 403，不记录结构化事件、请求来源、策略状态或计数器，见 `internal/api/handlers/management/handler.go:163-172`；无法回答“谁在尝试启用插件、次数多少”。
- 非法策略环境变量无 warning/error，部署拼写错误不可见。
- Fork 未同步上游新增的 `internal/logging/cpa_trace.go` 及相关日志测试，也未同步上游 OAuth 选择、Codex 媒体转发等可观测性增强。
- 现有 Logrus 结构化日志基础尚可，但关键边界仍存在“HTTP 返回错误、服务端无日志”的断点。

**重构优先级**
1. **P0：统一插件策略中间件/能力门面**：覆盖固定管理路由、动态管理路由、资源路由、Home 插件同步、删除和安装；策略拒绝必须在路由和运行时双重生效。
2. **P0：实现 Host 的真正 Disable/Shutdown 路径**：禁用时卸载 active/retired plugin、关闭 RPC/stream、清理注册表和后台任务，并增加回归测试。
3. **P1：引入可关闭的 Handler 生命周期**：保存 `cancel`、`WaitGroup`，由 `Server.Stop` 或 `Service.Shutdown` 统一停止 ticker 和异步 reload。
4. **P1：安全策略 fail-closed + 可观测**：非法值启动即报错或拒绝启动；增加 `plugins_disabled_by_policy` 日志、指标和健康状态。
5. **P1：补并发测试并执行 `-race`**：重点覆盖配置热更新、插件卸载、动态路由刷新、管理 API 并发写入。
6. **P2：统一错误包装与资源关闭策略**：记录 OAuth 回调写入失败、清理失败；配置保存改为临时文件、`fsync`、原子 rename。
7. **P2：尽快同步上游 96 个提交**：当前 fork 的测试和日志质量落后明显，继续在旧基线上修补会放大后续合并成本。

**总体结论**
- 当前 fork 的策略测试意图清晰，但实现仍是“配置层禁用”为主，尚未达到完整的运行时能力隔离。
- 主要风险集中在 **插件禁用不彻底、资源未释放、生命周期不可控、策略错误静默和日志缺口**。
- 建议先处理两个 P0，再同步上游并重新建立 `go test ./...`、`go test -race ./...`、`go test -coverprofile=coverage.out ./...` 的质量门禁。