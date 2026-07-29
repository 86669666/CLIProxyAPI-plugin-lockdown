# 安全审计报告

## 审计结论

本次为**只读静态差异审计**。审计对象：

- Fork：`/opt/workspace/git/CLIProxyAPI-plugin-lockdown`
- 指定上游：`/opt/workspace/git/CLIProxyAPI-upstream`
- Fork HEAD：`a6bc9b56`
- 上游 HEAD：`c9417c8a`
- 公共基线：`09da52ad`
- Fork 独有提交：3 个
- 指定上游相对公共基线新增：161 个提交

总体结论：

- Fork 的插件 lockdown 是一项**有效的插件能力熔断和误配置防护**，能阻止标准启动路径、SDK Builder 和主要管理 API 重新启用插件。
- 它**不是插件沙箱**：插件仍是进程内动态代码，启用后拥有服务进程的文件、网络、内存和凭据权限。README 已正确披露该边界，没有夸大为完整隔离，见 `README_CN.md:55`。
- Lockdown **默认关闭且错误配置时 fail-open**。安全性强依赖部署系统在进程启动前正确设置 `CLIPROXY_DISABLE_PLUGINS=true`。
- 最高优先级问题是：**Home 日志转发携带原始 Authorization、文件接口跟随符号链接、多个上传/下载路径没有体积上限、配置文件权限过宽，以及管理 API 的任意外连/SSRF 能力**。
- 普通 API key 验证没有 fork 独有变更；主要风险是危险部署默认值、query 参数密钥和多凭据来源歧义。

---

# 安全加固点

## 1. 插件能力锁定

Fork 在配置规范化阶段强制关闭全局插件和每个插件实例，并同步修改原始 YAML 节点：

- 全局 `plugins.enabled=false`
- 每个 `plugins.configs.<id>.enabled=false`
- 原始插件配置节点中的 `enabled=false`

证据：`internal/config/config.go:883`

标准 SDK Builder 同样执行规范化，减少自定义 SDK 启动路径遗漏：

- `sdk/cliproxy/builder.go:190`

策略启用后，插件能力响应头返回 `0`：

- `internal/pluginhost/support.go:5`

管理 API 对以下高风险操作统一返回 `403`：

- 插件商店查询
- 插件安装
- 插件 enable 变更
- 插件配置 PUT/PATCH
- 通过完整 YAML 重新启用插件

策略拒绝函数：`internal/api/handlers/management/handler.go:163`

Fork 同时明确保留插件盘点、配置读取和删除清理接口，属于文档声明的设计选择，而不是未披露绕过：

- `README_CN.md:108`
- `README_CN.md:145`

## 2. 管理 API 密钥验证

管理接口已经具备较完整的基础认证控制：

- 本地和远程请求均要求管理密钥。
- 远程请求额外要求 `allow-remote-management=true`。
- 环境密钥和本地密码使用常量时间比较。
- 配置密钥支持 bcrypt。
- 单 IP 连续失败 5 次后封禁 30 分钟。

证据：

- `internal/api/handlers/management/handler.go:273`
- `internal/api/handlers/management/handler.go:348`
- `internal/api/handlers/management/handler.go:386`
- `internal/api/handlers/management/handler.go:395`

## 3. 文件路径基础校验

Auth 文件下载限制了：

