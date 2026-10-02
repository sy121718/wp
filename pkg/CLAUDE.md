# pkg 组件开发说明

`pkg/` 存放项目的通用基础组件，采用 facade + provider/driver 的组织方式。

## 组件清单与职责

```text
pkg/
├── auth/       Session + Cookie 认证 + Redis 会话/封禁/心跳（已完成 JWT 迁移）
├── cache/      Redis 会话存储（Critical：auth 依赖，config 必启用）
├── captcha/    验证码（标准库自绘 PNG，GenerateImage；答案不下发）
├── casbin/     鉴权（自研 persist.Adapter，SyncedEnforcer）
├── crypto/     签名与哈希
├── database/   数据库（PostgreSQL 主库 / MySQL 兼容）
├── datarule/   数据权限（域白名单 + 方言引用符 + 部门整段匹配）
├── enums/      历史兼容常量仓库
├── i18n/       文案直查
├── logger/     结构化日志
├── queue/      asynq 任务队列
├── response/   统一响应结构
├── rls/        工程隔离的行级安全原语（DB-009：事务内 set_config app.project_id）
├── upload/     上传（local/qiniu，Windows 保留名与 O_NOFOLLOW 已加固）
├── utils/      工具
└── validate/   校验
```

> `cache` **不是**废弃组件：auth 的会话/封禁/心跳硬依赖它，`config/register.go` 中为 Critical 且置于 auth
> 之前初始化，`redis.enabled=false` 时启动 fail-fast（`auth.RequireSessionStorage`）。

## 总体原则

**1. facade 入口** —— 组件根包对外暴露统一 API，具体实现放 `provider/` 或 `driver/`。

**2. 生命周期** —— 组件统一由 `config.InitComponents()` / `config.CloseComponents()` 编排；但组件自己的
严格配置校验必须在各自 `Init()` 内部完成。

**3. 配置** —— 默认值在 `config/config.go`；每个 `pkg` 自己解析自己的配置；**`pkg` 不导入 `config`**；
`config` 不手工点名调用某个 `pkg` 的校验函数。

**4. 错误处理保持简单** —— 参数/状态校验直接返回简单中文提示；底层系统错误优先直接返回原始 `err`；
不做复杂的统一错误码中转，不做国际化翻译中转；初始化阶段配置不合法直接在本包 `Init()` 返回错误。

```go
// 不建议
return fmt.Errorf("创建日志目录失败: %w", err)
// 更倾向
return err
```

## response 组件约定

输出统一响应结构；使用数字状态码；**不维护字符串错误码**；**不做 i18n 翻译**。

```go
type Response struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

response.Success(c, data)
response.SuccessWithMessage(c, "保存成功", data)
response.ErrorWithMessage(c, 400, "请求参数错误")
```

## i18n 与 enums

`pkg/i18n` 只用于**直接查文案**（`i18n.GetText(key, lang)` / `i18n.GetHttpCode(code)`）。
`pkg/enums` 保留为历史兼容常量仓库。

以下位置**不要**依赖 i18n 或围绕 `pkg/enums` 设计响应逻辑：`pkg/response`、默认系统中间件错误返回、
`pkg` 默认错误提示。新代码直接写具体状态码和最终提示，不要再做「error code → 文案 → 状态码」的统一中转。

## 当前 facade 能力

- `database` —— 显式初始化、driver 分发、运行时性能选项
- `queue` —— 任务注册 facade、入队 facade
- `upload` —— `Upload`（生产路径：provider 与配置来自全局 `Init`）；
  `Use` / `UseCfg` / `NewUploader` 是同族的 **provider 维度门面**（`Client` / `Uploader`），
  当前调用方是 `public/test/pkg/upload` 的用例 —— 生产没有任何调用点。
  给它们加调用方时先想清「为什么 `Upload` 不够」，不要只为绕过全局配置而用。

## 新增 pkg 的要求

目录清晰 · 根包只做 facade · 配置自己解析 · 校验自己在 `Init()` 做完 · 错误处理保持简单 ·
不在 pkg 内扩散业务语义中转
