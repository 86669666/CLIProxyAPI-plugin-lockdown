# P0 高危问题修复计划

## 修复顺序

### 1. H-01: Home 日志泄露原始 Authorization
- 文件: internal/logging/request_logger.go:430
- 修复: 调用已有的 header redactor
- 预计: 5 分钟

### 2. MH-01: Auth/日志下载跟随符号链接
- 文件: internal/api/handlers/management/auth_files.go:946
- 修复: 实现 safeOpenBeneath() + O_NOFOLLOW
- 预计: 15 分钟

### 3. MH-02: 上传/配置接口无请求体上限
- 文件: internal/api/handlers/management/config_basic.go:111
- 修复: 添加 MaxBytesReader
- 预计: 10 分钟

### 4. P0-1: .env 时序绕过
- 文件: cmd/server/main.go:184
- 修复: 提前加载策略到 bootstrap 前
- 预计: 10 分钟

### 5. P0-2: 禁用不等于卸载
- 文件: internal/pluginhost/host.go:198
- 修复: 实现完整 Shutdown 流程
- 预计: 20 分钟

总预计: 60 分钟
