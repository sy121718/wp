# pkg 组件开发说明

`pkg/` 存放项目的通用基础组件，采用 facade + provider/driver 的组织方式。

## 当前组件

```text
pkg/
├── auth/          # Session + Cookie 认证 + Redis 会话/封禁/心跳（已完成 JWT 迁移）
├── cache/         # Redis 会话存储（Critical：auth 依赖，config 必启用；见 config/register.go）
├── captcha/       # 验证码（标准库自绘 PNG，GenerateImage；答案不下发）
├── casbin/        # 鉴权（自研 persist.Adapter，SyncedEnforcer）
├── crypto/        # 签名与哈希
├── database/      # 数据库（PostgreSQL 主库 / MySQL 兼容）
├── datarule/      # 数据权限（方言引用符 + 部门整段匹配）
├── enums/         # 历史兼容常量仓库
├── i18n/          # 文案直查
├── lock/          # 已无调用方（遗留，可清理）
├── logger/        # 结构化日志
├── queue/         # asynq 任务队列
├── response/      # 统一响应结构
├── upload/        # 上传（local/qiniu，Windows 保留名与 O_NOFOLLOW 已加固）
├── utils/         # 工具
└── validate/      # 校验
```

> `cache` 不再是废弃组件：auth 的会话/封禁/心跳硬依赖它，`config/register.go` 中为 Critical 且置于 auth 之前初始化，`redis.enabled=false` 时启动 fail-fast（`auth.RequireSessionStorage`）。`lock/` 已无 import 方，待清理。

## 总体原则

### 1. facade 入口

组件根包负责对外暴露统一 API，具体实现放在 `provider/` 或 `driver/`。

### 2. 生命周期

组件统一由 `config.InitComponents()` / `config.CloseComponents()` 编排。
但组件自己的严格配置校验必须在各自 `Init()` 内部完成。

### 3. 配置

- 默认值在 `config/config.go`
- 每个 `pkg` 自己解析自己的配置
- `pkg` 不导入 `config`
- `config` 不手工点名调用某个 `pkg` 的校验函数

### 4. 错误处理

`pkg` 保持简单：

- 参数/状态校验：直接返回简单中文提示
- 底层系统错误：优先直接返回原始 `err`
- 不做复杂的统一错误码中转
- 不做复杂的国际化翻译中转
- 初始化阶段如果配置不合法，直接在本包 `Init()` 返回错误

不建议：

```go
return fmt.Errorf("创建日志目录失败: %w", err)
return fmt.Errorf("请求七牛上传失败: %w", err)
```

更倾向：

```go
return err
```

## response 组件约定

`pkg/response` 当前定位很简单：

- 输出统一响应结构
- 使用数字状态码
- 不维护字符串错误码
- 不做 i18n 翻译

结构：

```go
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}
```

使用：

```go
response.Success(c, data)
response.SuccessWithMessage(c, "保存成功", data)
response.ErrorWithMessage(c, 400, "请求参数错误")
```

## i18n 组件约定

`pkg/i18n` 仍然存在，但只用于直接查文案：

```go
text := i18n.GetText("ui_button_submit", "zh-CN")
httpCode := i18n.GetHttpCode("ErrAdminNotFound")
```

当前不要在这些位置继续依赖 i18n：

- `pkg/response`
- 默认系统中间件错误返回
- `pkg` 默认错误提示

## enums 组件约定

`pkg/enums` 当前保留为历史兼容常量仓库。

但新代码约定是：

- `pkg` 和系统包不要围绕它设计响应逻辑
- 不要再做“error code -> 文案 -> 状态码”的统一中转
- 直接写具体状态码和最终提示

## 当前 facade 能力

### database

- 显式初始化
- driver 分发
- 运行时性能选项

### queue

- 任务注册 facade
- 入队 facade

### upload

- `Upload`
- `UploadWithConfig`
- `Use`
- `UseCfg`
- `NewUploader`
- `NewUploaderWithConfig`

## 新增 pkg 的要求

- 目录清晰
- 根包只做 facade
- 配置自己解析
- 校验自己在 `Init()` 做完
- 错误处理保持简单
- 不在 pkg 内扩散业务语义中转