- 空文件名
- `/` 和 `\`
- Volume Name
- 非 `.json` 文件

Auth 上传使用 `filepath.Base` 规整客户端文件名，写入权限为 `0600`：

- `internal/api/handlers/management/auth_files.go:926`
- `internal/api/handlers/management/auth_files.go:957`

日志下载具有文件名前后缀校验和 lexical 路径边界检查：

- `internal/api/handlers/management/logs.go:364`
- `internal/api/handlers/management/logs.go:951`

## 4. 插件安装供应链检查

插件安装链具备以下防护：

- 插件 ID 和版本格式验证
- SHA-256 checksum 验证
- ZIP 绝对路径及 `../` 检查
- 拒绝反斜杠路径
- 拒绝 ZIP 中的符号链接和非普通文件
- 动态库必须位于 ZIP 根目录
- 原子写入插件文件

证据：

- `internal/pluginstore/install.go:243`
- `internal/pluginstore/install.go:318`
- `internal/pluginstore/install.go:375`
- `internal/pluginstore/install.go:392`

因此，未发现传统 Zip Slip 直接写出插件目录的问题。

## 5. 配置上传验证

完整 YAML 上传会：

1. 解析 YAML。
2. 检查 lockdown 下是否请求启用插件。
3. 使用随机临时文件执行完整配置加载验证。
4. 验证成功后写入正式配置。

证据：`internal/api/handlers/management/config_basic.go:110`

Fork 把验证文件移动到系统临时目录，降低配置目录内产生临时秘密副本的概率：

- `internal/api/handlers/management/config_basic.go:126`

## 6. 已有日志脱敏

现有日志控制包括：

- Gin access log 对敏感 query 参数脱敏：`internal/logging/gin_logger.go:50`
- 请求详细日志 URL 对敏感 query 参数脱敏：`internal/api/middleware/request_logging.go:312`
- 本地详细请求日志对请求头调用敏感值掩码：`internal/logging/request_logger.go:1038`
- 管理 API 路由不进入普通请求详细日志：`internal/api/middleware/request_logging.go:456`
- 配置差异日志不会直接输出 API key/secret 值。

---

# 潜在漏洞

## 高风险

### H-01：Home 日志转发泄露原始 Authorization

**归属：Fork 与当前基线共同存在**

本地日志文本会对请求头脱敏，但转发给 Home 的结构化 payload 使用未经脱敏的原始 headers：

- 原样复制头部：`internal/logging/request_logger.go:430`
- 转发 Home：`internal/logging/request_logger.go:453`

测试甚至明确断言 Home 收到：

```text
Authorization: Bearer secret
```

证据：`internal/logging/request_logger_home_test.go:93`

**影响：**

- 客户端 API key、Bearer token 被跨信任边界传输。
- Home 服务、传输链路、Home 日志或调试系统被攻破后，可批量获取客户端凭据。
- 不符合常见“认证凭据不得进入日志/遥测”的合规要求，对应 CWE-532。

**攻击条件：**

- Home request-log 转发已启用。
- 请求包含 Authorization 或其他敏感认证头。

**建议：**

- 在构造 Home payload 前调用与本地日志相同的 header redactor。
- 默认删除 `Authorization`、`Proxy-Authorization`、`Cookie`、`Set-Cookie`、`X-Api-Key`、`X-Goog-Api-Key`。
- Home 如确需身份关联，只发送不可逆 HMAC 指纹或内部 principal ID。
- 增加测试，明确断言 Home payload 不包含原始凭据。

### H-02：Lockdown 不是插件沙箱

**归属：架构设计风险；README 已正确披露**

插件是进程内动态代码。一旦插件被启用和加载，它可继承服务进程的：

- 文件系统权限
- 网络访问权限
- 环境变量
- 内存与凭据访问能力
- 进程稳定性影响能力

Lockdown 仅阻止加载和管理能力，不提供 syscall、网络、文件系统、CPU、内存或进程级隔离。

关闭配置时，Host 清除路由和快照，但没有从 Go 进程真正卸载已经执行过的任意代码：

- `internal/pluginhost/host.go:182`
- `internal/pluginhost/host.go:198`

恶意插件已经创建的 goroutine、打开的文件、全局状态和外连不会自动撤销。

**建议：**

- 高安全部署应在构建期完全排除插件代码，或将插件迁移到独立低权限进程。
- 使用独立 UID、只读根文件系统、最小挂载、seccomp/AppArmor/SELinux 和 egress allowlist。
- 将 `CLIPROXY_DISABLE_PLUGINS` 作为启动期不可变策略，禁止运行时降级。
- 不应把运行时重新设置环境变量视为安全撤销机制。

---

## 中高风险

### MH-01：Auth 文件及日志下载跟随符号链接

**归属：Fork 与上游继承问题；CWE-59**

Auth 文件路径进行了字符串级文件名检查，但最终通过普通 `os.ReadFile`、`os.WriteFile` 访问目标，没有：

- `Lstat` 拒绝符号链接
- `O_NOFOLLOW`
- 解析真实路径后重新验证目录边界
- 安全的 directory-relative open

上传写入路径：

- `internal/api/handlers/management/auth_files.go:946`
- `internal/api/handlers/management/auth_files.go:957`

如果 AuthDir 内存在：

```text
victim.json -> /path/to/sensitive/file
```

则可能导致：

- 下载接口读取任意进程可读文件。
- 上传接口截断或覆盖任意进程可写文件。

日志下载也只验证 lexical path，随后 `os.Stat` 和 `FileAttachment` 会跟随符号链接：

- `internal/api/handlers/management/logs.go:386`
- `internal/api/handlers/management/logs.go:400`

**攻击条件：**

- 攻击者能在 AuthDir 或日志目录创建/替换符号链接。
- 常见于共享卷、多容器共享目录、弱本地权限或被攻破的旁路进程。
- 文件 API 仍需管理认证。

**建议：**

- 使用统一 `safeOpenBeneath()`。
- Linux 优先使用 `openat2` 的 `RESOLVE_BENEATH | RESOLVE_NO_SYMLINKS`。
- 跨平台至少对目录及目标执行 `Lstat`，拒绝所有 symlink。
- 写文件使用目录句柄、临时文件、原子 rename，并在 rename 前后重新验证。
- AuthDir、日志目录必须仅服务 UID 可写。

### MH-02：上传和配置接口缺少请求体上限

**归属：Fork 与上游继承问题；CWE-400**

以下路径直接 `io.ReadAll`，没有 `http.MaxBytesReader` 或 `io.LimitReader`：

- 配置 YAML：`internal/api/handlers/management/config_basic.go:111`
- Multipart Auth 文件：`internal/api/handlers/management/auth_files.go:936`
- Raw Auth 文件请求也存在无界读取路径。

**影响：**

- 大请求导致进程内存耗尽。
- Multipart 解析可能先消耗临时磁盘。
- 超大 YAML 会额外消耗解析 CPU 和内存。
- 拥有管理密钥的内部滥用或密钥泄露可导致稳定 DoS。

**建议：**

- 配置 YAML 限制为 1–4 MiB。
- 单个 Auth JSON 限制为 1 MiB 左右。
- 限制 multipart 文件数量及总大小。
- 超限返回 `413 Payload Too Large`。
- 在 Gin multipart 解析前设置限制，而不是读取完成后检查长度。
- 增加 YAML 深度、节点数和 alias 复杂度测试。

### MH-03：插件商店下载和 ZIP 解压缺少可靠上限

**归属：插件启用时有效；lockdown 正确启用时不可达**

GitHub release、registry metadata 和 artifact 下载调用 `get(..., maxSize=0)`：

- Registry：`internal/pluginstore/github.go:40`
- Release metadata：`internal/pluginstore/github.go:58`
- Artifact：`internal/pluginstore/github.go:116`

`maxSize=0` 表示没有响应体上限。ZIP 中目标动态库也使用无界 `io.ReadAll`：

- `internal/pluginstore/install.go:351`

因此恶意或被攻破的插件源可造成：

- 超大 HTTP 响应内存耗尽
- 压缩炸弹解压内存耗尽
- 超大插件文件磁盘消耗

Checksum 只能保证内容与源提供的 checksum 一致，不能阻止恶意源同时提供恶意归档和对应 checksum。

**建议：**

- Registry/metadata 限制在 1–4 MiB。
- checksum 文件限制在数百 KiB。
- 压缩归档限制在明确大小，例如 64 MiB。
- 解压后的动态库设置独立上限。
- 同时验证 HTTP `Content-Length` 和实际读取字节数。
- 官方插件源应使用签名清单或 Sigstore，而不只依赖同源 checksum。

### MH-04：管理 APICall 是带凭据的任意 SSRF 能力

**归属：上游管理功能，不是 lockdown 新增；CWE-918**

管理端 `/v0/management/api-call` 接受调用方提供的任意 URL、方法、headers、Host override 和 body：

- URL 只检查存在 scheme/host：`internal/api/handlers/management/api_tools.go:108`
- 可将选定账号 token 替换进任意 header：`internal/api/handlers/management/api_tools.go:127`
- 可覆盖 HTTP Host：`internal/api/handlers/management/api_tools.go:164`

没有阻止访问：

- `127.0.0.0/8`
- RFC1918 私网
- 链路本地地址
- 云 metadata 地址
- Unix/内部管理服务对应的代理路径
- DNS rebinding
- 重定向后的内网地址

响应体也通过 `io.ReadAll` 无界读取：

- `internal/api/handlers/management/api_tools.go:192`

**风险判断：**

这是受管理密钥保护的运维功能，不能等同于未认证 SSRF；但一旦管理密钥泄露，攻击者可同时获得内网探测和 OAuth/API token 外送能力。

**建议：**

- 默认禁用 APICall，单独配置 capability 开关。
- URL 使用 allowlist，而不是 denylist。
- 每次 DNS 解析后检查最终 IP。
- 每次 redirect 重新执行 scheme、host 和 IP 校验。
- 禁止 Host override，或仅允许与 URL host 一致。
- token 只能发送到该 credential 已配置的 provider host。
- 对响应体设置上限。

---

## 中风险

### M-01：Lockdown 环境变量错误时 fail-open

策略实现为：

```go
disabled, err := strconv.ParseBool(raw)
return err == nil && disabled
```

证据：`internal/config/config.go:895`

以下配置不会禁用插件：

```text
CLIPROXY_DISABLE_PLUGINS=tru
CLIPROXY_DISABLE_PLUGINS=enabled
CLIPROXY_DISABLE_PLUGINS="true "
```

最后一项会先 TrimSpace，因此实际可接受；但拼写错误、模板渲染错误或不支持值会静默 fail-open。

README 已披露变量缺失或无法解析时不启用：

- `README_CN.md:100`

**建议：**

- 环境变量只要存在且不是合法布尔值，就拒绝启动。
- 安全发行版可把 lockdown 设为编译期默认开启。
- 启动日志输出明确的结构化状态，但不得输出环境中其他秘密。
- 部署健康检查必须断言 `X-CPA-SUPPORT-PLUGIN: 0`。

### M-02：配置文件新建权限为 0644

管理接口写配置使用：

```go
os.OpenFile(..., 0644)
```

证据：`internal/api/handlers/management/config_basic.go:93`

配置文件可能包含：

- 上游 API key
- proxy 凭据
- 管理配置
- 插件源认证环境变量名称
- 内部 URL 和网络拓扑

新文件可能被同机其他用户读取；已有文件权限也不会被收紧。

**建议：**

- 默认使用 `0600`。
- 启动时检查并告警 group/world-readable 配置文件。
- 使用同目录随机临时文件、`0600`、`fsync`、原子 rename。
- 拒绝配置路径本身为 symlink。
- 对容器挂载设置只允许服务 UID 读取。

### M-03：普通 API key 允许 query 参数传递

支持的凭据来源包括：

- `Authorization`
- `X-Goog-Api-Key`
- `X-Api-Key`
- Query `key`
- Query `auth_token`

证据：`internal/access/config_access/provider.go:62`

应用自身日志虽然会 mask query，但 query 中的密钥仍可能进入：

- 反向代理 access log
- WAF/CDN
- 浏览器历史
- APM
- Referer
- 网络诊断工具

**建议：**

- 默认禁止 query API key。
- 如 Gemini 兼容确有需要，只在特定兼容路由开启。
- 在边缘代理层同步配置 query redaction。
- 禁止通过 GET URL 承载长期凭据。

### M-04：多个凭据来源“任一有效即通过”

验证器依次尝试多个凭据，只要其中任意一个匹配就认证成功：

- `internal/access/config_access/provider.go:77`
- `internal/access/config_access/provider.go:88`

例如：

- Authorization 无效
- Query key 有效

请求仍会通过。可能造成 credential smuggling、代理层与应用层认证判断不一致，以及审计归因歧义。

**建议：**

- 同一请求出现多个不同凭据时 fail closed。
- 或定义单一严格优先级，并拒绝低优先级凭据与高优先级凭据冲突。
- 日志记录凭据来源，但不得记录值。

### M-05：响应头、错误文本和请求/响应体脱敏不完整

响应头被原样写入请求日志，没有调用敏感 header mask：

- `internal/logging/request_logger.go:1286`
- `internal/logging/request_logger.go:1296`

可能记录：

- `Set-Cookie`
- 上游 token header
- 调试认证头
- 私有请求标识

API error 的 `error.Error()` 也被原样记录：

- `internal/logging/request_logger.go:1258`
- `internal/logging/request_logger.go:1271`

此外，请求和响应 body 可能包含：

- 用户 PII
- 源代码
- 商业机密
- 模型 prompt
- OAuth 或工具调用输出中的凭据

**建议：**

- 对 response headers 使用与 request headers 相同的脱敏器。
- `Set-Cookie` 默认整项删除。
- 对 error chain、URL、JSON body 建立统一 redactor。
- request-log 默认关闭；启用时设置采样、保留期、访问审计和加密。
- 为 OpenAI/Claude/Gemini 常见 JSON 字段提供可配置字段级脱敏。

### M-06：Fork 落后实际指定上游 161 个提交

Fork 文档固定在较旧基线上，指定上游已前进 161 个提交。安全相关漂移包括：

- 上游插件 client guard 增加 context cancellation 和异步 shutdown。
- Fork 当前同步等待所有插件调用完成；恶意插件永不返回时，可永久阻塞 shutdown/reload：`internal/pluginhost/client_guard.go:54`
- 上游增加插件商店认证材料生命周期和错误信息清理。
- 上游增加 Auth 文件按 identity/auth index 过滤等修复。

在 lockdown 正确启用时，插件运行时风险大部分不可达；但变量遗漏或允许插件的部署仍受旧实现影响。

**建议：**

- 制定固定的上游安全同步周期。
- 每次 rebase 重点复审 `internal/pluginhost`、`internal/pluginstore`、`internal/logging` 和 `auth_files.go`。
- 迁移上游安全修复时，确保不会恢复被 lockdown 禁止的插件入口。

---

## 低风险与部署风险

### L-01：没有 API key provider 时普通 API 匿名开放

Access Manager 在没有 provider 时返回成功语义：

- `sdk/access/manager.go:45`

服务端明确保留 legacy 行为：没有认证 provider 时允许所有请求：

- `internal/api/server.go:2011`

这不是认证绕过，而是危险部署默认值。

**建议：**

- 生产启动时若 `api-keys` 为空则拒绝启动，除非显式设置 `allow-anonymous=true`。
- 健康检查确认认证 provider 数量大于零。
- 网关层再实施一层认证。

### L-02：普通 API key 查找不是常量时间

普通 API key 使用 map 精确查找：

- `internal/access/config_access/provider.go:92`

未发现前缀匹配、空 key 或部分匹配绕过。远程 timing 利用通常较困难，因此列为加固项而非高危漏洞。

建议可改为：

- 存储 key 的 HMAC/SHA-256 指纹后比较。
- 避免将原始 key 作为 `Principal` 长期传递或写入上下文。

### L-03：根页面伪装仅降低指纹

根路径改为通用 Welcome 页面和安全响应头，可以减少直接产品识别，但不构成访问控制。其他 API、错误格式、端口、TLS 和响应头仍可暴露实现。

README 已正确披露该限制：

- `README_CN.md:177`

---

# 五项专项评价

| 专项 | 评价 | 结论 |
|---|---|---|
| 插件沙箱隔离 | 部分通过 | 有可靠 kill switch，但没有真正进程/系统调用隔离 |
| API key 验证 | 部分通过 | 管理密钥较强；普通 API 存在匿名默认、query key 和多来源歧义 |
| 文件上传/下载 | 不通过 | 路径穿越有防护，但缺少体积上限和 symlink 安全打开 |
| 配置注入 | 部分通过 | 未发现直接 shell/template 执行；存在可控外连、SSRF、无界 YAML 和弱文件权限 |
| 日志脱敏 | 不通过 | 本地请求头有脱敏，但 Home 原始头、响应头、错误和 body 未统一处理 |

---

# 合规性检查清单

## 插件与供应链

- [ ] 生产进程环境在 `exec` 前设置 `CLIPROXY_DISABLE_PLUGINS=true`
- [ ] 不仅依赖工作目录 `.env`
- [ ] 启动探针验证 `X-CPA-SUPPORT-PLUGIN: 0`
- [ ] 插件目录对服务进程只读或不挂载
- [ ] 高安全构建完全排除插件代码
- [ ] 插件下载设置压缩和解压大小上限
- [ ] 插件来源采用 allowlist
- [ ] 插件制品使用独立签名验证，而非仅同源 checksum
- [ ] 定期同步上游 pluginhost/pluginstore 安全修复
- [ ] 容器启用 seccomp/AppArmor/SELinux 和 egress 策略

## API 认证

- [ ] 生产配置至少一个普通 API access provider
- [ ] 无认证配置时拒绝启动，而不是匿名开放
- [ ] 禁止或严格限定 query 参数 API key
- [ ] 多个不同凭据来源同时出现时拒绝请求
- [ ] 管理接口仅监听管理网络或 localhost
- [ ] `allow-remote-management` 默认为 false
- [ ] 管理密钥使用高熵值或 bcrypt
- [ ] 正确配置可信代理，防止基于伪造 `X-Forwarded-For` 绕过 IP 限速
- [ ] 管理密钥轮换和失败事件进入安全审计日志
- [ ] API principal 使用 key 指纹，不存储完整 key

## 文件上传与下载

- [ ] 配置上传设置最大请求体
- [ ] Auth JSON 设置单文件和总请求体上限
- [ ] Multipart 设置文件数限制
- [ ] AuthDir 和日志目录仅服务 UID 可写
- [ ] 所有下载拒绝 symlink
- [ ] 所有写入使用 no-follow/beneath 语义
- [ ] 写入采用临时文件、`0600` 和原子 rename
- [ ] 下载响应设置 `Cache-Control: no-store`
- [ ] Auth 文件内容执行 JSON schema 校验
- [ ] 日志下载操作记录管理审计事件

## 配置安全

- [ ] `config.yaml` 权限为 `0600`
- [ ] 配置文件路径不是 symlink
- [ ] YAML 设置字节数、深度、节点数和 alias 限制
- [ ] `base-url`、`proxy-url`、插件源 URL 使用 allowlist
- [ ] 禁止 URL 用户信息和敏感 query 参数
- [ ] 管理 APICall 默认关闭
- [ ] APICall 禁止私网、环回、链路本地和 metadata IP
- [ ] APICall redirect 后重新验证目标
- [ ] OAuth token 只能发往对应 provider 域名
- [ ] 配置变更具备操作者、时间、旧值指纹和审批记录

## 日志与隐私

- [ ] Home payload 删除原始 Authorization 和 Cookie
- [ ] Request 与 response headers 使用同一脱敏器
- [ ] 默认删除 `Set-Cookie`
- [ ] Query、URL、error chain 和 JSON body 使用统一 redactor
- [ ] 模型 prompt/response 日志默认关闭或采样
- [ ] 日志目录权限符合最小权限
- [ ] 静态日志和 Home 传输均加密
- [ ] 配置日志保留周期及自动删除
- [ ] 对日志访问、下载和导出进行审计
- [ ] 进行 PII、密钥和源代码泄露扫描

---

## 修复优先级

建议按以下顺序处理：

1. **立即停止向 Home 发送原始认证头。**
2. **为 Auth/config/APICall/plugin 下载增加硬性体积上限。**
3. **统一实现拒绝 symlink 的安全文件访问 helper。**
4. **将 `config.yaml` 权限调整为 `0600` 并原子写入。**
5. **限制 APICall SSRF 和 token 可发送域名。**
6. **让 lockdown 非法环境变量值拒绝启动。**
7. **禁止生产环境无 API key provider 的匿名启动。**
8. **同步指定上游的 plugin client cancellation、插件商店认证清理和 Auth 文件修复。**

本次未修改任何文件，也未运行可能写入 Go build cache 的测试命令；结论基于 fork、指定上游和公共基线之间的只读静态代码审计。