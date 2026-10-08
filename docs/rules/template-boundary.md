# 模板职责边界：只拿数据 + 判断渲染哪个 HTML

判据一句话：**模板回答「渲染哪个」，不回答「值是多少」。**

这条不是风格偏好，在本仓库它是**引擎强制的**：Jet 模板里写不了 Go（没有 `{php}` 式代码块，
可调用的函数是启动时注册的闭集 —— 13 个全局函数 + per-request 注入的闭包）。所以
「哪些逻辑能进模板」有硬门槛：**要进，就得先在 Go 侧注册成一个函数**。对比 ThinkPHP 的
`think-template`（`parsePhp` Template.php:439 能把 `{php}` 变成真 PHP、模板里直接 `{:number_format($v,2)}`
调整个 PHP 运行时），那边逻辑溜进模板是自然发生的，只能靠自觉。

## 三条边界

| # | 能做 | 落点 |
|---|---|---|
| 1 | **选择与循环**：`{{if}}` / `{{else}}` / `{{range}}` / `{{include}}` / `{{yield}}` / `{{import}}` / `{{extends}}` / `{{block}}` | `internal/templates/admin/**`（实测用量：`{{if` 2199、`{{range` 425、`{{yield` 139、`{{extends` 81） |
| 2 | **取数据**：handler 交过来的变量、per-request 闭包（`.["t"]` i18n、3349 处） | `internal/shell` 的 `Prepare`（数据袋）；`internal/shell/shell.go:111-146` 的三样权限数据 |
| 3 | **展示格式化**：调用**已注册**的函数（`money`/`date`/`percent`/`fill`…） | `internal/templates/funcs.go`（447 行，13 个全局函数） |

| # | 不能做 | 该怎么办 |
|---|---|---|
| 4 | **新计算**：业务规则、查表、跨实体推导 | 在 Go 侧算好，作为变量传进来 |
| 5 | **新格式化**：模板里没有的金额/日期/单位规则 | 在 `funcs.go` 注册一个函数（注册了才可能写出来） |
| 6 | **多语句过程逻辑**：赋值累加、分支嵌套做数据处理 | handler / service 里做；模板只做「显示哪块」 |
| 7 | **从 `gin.Context` 现场查**：如全局 `can()` 读当前请求权限 | per-request 闭包注入（见下） |

## 两个具体的坑（都在本仓库踩过）

**全局函数不能读请求上下文。** `internal/templates/jet_render.go:48` 的 `injectGlobals(set)`
挂在**进程级** `*jet.Set` 单例上，`:82` `t.Execute(&buf, nil, i.data)` 渲染时没有 ctx。
所以「全局 `can()` 从 gin.Context 实时查权限」在本仓库做不到 —— 它会读到**最后一个请求**的
权限，多用户串号。正确形态是 per-request 注入闭包（`.["t"]` 就是这么做的）。

**模板判显示 ≠ 访问控制。** 模板里 `{{if .Buttons["product.delete"]}}` 决定按钮画不画，
这只是**界面提示**；真闸门是写路由上的 Casbin。菜单隐藏同样不是访问控制 —— 直输 URL 能绕过，
所以页面 GET 也挂了鉴权（见 `docs/rules/response-shapes.md` 第 4 格与
`scripts/check-page-get-authz.sh`）。

## 违反形态（一眼可辨）

```
❌ {{ if .Price > 1000 && .Level == 2 }}          // 业务规则进模板
❌ {{ .Total * .Rate * 0.85 }}                     // 新计算
❌ {{ len(.Items) > 0 && .Items[0].Status == 3 }}  // 跨实体推导
❌ {{ can("product.delete") }}                     // 全局函数读请求上下文（串号）

✅ {{ if .ShowBulkBar }}                            // 结论由 handler 算好
✅ {{ money(.PriceCents, "¥") }}                    // 已注册的展示格式化
✅ {{ if len(n.Children) > 0 }}                     // 结构判断（不是业务规则）
✅ {{ if .Buttons["product.delete"] }}              // 权限位由 Prepare 注入
```

## 与相邻规则的关系

