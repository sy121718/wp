# 重构计划（2026-10-18 定稿）

## 0 一句话

V 之外不出现「给用户看的字符串」，也不出现「字符串的形状」。控制器只有三个动作：获取数据、验证、无脑把业务层给的数据交给封装函数。

## 1 三条硬判据（会变红）

| # | 判据 |
|---|---|
| 1 | 控制器方法签名 `func(c *gin.Context)`；语句 ≤ 6；最后一条语句是一次 `response.*` 或 `view.Render` 调用；禁字段赋值、`gin.H{`、`.Format(`、取词、`switch`、`range` |
| 2 | dto 无展示字段（`Label/LabelKey/LabelFallback/Tone/Text/Title/PageTitle/URL/*Label`），无 `PageStatus()/PageNoStore()` |
| 3 | 模块内（非 router 控制器、service）禁 `i18n.Translate` / `text.Translate*` / `MsgText`，出站内容白名单除外 |
| 4 | 通用 UI 形态（分页、徽标、空态、抽屉、批量条、下拉、日期筛选）只在 `internal/templates/**` 的 partial 或 `static/js/ui/**` 里实现；`pkg/` 不新增给页面用的助手包 |
| 5 | 响应 message 只由 `pkg/response` 出口按请求语言取词一次；业务层交 `facing.Msg`（key + 兜底），控制器不取词 |

## 2 基线（2026-10-18 实测）

| 指标 | 现在 | 目标 |
|---|---|---|
| 控制器生产代码 | 49 519 行 / 486 方法 / 平均 102 行 | 每方法 ≤ 10 行 |
| service 生产代码 | 75 598 行 | 不含任何显示字段 |
| 控制器取词（text.*） | 27 处 / 5 文件 | 0 |
| service 取词（i18n.Translate） | 44 处 / 13 文件 | 只剩出站内容 |
| 控制器拼 message（SuccessWithMessage…text.） | 10 处 | 0 |
| dto 展示字段 | 49 处 | 0 |
| 视图 dto / PageStatus / LoadFailed | 18 / 15 / 119 | 0 |
| 控制器视图赋值 / gin.H | 88 / 277 | 0 |
| 分页派生计算（Facts / Pages / Start / End） | 12 处 | 0（已清零） |
| Go 里 .Format | 49 处（页面显示 30） | 只留出站内容 |
| dto 时间 field：*At string / JSONTime | 64 / 98 | 时间类型 / unix 毫秒 |
| (any, error) handler | 30 | 0 |
| c.PostForm | 367 | 0 |
| *_err.go / *_jump.go | 12 | 0 |
| shell.RenderJump | 56 文件 | 0 |
| 非 router 控制器 import adminui | 45 文件 | 0 |

## 3 阶段 0：先删错（不加任何功能）

每小步一条提交，提交信息写「消掉哪条错」。

| 步 | 做什么 | 判据（红→绿） |
|---|---|---|
| 0.1 | 删 `internal/text` 的 `Translate/TranslateArgs/TranslateFill/MsgText/Internal/InternalMsg`；控制器 27 处改 `return err` / `response.Success`；10 处重复 message 消除 | 模块内 `text.Translate*\|MsgText` = 0 |
| 0.2 | service 44 处取词分档：删 `FacingText` 型（comment / membership / navigation）；出站内容（邮件正文、打包报告）收进显式出口并登记理由 | 模块内 `i18n.Translate` 只剩白名单条目 |
| 0.3 | 删 dto 49 处展示字段 + 18 个视图 dto + 15 个 PageStatus；模板改为自己取词与分档 | dto 无展示字段、无 PageStatus |
| 0.4 | 时间：`JSONTime.MarshalJSON` 输出 unix 毫秒；64 处 `*At string` 改回时间类型；49 处 `.Format` 分档 | dto 时间字段无 string；模块内 `.Format` 只剩白名单 |
| 0.5 ✅ | 分页：派生计算清零（`pagination.Facts` / `Pages` / `Start` / `End` / `HasPagination` 全删）；`pkg/pagination` 只剩 `Page` 事实（Total/Page/Limit/PageParam/DropLimit/Base）、`ClampPage`、`Slice`、`BaseURL`、`Query`；派生与链接在 `internal/templates/pager.go` + partial | `rg "pagination.Facts|HasPagination\(\)" -g "*.go"` = 0 |
| 0.6 | 出口统一：30 个 `(any, error)` 改 `func(c *gin.Context)` + `view.Render` / `response.*`；删 wrapView / failView | 每方法最后一条语句是封装调用 |
| 0.7 | 通用能力归位：`pkg/pagination` 只留 `ClampPage`；`pkg/money` 若只服务显示则删；状态徽标/空态/下拉在 partial 里唯一定义；Jet 侧补 `formatTime` / `money` / `statusBadge` | partial 与 js/ui 为唯一实现；`pkg` 无新增页面助手 |

