# pkg 约定

## 定位
可被任何 `internal` 包 import 的基础设施与契约包。**pkg 不 import `internal/**`**，不带业务语义。

## 包清单（职责一句）
- 契约三件：`binding`（绑定唯一入口）、`facing`（错误事实 key+兜底+状态码）、`response`（JSON 出口）、`view`（视图结果接口）。
- 基础设施：`i18n` `logger` `database` `cache` `rls` `jsonx` `utils` `money` `pagination` `pathkit` `safepath`。
- 能力包（provider 型，实现放 `<pkg>/provider/`）：`auth` `casbin` `crypto` `captcha` `mailer` `queue` `upload` `validate` `sitehttps` `sitetz`。
- 小工具：`datarule` `reqparams` `backpath` `bulkids` `enums`。

## 硬约束
- JSON 只经 `jsonx`（import 写 `json "go_wp/pkg/jsonx"`）；时间只经 `utils.JSONTime`；日志只经 `logger` 结构化字段。
- 错误文案在 pkg 侧写中文原文或透出 err；业务错误的 key 与兜底由模块 `enums` 给。
- 组件不决定进程退出：`Init()` 只返回 error；配置校验在自己包内完成。
- 新增 pkg 包的准入：三处以上复用、无业务语义、有 unit 测试。
- 改 pkg 的导出签名 = 改全仓契约：先 `rg` 调用方，再改；`facing.Msg` 这类别名不要建第二份。