| 规则 | 管什么 |
|---|---|
| `docs/rules/response-shapes.md` | 响应**形态**：整页 HTML / 片段 / 数据回执 / 拒绝 |
| 本文 | 模板**能写什么**：拿数据 + 判断渲染哪个 |
| `internal/middleware/builtin/page_authz.go` | 页面 GET 的判定与拒绝标记（判定不认识 HTML） |

## 语义 vs 外观：颜色是 CSS 的事，模板只给语义

分界线不是「后端 vs 前端」，而是**语义 vs 外观**。四层各给一样东西：

| 层 | 给什么 | 例子 |
|---|---|---|
| 数据 | 原始值 | `deleted_count: 3` |
| **Go** | **语义枚举**（有限集，可单测、可复用） | `Tone = "danger"` |
| **Jet** | 把语义写进 class/属性 + 选渲染哪块 | `<span class="badge badge-{{.Tone}}">` |
| **CSS** | 语义 → 外观（换肤/暗色只改这里） | `.badge-danger { --badge-bg: … }` |

三条禁令（都能机器检查）：

- **模板不产出颜色值**：`style="color:#c00"`、`style="background:rgb(…)"` 越界；
  `style="background: var(--chart-c3)"` 可接受（用的是调色板变量，不是字面量）。
- **模板不写业务阈值**：`class="badge {{if .DeletedCount != 0}}badge-danger{{else}}badge-mute{{end}}"`
  —— 「删除数 > 0 就是危险」是业务规则，写在模板里没法单测，还会在每个列表页各抄一遍。
  正确形态：Go 给 `Tone` 字段，模板 `badge-{{.Tone}}`（零分支、零业务）。
- **模板不写布局数字**：`style="grid-column: {{if canDelete}}8{{else}}7{{end}}"` —— 列跨度改由
  CSS grid 的列定义（或 `colspan` 由 Go 给）决定，否则增删一列要去 N 个模板改数字。

本仓库已有的两个正确先例（Go 算语义、模板只用结果）：
`commentStatusClass`（`internal/module/comment/inbound/http/comment_page.go:340`）、
`sessionEventTone`（`internal/module/ai/inbound/http/ai_page.go:1643`）。

### tone 词表与别名（2026-10-08 收敛完成）

模板表现层的存量违规已清零（`scripts/template-presentation-allow.txt` 现为空清单，22 个门禁全绿）。
收敛后的约定：

- **Go 侧一律出语义档**，取值域固定为 `ok / warn / danger / mute / info`（空串 = 不带档）。
  实现都是「值 → 档」的小纯函数，就近放在数据来源处：`orderenums.OrderStatusTone` /
  `ReturnStatusTone` / `CouponStateTone`、`i18n.ContentEngineTone`（放常量旁边 ——
  否则字符串字面量 `"ai"` 会散进模板，常量值一改页面就静默失色）、`customerStatusTone`、
  `mcpSwitchTone`、`variationTone` 等。
- **模板只写 `badge badge-{{.XxxTone}}`**：不判条件、不拼档位、不比较状态值。
- **`.badge-<tone>` 别名是「语义 → 外观」的唯一映射点**（`internal/templates/static/css/ui.css`）：
  `.badge-ok` / `.badge-warn` 与既有的 `.badge-success` / `.badge-warning` 指向同一组变量。
  换一套视觉（改色、换图标、换成圆点）只改这里，Go 与模板都不动。
- **筛选行把「取值 + 档位」一起给模板**（`orderStatusTabs()` / `returnStatusTabs()`）：
  之前模板 `{{if sv == "pending"}}…{{end}}` 把同一套映射在筛选行、列表行、详情头各写一遍
  （orders.html 三处、returns.html 四处的由来）—— 现在一处在 Go。
- **DTO 上的 tone 一律 `json:"-"`**：分档是「怎么显示」不是数据，接口契约不该因为它变样；
  页面与 JSON 接口共用同一个 DTO 时，出网的那份不带它。

**为什么不全推到前端 JS 算**：①业务码含义泄漏（前端要知道 `status===3` 是逾期）；
②SSR 首屏会闪一下（FOUC）；③禁用 JS 就没了 —— 而后台首屏必须无 JS 可用。
htmx 管的是**取片段/换 DOM**，不是算颜色：它换进来的片段自带语义类，CSS 照常生效。