## 4 阶段 1：product 范例（一个模块走通）

| 步 | 范围 | 验收 |
|---|---|---|
| 1.1 | 列表页（最复杂一屏）：数据根 map + 行 dto + 模板取词/状态分档/空态/分页 | 控制器 3 动作；六个数归零 |
| 1.2 | 详情 / 编辑 / 新建 | 同上 |
| 1.3 | 抽屉与面板片段 | 片段失败 4xx + JSON |
| 1.4 | 失败形态：删 LoadFailed / LoadError / HTTPStatus | 页面 GET 取数失败走 JSON |

验收附加：改动前后 HTML 逐字一致，差异只允许来自时间格式与分页链接（按约定由模板决定）。

## 5 阶段 2：门禁（先红后绿，双向台账）

| 门禁 | 文件 | 判据 |
|---|---|---|
| controller_orchestration_test | internal/architecture | 签名、语句数、最后一句、禁用节点 |
| dto_presentation_test | internal/architecture | 展示字段与 PageStatus |
| module_text_test | internal/architecture | 模块内取词 |
| time_shape_test | internal/architecture | dto 时间类型、`.Format` |
| pagination_shape_test | internal/architecture | inbound/http 无 pagination |
| template_own_text_test | internal/architecture | 每个页面模板 t/tf 调用数 ≥ 阈值（反向判据：模板不取词 = 文案在 Go） |
| generic_ui_owner_test | internal/architecture | `pkg/` 下不得新增页面助手包（白名单：facing / response / binding / jsonx / view）；同一视觉形态在 partials 里只有一处实现 |

规矩：判据先落，让当前代码变红，再逐条修到绿；台账登记处数与文件，双向判定。

## 6 阶段 3：推广（每模块 3 批：列表 → 详情/表单 → 片段与失败）

顺序：product（范例）→ inventory → admin → page → order → mail → ai → navigation → content → membership → sysconfig → analytics。

每批验收：模块内六个数字归零 + feature 测试绿 + 跨文件数下降。

## 7 阶段 4：收尾

| 步 | 做什么 |
|---|---|
| 4.1 | 12 个 `*_err.go` / `*_jump.go` 清零；56 文件 `shell.RenderJump` 清零 |
| 4.2 | 367 处 `c.PostForm` 清零（project 83 / mail 69 / ai 60 / admin 52 / content 33 / order 32 / navigation 24 等） |
| 4.3 | 45 个非 router 控制器去掉 adminui 依赖 |
| 4.4 | 规则更新：时间 unix 毫秒、模块零取词、控制器三动作、二开验收 |
| 4.5 | 删本文件 |

## 8 提交与汇报规矩

- 删错优先于加功能：还有未清的错，不许新增能力。
- 提交信息必须写「本提交消掉哪条错」；写不出就不许提交。
- 门禁先红后绿，不允许先改代码再补判据。
- 不再汇报「测试通过 / 门禁绿 / 行数下降」。只报二开五场景与跨文件数。

## 9 二开五场景（验收口径）

| 场景 | 目标代价 | 度量 |
|---|---|---|
| 改一个显示（状态列文案 / 时间格式 / 徽标） | 只改模板 | git diff --name-only 文件数 = 1 |
| 新增一屏列表 | 模板 + router 一行 + service 一个查询 | 改动文件 ≤ 3 |
| 查一句文案来源 | 模板 key → seed | 不经过 Go |
| 读一屏代码 | 控制器 3 语句、一个 service 函数 | 方法行数 ≤ 10 |
| 找一个字段谁算的 | 唯一赋值点 | AST：同字段赋值点 = 1 |

