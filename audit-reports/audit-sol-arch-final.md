**审计结论**
- 对比基线：lockdown `a6bc9b56`，upstream `c9417c8a`。
- lockdown 已形成“环境策略 → 配置强制关闭 → Host 快照清空 → 管理写接口拒绝”的基本闭环。
- 但它属于**软运行时隔离**，不是可靠的“插件代码绝不加载/执行”保证；存在启动时序和卸载不完整问题。

**关键对比**

1. **Host 禁用逻辑基本相同**

```go
if !rc.Enabled {
    h.managementRoutes = make(map[string]managementRouteRecord)
    h.resourceRoutes = make(map[string]resourceRouteRecord)
    h.rebuildActivePluginMapsLocked(nil)
    h.snapshot.Store(emptySnapshot())
    h.refreshThinkingProviders(nil)
    return
}
```

- lockdown：`internal/pluginhost/host.go:198`
- upstream：`internal/pluginhost/host.go:216`
- 作用：移除路由、能力快照和 thinking provider。
- 缺陷：没有调用插件 `Shutdown()`，也没有清空 `loaded`/`retired`；`rebuildActivePluginMapsLocked(nil)`仅清空版本和路径索引，见 `internal/pluginhost/host.go:580`。

2. **管理 API：lockdown 增加前置拒绝**

```go
func (h *Handler) PatchPluginEnabled(c *gin.Context) {
    if rejectDisabledPluginCapability(c) {
        return
    }
    // ...
}
```

- lockdown 拦截：
  - `PatchPluginEnabled`：`internal/api/handlers/management/plugins.go:213`
  - `PutPluginConfig`：`internal/api/handlers/management/plugins.go:252`
  - `PatchPluginConfig`：`internal/api/handlers/management/plugins.go:283`
  - 商店列表/安装：`internal/api/handlers/management/plugin_store.go:132`、`:227`
  - 整体 YAML 配置中的插件启用：`internal/api/handlers/management/config_basic.go:121`
- upstream 无这些策略检查，例如 `internal/api/handlers/management/plugins.go:213` 直接修改配置。
- lockdown 有意保留插件查询和删除接口；`DeletePlugin` 未拒绝，见 `internal/api/handlers/management/plugins.go:328`。删除属于风险收敛操作，保留合理。

3. **策略解析**

```go
raw := strings.TrimSpace(os.Getenv("CLIPROXY_DISABLE_PLUGINS"))
disabled, err := strconv.ParseBool(raw)
return err == nil && disabled
```

- lockdown：`internal/config/config.go:894`
- 配置归一化时强制：
  - `Plugins.Enabled = false`
  - 每个插件 `enabled = false`
  - 同步修改原始 YAML node  
  见 `internal/config/config.go:883`。
- upstream 的归一化位于 `internal/config/config_normalization.go:10`，没有进程级禁用策略。

4. **启动顺序**

```go
pluginHost.ApplyConfig(bootstrapCfg)
pluginHost.RegisterCommandLineFlags(...)
// ...
godotenv.Load(".env")
```

- lockdown：`cmd/server/main.go:139`，`.env` 到 `cmd/server/main.go:184` 才加载。
- upstream 启动顺序相同。
- 因此只有在**进程启动环境**预先设置策略时，lockdown 才能阻止 bootstrap 插件加载和 CLI flag 注册。
- lockdown 的 Home 插件同步代码还落后于 upstream；upstream 已增加按 `Plugins.Enabled` 跳过、凭证隔离及更完整的同步状态处理。

**安全问题**

- **高危：`.env` 时序绕过。** 若策略只写在工作目录 `.env`，插件会先被加载、初始化并注册 CLI flags，随后策略才生效。恶意插件代码已经执行。
- **高危：禁用不等于卸载。** 动态从 enabled 切换为 disabled 只撤销公开能力；插件进程、goroutine、文件句柄或初始化副作用可能继续存在。
- **中危：策略解析 fail-open。** `CLIPROXY_DISABLE_PLUGINS=treu`、空格外的非法值或部署模板错误都会静默恢复插件能力。
- **中危：策略分散。** 拦截依赖每个 handler 手工调用 helper；未来新增安装、上传、启用或通用配置写入口时容易漏加。
- **中危：fork 漂移。** lockdown 的 Host 同步实现和 Home 插件流程落后于 upstream，继续独立维护会扩大安全补丁缺口。
- **低危：查询面仍暴露。** `ListPlugins`/`GetPluginConfig` 在锁定模式下仍可枚举插件文件和配置；管理 API 若被越权，会增加信息泄漏面。

**修复优先级**

1. 在任何配置读取和插件 bootstrap 前加载策略；更稳妥的是仅接受真实进程环境或启动参数，并明确禁止将 `.env` 作为安全边界。
2. 策略变量存在但无法解析时**启动失败**，不要 fail-open；启动日志明确输出锁定状态。
3. `ApplyConfig(!Enabled)` 应执行完整停用：原子撤销注册、清空运行状态，并调用所有已加载插件的 `Shutdown()`；必要时要求重启完成强隔离。
4. 在管理路由组增加统一 lockdown middleware，handler 内检查仅作为纵深防御。
5. 锁定模式下考虑让插件列表、配置和商店接口统一返回 `403` 或最小化空响应。
6. 基于最新 upstream 重放 lockdown 补丁，保留 upstream 的可取消插件加载和新版 Home 同步流程。