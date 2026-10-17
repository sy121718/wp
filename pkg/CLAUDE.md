# pkg 组件开发说明

`pkg/` 存放项目的通用基础组件，采用 facade + provider/driver 的组织方式。

## 组件清单与职责

```text
pkg/
├── auth/       Session + Cookie 认证 + Redis 会话/封禁/心跳（已完成 JWT 迁移）
├── backpath/   写操作后的回跳地址（站内路径 + 按调用点白名单透传参数）
├── bulkids/    后台批量操作的 id 归一与边界校验
├── cache/      Redis 会话存储（Critical：auth 依赖，config 必启用）
├── captcha/    验证码（标准库自绘 PNG，GenerateImage；答案不下发）
├── casbin/     鉴权（自研 persist.Adapter，SyncedEnforcer）
├── crypto/     签名与哈希
├── database/   数据库（PostgreSQL 主库 / MySQL 兼容）
├── datarule/   数据权限（域白名单 + 方言引用符 + 部门整段匹配）
├── enums/      历史兼容常量仓库
├── facing/     自带对外文案的哨兵错误（声明处带 key + 中文，出口只调一次 FactOf）
├── i18n/       文案直查
├── jsonx/      唯一 JSON 编解码入口（v2 + 还原 v1 可观察行为的选项）
├── logger/     结构化日志
├── mailer/     邮件发送（issue #37）
├── money/      金额格式化唯一实现（FormatYuan / FormatYuanOrDash）
├── pagination/ 分页条数据 + 取址与页码收敛（Query / BaseURL / ClampPage / Slice）
├── pathkit/    站点路径归一化唯一实现（审计 CQ-012）
├── queue/      asynq 任务队列
├── reqparams/  请求参数与表单值的形状收敛（字符串 → 目标类型）
├── response/   统一响应结构
├── rls/        工程隔离的行级安全原语（DB-009：事务内 set_config app.project_id）
├── safepath/   「站内相对路径」判据唯一实现
├── sitehttps/  「当前部署对外是不是 HTTPS」单一判定
├── sitetz/     站点时区的单一声明点（审计 TX-011 / DB-018）
├── upload/     上传（local/qiniu，Windows 保留名与 O_NOFOLLOW 已加固）
├── utils/      工具
├── view/       视图引擎门面（模板名 + 变量 → 字节）
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
- `pagination` —— 事实层 `Facts/BuildPagination`（分页条数据）与请求层
  `Query`（成对取值、空白跳过）/ `BaseURL`（基地址不带 page/limit）/ `ClampPage`（页码收敛到末页）/
  `Slice`（内存切片分页）。`DefaultPageSize = 20`，模块只保留自己的每页条数常量。
  页面里这些助手一律从这里取：**每页条数可以按页面调，取址与收敛的口径不能各写一份**
  （基地址塞了 page 会生成两个同名参数，表现是「点了下一页没反应」且不报错）
- `upload` —— `Upload`（生产路径：provider 与配置来自全局 `Init`）；
  `Use` / `UseCfg` / `NewUploader` 是同族的 **provider 维度门面**（`Client` / `Uploader`），
  当前调用方是 `public/test/pkg/upload` 的用例 —— 生产没有任何调用点。
  给它们加调用方时先想清「为什么 `Upload` 不够」，不要只为绕过全局配置而用。

## 新增 pkg 的要求

目录清晰 · 根包只做 facade · 配置自己解析 · 校验自己在 `Init()` 做完 · 错误处理保持简单 ·
不在 pkg 内扩散业务语义中转
