# internal/module 开发规范

通用分层判据在 AGENTS.md「分层判据」，本文件只写本目录的结构与特有约束。

## 目录
```text
<mod>/
  contract/      对外契约：接口 + 不可变 DTO（别的模块只能 import 这里）
  dto/           请求/响应数据结构（只做绑定与校验；不带展示字段）
  enums/         枚举常量、错误 key 与兜底表、界面词条 key
  model/         表访问单元（Repository）：本模块表 CRUD 与聚合内原子组合
  service/       业务规则、事务边界、多表拼装（只出数据，不出文案）
  inbound/http/  控制器：<mod>_router.go 路由 + <domain>_handle.go 处理器
  inbound/mcp/   AI 工具（读用 mcp.New、写用 mcp.NewWrite）
  outbound/      对外部系统的适配器
```

## 控制器
- 文件名只有两种：`<mod>_router.go`、`<domain>_handle.go`；类型 `<Module>Handle` / `<Module><Domain>PageHandle`。
- 方法体只允许：`binding.JSON/Form/Query/Field`、校验失败转响应、调 service、`return`、`response.*`。
- 页面处理器签名 `func(*gin.Context) (any, error)`；模板名只出现在 router（`adminui.Page/Bare`）。
- 一文件 = 一个业务功能；单方法 ≤60 行；不 import `internal/adminui`（router 除外）。

## service
- 只返回数据对象与事实（`pkg/facing` 的 key + 兜底）；不取词、不格式化、不拼句。
- 事务边界在这里：涉及两处以上写入必须同一事务，句柄从 model 下传。
- 不调用 model 的 `DB(ctx)`/`RevisionDB(ctx)`（`scripts/check-service-db-boundary.sh`）。
- 跨模块只用对方的 `contract`；不 import 对方的 model/service。

## model
- 只做本模块表的 CRUD 与聚合内原子组合；不写业务规则、不写死业务条件、不跨聚合事务。
- 不声明列型（gorm 能推断就交给它）；跨模块表名一律走契约，不在 model 里出现别人的表。

## 响应与错误
- 业务错误在声明处给状态码：`enums.Err(key)` 返回 `facing.WithStatus(...)`；出口只调 `response.ErrorStatusAuto`。
- 兜底文案跟声明处走（`errFallbacks` + `Err`/`ErrDetail`），明细用半角 ": "。
- 文案事实 / 界面 key 分域：界面 key 前缀 admin./shell./user./workbench. 或 Msg*。

## 测试
模块内 unit 只测纯逻辑与装配；接口链路走 `public/test/<mod>/feature`（真实 PG）。