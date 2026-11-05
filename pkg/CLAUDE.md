# pkg 约定

## 定位
可被任何 `internal` 包 import 的基础设施与契约包。**pkg 不 import `internal/**`**，不带业务语义。
分层判据、取词规则、响应协议的唯一来源是 [AGENTS.md](../AGENTS.md)。

## 包清单（职责一句）
- 契约：`facing`（错误事实 key+状态码）、`response`（JSON 出口 `MessageOf` 取词产生 message，缺词时给归口文案）、`view`（视图结果、成功/失败出口与请求级渲染接线）、`view/jet`（Jet 引擎、请求作用域、显示函数、缓冲渲染与失败接线）、`view/ui`（公共模板/控件/资产，唯一 UI 实现）、`view/ui/icons`（唯一 SVG 与目录源，Go 与浏览器同源）。
- `binding` 是待清退的旧绑定与辅助实现，不再作为唯一入口；请求绑定使用 Gin 原生能力，具体判据见根规则。未迁调用按域整改，不新增调用或同义薄包装。
- 框架能力：`builder`（编译、组件、样式、富文本、可视化工具资产；业务扩展留 `internal/builder/extensions`）、`publish`（Store/PublicationStore/Manifest/Artifact/Publisher/路径锁）、`middleware`（通用请求与安全执行、路由权限登记）、`mcp`（工具运行机制）、`lifecycle`（初始化/就绪/逆序关闭）、`seo`、`uispec`、`scaffold`、`database/retention`、`database/partition`。
- 基础设施：`i18n` `logger` `database` `cache` `rls` `jsonx` `utils` `money` `pagination` `pathkit` `safepath`。
- 能力包（provider 型，实现放 `<pkg>/provider/`）：`auth` `casbin` `crypto` `captcha` `mailer` `queue` `upload` `validate` `sitehttps` `sitetz`。
- 小工具：`datarule` `reqparams` `backpath` `bulkids` `enums` `actor`（把操作人经 ctx 传到 service，供发布会话 / 发布历史留痕，见 internal/middleware/builtin/actor_context.go）`ssrf`（出站目标的 SSRF 判定，webhook 与 mail 回调节点共用）。

## 硬约束
- JSON 只经 `jsonx`（import 写 `json "go_wp/pkg/jsonx"`）；时间只经 `utils.JSONTime`（JSON 出 unix 毫秒，页面给 `time.Time` 由模板格式化）；日志只经 `logger` 结构化字段。
- 错误文案在 pkg 侧写中文原文或透出 err；业务错误的 key 与状态码由模块 `enums` 给。HTML 失败经 `view` 接线交 Jet 按事实取词，数据 JSON 失败由 `response` 取词；原始内部错误不直接写响应。
- `money` / `pagination` 按**函数职责**区分，不能整包当「纯显示」：
  - 数据库分页工具（service 在用）：`pagination.ClampPage`、`pagination.Slice` —— 夹页与内存切片，属后端分页计算，归 service/model 使用。
  - 模板显示函数（模板/渲染基础设施在用）：`pagination.Window`、`pagination.Derive`、`PageBase`/`QueryURL`，以及 `money.*` 金额显示格式 —— 页码窗口、链接与金额显示，归前端层调用。注：模板侧实际用的是 `internal/templates/funcs.go` 的包装函数，它们调用 `pkg/money`。
  - 金额求和、数据库筛选/count 始终归 service；新增函数按职责落位，不借文档调整迁代码。
- 组件不决定进程退出：`Init()` 只返回 error；配置校验在自己包内完成。
- 新增 pkg 包的准入：三处以上复用、无业务语义、有 unit 测试。
- 改 pkg 的导出签名 = 改全仓契约：先 `rg` 调用方，再改；`facing.Msg` 这类别名不要建第二份。