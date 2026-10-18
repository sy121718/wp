# 模板职责边界：拿事实 + 决定怎么显示

判据一句话：**模板回答「显示成什么样」，不回答「值是多少」。**

事实（业务数据、权限位、动作可行性）由 Go 给；显示成什么样（选哪块、标成什么档、串成什么话）
由模板按事实决定；颜色与形状由 CSS 决定。这条分界不是「后端 vs 前端」，而是**事实 vs 展示**。

这条不是风格偏好，在本仓库它是**引擎强制的**：Jet 模板里写不了 Go（没有 `{php}` 式代码块，
可调用的函数是启动时注册的闭集 —— 13 个全局函数 + per-request 注入的闭包）。所以
「哪些逻辑能进模板」有硬门槛：**要进，就得先在 Go 侧注册成一个函数**。对比 ThinkPHP 的
`think-template`（`parsePhp` Template.php:439 能把 `{php}` 变成真 PHP、模板里直接 `{:number_format($v,2)}`
调整个 PHP 运行时），那边逻辑溜进模板是自然发生的，只能靠自觉。

## 语义 vs 外观：三层各给什么

| 层 | 给什么 | 例子 |
|---|---|---|
| Go | **业务事实与权限事实**：原始值、结论、服务端动作可行性、已解析的枚举 key + 兜底文案 | `Status: 1`、`Actionable: false`、`NextStatus: 1`、`StatusLabelKey: "admin.customers.status.active"` |
| Jet（模板） | **展示判断**：按事实选档 / 选块 / 拼展示串与链接 | `{{if r.Status == 1}}{{statusTone = "ok"}}{{end}}`、`{{dateTime(r.RegisteredAt)}}`、`href="{{detailURL}}"` |
| CSS | **外观**：语义档 → 颜色与形状（换肤、暗色只改这里） | `.badge-ok { --badge-bg: … }`（`static/css/ui.css:850`） |

模板拿不到、也不该现场算的东西（跨表推导、聚合、状态机规则、业务参数）一律归 Go —— 见下面第 4/6 条。

## 三条边界

| # | 能做 | 落点 |
|---|---|---|
| 1 | **选择与循环**：`{{if}}` / `{{else}}` / `{{range}}` / `{{include}}` / `{{yield}}` / `{{import}}` / `{{extends}}` / `{{block}}` | `internal/templates/admin/**`（实测用量 2026-10-08：`{{if` 2194、`{{range` 425、`{{yield` 142、`{{block` 96、`{{include` 84、`{{extends` 81、`{{import` 53） |
| 2 | **取数据**：handler 交过来的变量、per-request 闭包（`.["t"]` i18n，实测 3492 处） | `internal/adminui` 的 `Prepare`（数据袋，task-92 批 2a 自 shell 迁入；order 五个生产文件六处已直引，`internal/shell` 的过渡转发已删除）；`internal/middleware/adminctx` 的 `PermContextMiddleware` 写入的三样上下文数据（task-92 批 3 自 `internal/shell` 迁入，键名声明留在 `internal/shell` 的三个常量） |
| 3 | **展示**：按事实选档 / 选块（第 1 条），调用**已注册**的函数（`money`/`dateTime`/`fill`…）拼展示串 | `internal/templates/funcs.go:45`（`injectGlobals`，18 个全局函数：`money`/`thousands`/`percent`/`dateTime`/`date`/`dateTimeLocal`/`siteDateTime`/`dateTimeSecond`/`fill`/`formValue`/`formChecked`/`joinNonEmpty`/`currencyPrefix`/`percentChange`/`percentChangeDir`/`sprintf`/`queryURL`/`pageNumbers`） |

| # | 不能做 | 该怎么办 |
|---|---|---|
| 4 | **推导业务事实**：跨实体/跨表推导、聚合统计、状态机规则（该迁到哪个状态）、业务参数（费率、时限、阈值常量） | 在 Go 侧算好，作为事实传进来（`Actionable` / `NextStatus` / `HasOrders` 就是这么来的） |
| 5 | **新格式化**：模板里没有的金额/日期/单位规则 | 在 `funcs.go` 注册一个函数（注册了才可能写出来） |
| 6 | **对业务数据做过程逻辑**：赋值累加、分支嵌套处理业务数据 | handler / service 里做。注意：对**展示档**的赋值与分支不算（`{{tone := "mute"}}{{if …}}{{tone = "ok"}}{{end}}` 是合法形态，见下节） |
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
❌ <span style="color:#c00">                            // 模板产出颜色值（颜色是 CSS 的）
❌ style="padding-left:{{8 + depth}}px"                 // 布局算术（网格与间距是 CSS 的）
❌ {{tone := "critical"}}                               // 自造档：ui.css 没有 .badge-critical，静默失色
❌ {{ if .Total * .Rate > 1000 }}                       // 推导业务事实：费率、阈值都在模板里
❌ {{ if len(.Orders) > 0 && .Orders[0].Paid }}         // 跨实体推导
❌ {{ can("product.delete") }}                          // 全局函数读请求上下文（串号）

✅ {{tone := "mute"}}{{if r.Status == 1}}{{tone = "ok"}}…badge-{{tone}}   // 按事实（状态值）选档
✅ <span class="badge badge-{{.StatusTone}}">           // 直出 Go 给的档
✅ {{ if .Actionable }}                                  // 动作可行性由 Go 给（权限事实）
✅ {{ money(.PriceCents, "¥") }}                        // 已注册的展示格式化
✅ {{ if len(n.Children) > 0 }}                         // 结构判断（数结点，不是业务规则）
✅ {{ if .Buttons["product.delete"] }}                  // 权限位由 Prepare 注入
```

## 与相邻规则的关系

| 规则 | 管什么 |
|---|---|
| `docs/rules/response-shapes.md` | 响应**形态**：整页 HTML / 片段 / 数据回执 / 拒绝 |
| 本文 | 模板**能写什么**：拿事实 + 决定怎么显示 |
| `scripts/check-template-presentation.sh` | 本文的机器检查（4 条判据，见下） |
| `internal/templates/css_class_audit_test.go` | 类名对账：动态前缀的取值域必须登记在 `dynamicClassEvidence` |
| `internal/middleware/builtin/page_authz.go` | 页面 GET 的判定与拒绝标记（判定不认识 HTML） |

## 整页 GET 的失败出口：失败也必须是「这一页」

判据一句话：**整页 GET 由浏览器发起导航，所以它的每一种结论 —— 读到、读不到、参数错、不存在 —— 都必须是同一个页面的完整 HTML。** 回 JSON、回裸文本、回半截渲染，用户拿到的都是一个「坏掉的页面」：没有标题栏与左侧菜单，地址栏还停在原处，只能手改地址栏回去。

| 结论 | 形态 | 状态码 | 缓存 |
|---|---|---|---|
| 读到主数据 | 同页完整 HTML | 200 | 常规（允许 htmx 历史缓存 / 后退） |
| **主数据没读到**（取数失败） | 同页完整 HTML + 失败槽 | **500** | **`Cache-Control: no-store`** |
| 参数非法（整页 GET 的参数决定这一页是什么） | 同页完整 HTML + 参数错误槽 | **400** | `no-store` |
| 资源不存在（id / 路径指向的行不在） | 同页完整 HTML + 不存在槽 | **404** | `no-store` |
| 主数据读到、**可选辅助**没读到 | 同页完整 HTML，辅助位标出错误 | 200 | 常规 |

- **500 而不是 200**：200 + 一句「加载失败」在监控里与正常页一模一样，失败会一直无声地发生；失败页也不该被缓存，否则「重试」只是把同一份失败从缓存里再拿回来。
- **主数据 vs 可选辅助**必须分开：页面赖以成立的数据（列表、编辑对象、工程上下文）读不到 = 主数据失败 = 500；「有它更好」的候选数据（可选工程下拉、面板块候选、引用面提示）读不到 = 可选辅助 = 200 + 就地标出。

样板（本仓库唯一完整落地）：`internal/module/navigation/inbound/http/navigation_page_handle.go:290-317` —— 原文只进日志（`:310`）、零值数据 + `LoadError`（`:311-312`）、`no-store`（`:313`）、`c.HTML(500, 同一个模板, adminui.Prepare(c, failed))`（`:314`）。回归钉死四条（`internal/module/navigation/inbound/http/navigation_page_presentation_test.go:213-300`）：`Content-Type: text/html`；500 + `no-store`；页壳完整（`<!DOCTYPE` / `class="admin-layout"` / `</body>` / `</html>` / `id="adminRail"`）；**内部原文只进日志**（用例刻意用 `pq: relation "navigations" does not exist` 反证）。

### 失败槽：一个事实位 + 一句归口文字

| 槽 | 类型 | 语义 |
|---|---|---|
| `LoadFailed` | `bool` | 本次请求**没读到**（不是「读到了、只是空的」） |
| `LoadError` | `string` | 非空即「这一页失败了」；内容是**归口后的当前语言文字** |

- **失败优先于空态**：模板先判失败、再判空态 —— `{{if loadError}} … {{else if len(.Projects) == 0}}`（`internal/templates/admin/navigation/navigations.html:23-31`）。两档混在一起，运营会以为数据被删了，然后去重建一遍。
- `LoadFailed` 是既有形态（`masterdata_page.go:242-245`、`membership_page.go:264-268`、`inventory_page.go:1801-1804`、`order_create_page.go:292-293`；模板样板 `internal/templates/admin/membership/membership.html:46` 的 `{{if .LoadFailed}}` + `role="alert"`）—— 上面这几处按各自模块注释仍属**降级 200**，对应上表最后一行（可选辅助：工程上下文缺省，但页面主体照读）。
- **判据不是「用了哪个键」，而是这一支在失败时是否已经把页面赖以成立的主数据读完**。反例（同一个键、相反的档）：`internal/module/contenttemplate/inbound/http/contenttemplate_page.go:75-111` 的工程列表失败 —— 那一支在 `Impact` / `templates.List` 之前就 `return`，**列表主体根本没读**，因此是**主数据失败 = 500 + no-store**（`:109-110`）。它早期被错记为「可选辅助降级 200」的示例，2026-10 按短路证据纠正；任何模块出现「同样在列表查询之前 return」的同类分支，都按这条改判为 500。回归钉死 `internal/module/contenttemplate/inbound/http/content_template_page_err_test.go` 的 `TestContentTemplatesPageProjectListFailureRenders500`：500 + `text/html` + `no-store` + 页壳完整 + 筛选 `entityType` 回显 + 无行级入口/批量动作 + 引用面/清单/样例实体查询计数全 0 + 原文只进日志。
- `LoadError` 是归口文字槽：事实由 Go 给（`internal/module/navigation/inbound/http/navigation_error_handle.go` 的 `navigationErrMsg` → `adminui.Msg`），缺词落 `enums.errFallbacks` 的中文兜底；**取词发生在渲染期**，模板只把它摆出来，不拼句。判据是 `pkg/facing.FactOf`，模块不再自带白名单。

### 整页 GET 的失败出口不许用这四种

| ❌ | 为什么 |
|---|---|
| `response.ErrorWithMessage`（含 `adminui.PageErrorInternal` / `adminui.PageErrorBadRequest`） | 浏览器收到的是 JSON：没有页壳、没有菜单。`internal/adminui/errors.go:66` / `:46` 分别是 **500 / 400 JSON 出口**，不是整页渲染 |
| `c.String(500, text)` / `c.String(400, …)` | 页面被整页换成一段裸文本（文字还是 i18n key 时更糟）。`admin_err.go:139-144` 记过六个管理页的这种症状 |
| 只 `c.Status(400)` 不给 body | 那是**片段**端点的约定（`admin_page.go:118-133`、`block_page.go:392-412`：drawer.js 对非 200 显示「加载失败」并保留原界面）；整页 GET 用它 = 白屏 |
| 半截模板（渲染中途 return） | 只看 `<!DOCTYPE` 会漏掉「渲染中断、尾部截断」——所以回归断言必须同时查 `</body>` / `</html>` |

### 必需的 layout 键（少一个就整页 500）

`layout.html` 取不到字段时 Jet **直接抛错**（不是「空着」）。整页渲染必须给齐：

- `title` —— 由每个页面 handler 自己放进数据，`adminui.Prepare` **不注入**；门禁 `scripts/check-page-title.sh` 按形状扫（整页模板集合 = 含 `extends "../layout.html"` 的模板；数据源没有 `"title"` 键即命中），豁免清单 `scripts/page-title-allow.txt`。
- `menu` —— 侧栏哪一项高亮。
- `adminui.Prepare`（`internal/adminui/prepare.go`，task-92 批 2a 自 `internal/shell` 迁入）统一注入：`t`（per-request 取词闭包）、`csrf_token`、`PermSet`、`Buttons`、`NavGroups`、`SidebarOpen` / `SidebarPinned` / `HasSubnav`。**侧栏视图构造与装配同包**：`internal/adminui.BuildNav` 把授权菜单树转成侧栏视图（task-72 起），`Prepare` 只做取值注入与当前页标记 —— 改侧栏视图形状要改 `internal/adminui/nav.go`，改注入键要改 `internal/adminui/prepare.go`。（order 五个生产文件六处调用已直引 `adminui.Prepare`；`internal/shell` 的 `Prepare` 过渡转发与 parity 测试同批删除，永久 AST 守卫钉住导出与调用点归零。）
- **失败路径走同一份注入**：`adminui.Prepare(c, failed)`（`navigation_page_handle.go:314`）。漏了 `menu` / `NavGroups` 的「失败页」不是失败页，是又一个坏页面。

### 文案与语言的边界

- **业务文案由 Go 按系统语言解析**（`Accept-Language` → `shell.TranslateFor` / `pkg/response.RequestLanguage`），缺词落**受控 fallback**（模块内中文原文；连兜底都没有时给 `ErrInternal` 归口句）。**页面上不许出现裸 key**。
- **UI 取词仍归模板链**（`.["t"](key, 中文原文)`）：失败槽给的是「归口后的业务结论」，页壳上的按钮、标题、空态标题仍由模板取（`docs/rules/i18n.md` §五）。两条链的缺词行为各自明确（task-14 复审 P2 的口径，见 `navigation_error_handle.go` 文件头）。
- **JSON API 不受这里影响**：`/api/**` 的形态仍是 `pkg/response` 的 `{code,message,data}`（`docs/rules/response-shapes.md`）。页面出口改成「整页 HTML」时，同一个业务错误的 JSON 出口一行不动。

### 存量差异（登记，不在本契约的落地范围内）

- **整页 GET 的 400 / 404 完整页**：已落地的只有工作台（独立外壳，见下一条）与 navigation 的 500 样板；admin 布局下其余页面 GET 的参数非法 / 资源不存在分支大多还没走到「同页完整 HTML」这一档 —— 这一格仍是**规范先行**，按上表逐页改造。
- `internal/module/page/inbound/http/pages_handle.go`（重定向管理页）的「同页 + 500」已补齐 `no-store`（2026-10-08），以 navigation 样板为准的其余页面 500 出口照此对齐。
- ✅ **工作台（`internal/templates/workbench/layout.html`，独立外壳、不走 admin 布局）已收口**（2026-10-08）：`Workbench` 页面模式与 `workbenchBlock` / `workbenchTemplate` / `workbenchInstance` 三个模式共 24 处裸文本出口，全部改成**完整错误页**（`internal/templates/workbench/error.html`，只读 `lang` / `t` / `title` / `message` / `backPath`）；状态码保留事实区分（400 请求缺参 / 404 目标不存在 / 422 业务状态「这个实例还没有可编辑的文档」/ 500 未知内部异常 / 503 能力未装配），失败页一律 `Cache-Control: no-store`，原始异常只进结构化日志。它剩下的 `c.String` 全在**片段与预览端点**上（`workbench_err.go:146` 的既定设计：iframe 内嵌文档，不是整页外壳），shell 迁移基线登记为 `keep`（`scripts/shell-migration-allow.txt` 的 `exit:workbench:cstring`，49 → 25）。

## 语义 vs 外观：档位怎么表达（Go 出档，或模板按事实选档）

档位（tone）是「这条事实显示成什么颜色」的判断，两种合法形态并存：

1. **Go 出档** —— 跨页面复用、需要单测的映射放在 Go：`orderenums.OrderStatusTone`、
   `ReturnStatusTone`、`CouponStateTone`、`i18n.ContentEngineTone`（放常量旁边 ——
   否则字符串字面量 `"ai"` 会散进模板，常量值一改页面就静默失色）、`sessionEventTone`
   （`internal/module/ai/inbound/http/ai_page.go:1643`）、`commentStatusClass`
   （`internal/module/comment/inbound/http/comment_page.go:340`）。
2. **模板按事实选档** —— 就近的、单页面的展示判断留在模板：

```
{{tabTone := "mute"}}
{{if tab.Key == "active"}}{{tabTone = "ok"}}{{else if tab.Key == "disabled"}}{{tabTone = "danger"}}…{{end}}
<a class="badge badge-{{tabTone}}" href="{{tab.URL}}">…</a>
```

   （`internal/templates/admin/user/customers.html:81-83`；同页 `verifyTone`（:251-252：按
   `r.EmailVerified` 取 `warn`/`ok`）、`statusTone`（:258-260：按状态值取 `mute/ok/danger/warn`），
   `admin/user/customer_detail.html:57/:60`、`admin/user/customer_rfm.html:43`。）

两种形态的共同约束：

- **档只能取词表 `ok / warn / danger / mute / info`**（空串 = 不带档）。`.badge-<档>` 别名只在
  `static/css/ui.css:840-851` 定义了这些（`.badge-ok`/`.badge-warn` 与既有的
  `.badge-success`/`.badge-warning` 指向同一组变量）。**自造档不报错，只会静默失色** ——
  所以它由门禁判据 4 和 `TestDynamicClassToneValuesInVocabulary` 双向钉住。
- **一张映射只写一处**：同一映射在同一页面重复三遍就该收敛 —— 抽成模板 `{{block}}`
  （例：`internal/templates/admin/block/block_row.html` 把「枚举 → 文案、计数 → 提示、时刻 → 串」
  收在一个片段里）或回到 Go 出档。
- **业务码比较留在模板的代价**：`r.Status == 1` 这类字面量出现在模板里，前端要改档位就得改模板。
  可接受的原因是它就近、可单测（渲染测试直打 handler，断言 HTML 里的档），
  而跨页面复用的映射仍归 Go（第 1 种形态）。

三条禁令（都能机器检查）：

- **模板不产出颜色值**：`style="color:#c00"`、`style="background:rgb(…)` 越界；
  `style="background: var(--chart-c3)"` 可接受（用的是调色板变量，不是字面量）。
- **模板不写布局算术**：`style="grid-column: {{if canDelete}}8{{else}}7{{end}}"`、
  `style="padding-left:{{8 + depth}}px"` —— 列跨度与间距改由 CSS grid / 类名决定，
  否则增删一列要去 N 个模板改数字。
- **模板不发明业务事实**：模板只按 Go 给的事实选展示。跨实体推导、聚合、业务参数
  （费率、时限、阈值）都不要在模板里出现 —— 那类判断没法单测，还会在每个列表页各抄一遍。

## 筛选行：取值与 URL 是事实，档位可就近选

筛选行的 tab 是「计数 + 当前选中 + 跳转 URL」——计数与 URL（保留其它筛选条件）是 Go 的事实，
档位可以两种形态任一：

- Go 连档一起给（`orderStatusTabs()` / `returnStatusTabs()` 的 `tab.Tone`）；
- 模板按 tab 键选（user 的客户列表 / RFM 分段，`admin/user/customers.html:81-83`、
  `admin/user/customer_rfm.html:43`）。

已经由 Go 出档的不必回迁，就近选档的也不必上移 —— 判据是「这张映射会不会被第二处用」。
URL 仍由 Go 拼（模板不拼查询串：保留哪些筛选条件是请求上下文相关的事实）。

## 渲染出口：控制器只交视图，摊平与失败档交给基建

整页 / 片段不再在控制器里手工拼 `gin.H`、也不再由控制器编排「哪个失败点用哪个状态码」。
渲染入口都在 `internal/adminui/`（本仓实测，样板见 `internal/module/product/dto/product_page_resp.go`）：

| 入口 | 用途 |
|---|---|
| `Prepare(c, data)` | 存量：data 已是 `gin.H` 的整页 |
| `PrepareView(c, view any)` / `ViewMap(view any)` | 结构体视图 → **一层键**（键名 = 字段名；`view:"title"` 改键名；未导出字段跳过；非结构体 panic） |
| `RenderPage(c, tpl, view)` / `RenderFragment(c, tpl, view)` | 视图实现 `PageResult{ PageStatus() int; PageNoStore() bool }` 时**由视图决定状态码与 no-store**；不实现则 200 + 可缓存 |
| `FillForm(view, values)` | 表单回显：字段名默认首字母小写，含连续大写（`ProjectID`）必须写 `form:"projectId"` 标签 |

- **失败档跟着视图走**：`LoadFailed` / `LoadError` 与「500 + `no-store`」同属这份视图，控制器不再持有
  `loadErr` / `httpStatus` 这对局部变量（早期形态是「每个失败点各写一遍失败页」，实测 6 行 × 18 处）。
- **键名保真**：layout 用 `{{.title}}` / `{{.menu}}` 这类**小写**键，视图字段就写 `view:"title"` ——
  摊平只做「字段名 → 键」，不做大小写改写。
- **视图字段名就是模板契约**：改名等于改模板，同批改。
- 视图形状（`XxxView` / `XxxRow` / `XxxItem`）放模块 `dto/`，不放 `inbound/http`；
  门禁 `internal/module/page/inbound/http/page_view_boundary_test.go` 的 `TestViewShapesLiveInDTO`
  （清单只减不增，两个方向都失败）。

## 门禁

`scripts/check-template-presentation.sh`（4 条判据，存量按文件登记在
`scripts/template-presentation-allow.txt`，现为空；新增违规不豁免）：

1. `style="` 里出现颜色字面量（`#…` / `rgb(` / `rgba(` / `hsl(`）；
2. `style="` 里出现布局算术（`{{…}}` 参与 `+ * ?` 或 `calc()`）；
3. **写死调色板类名**：`badge-red`、`text-gray-400`、`bg-blue-50`、`border-slate-200` 这类
   「语义 + 色号」的类名 —— 颜色必须走语义档，否则换肤时漏改；
4. **档位越表**：模板局部变量（`badge-{{tone}}` 形态）被赋成词表外的档。

第 3、4 条是 2026-10-08 新增；旧的判据 2（只认 `{{if}}` 后紧跟 `badge-danger` 这一种直出写法）
已删除 —— 它把「模板按事实选档」误报为违规，而那种写法正是现在认可的形态。

`internal/templates/css_class_audit_test.go` 守另一头：`dynamicClassEvidence["badge-"]` 登记
拼接点（file:line + 表达式）与取值域，`TestDynamicClassEvidenceIsCurrent` 保证登记与代码同步，
`TestDynamicClassToneValuesInVocabulary` 保证模板自己选出来的档也在词表内。

## DTO 上的 tone

**DTO 上的 tone 一律 `json:"-"`**：分档是「怎么显示」不是数据，接口契约不该因为它变样；
页面与 JSON 接口共用同一个 DTO 时，出网的那份不带它。同理，展示用的时间串（`*Text`）如果
接口另有消费者就保留在 DTO 里，但**页面不再用它** —— 页面直接用 `*utils.JSONTime` +
模板 `dateTime()`，展示口径只有一处。

## 为什么不全推到前端 JS 算

①业务码含义泄漏（前端要知道 `status===3` 是逾期）；②SSR 首屏会闪一下（FOUC）；
③禁用 JS 就没了 —— 而后台首屏必须无 JS 可用。
htmx 管的是**取片段/换 DOM**，不是算颜色：它换进来的片段自带语义类，CSS 照常生效。

## 变更记录

- **2026-10**：**纠正 `LoadFailed` 的档位判据** —— 内容模板页的工程列表失败（`internal/module/contenttemplate/inbound/http/contenttemplate_page.go:75-111`）
  原被当作「可选辅助降级 200」的示例，但该支在 `Impact` / `templates.List` 之前就 `return`（**列表主体根本没读**），
  所属筛选/写动作的工程上下文也随之缺失 ⇒ 改判为**主数据失败：同页完整 HTML + 500 + `no-store`**。
  §失败槽的示例列表随之更正，并把「这一支失败时是否已读完页面赖以成立的主数据」立为判据（不再看用了哪个键）；
  回归钉死 `TestContentTemplatesPageProjectListFailureRenders500`（500 / `no-store` / 页壳完整 / 筛选回显 /
  无行级入口与批量动作 / 引用面·清单·样例实体查询计数全 0 / 原文只进日志）。
- **2026-10-08**：**工作台整页失败出口收口** —— `Workbench` 页面模式（6 处）与块 / 模板 / 实例三模式（18 处）的裸 `c.String` 出口全部改为**完整错误页** `internal/templates/workbench/error.html`（独立外壳，不复用 `workbench/layout`：那个布局用点号取十几个编辑器专用键，错误请求下这些键都不存在，复用等于把「打不开」换成又一个渲染期 500）。新增渲染出口 `renderWorkbenchPageError` / `renderWorkbenchPageErrorText`（后者供两处：携带 `{type}` 占位符的 400 —— 替换必须在取词**之后**做，否则会拼出半句外语；走 facing 白名单的 422 —— 那是业务状态，不在短句表里），空文案一律回落短句表；`workbenchInstance` 三处被 `_` 丢弃的 marshal 错误补成 500 分支；`page.RedirectPage` 的 500 补 `no-store`。存量差异一节随之更新（workbench 从「冲突待裁定」改为「已收口」）。
- **2026-10-08**：新增「整页 GET 的失败出口」一节 —— 主数据取数失败 = **同页完整 HTML + 500 + `no-store`**、
  参数非法 400、资源不存在 404、主数据成而可选辅助失败可 200；失败槽 `LoadFailed`（事实位）+
  `LoadError`（归口文字），**失败优先于空态**；必需 layout 键（`title` 由页面自备，其余由 `adminui.Prepare` 注入，
  失败路径同样 `adminui.Prepare(c, failed)`）；业务文案按系统语言由 Go 归口、缺词走受控 fallback，UI 取词仍归模板链；
  JSON API 不受页面出口变更影响。同时登记三处存量差异（整页 400/404 完整页尚未实现、`page.RedirectPage` 缺 `no-store`、
  workbench 页面 GET 的 `c.String(400/404)` 与本契约冲突待裁定）。
- **2026-10-08**：分界从「档位只能由 Go 出」放宽为「Go 给事实、模板按事实选档」——
  tone 只是展示判断，模板（服务端渲染）承担它不会造成 FOUC、也不依赖 JS；
  Go 侧则继续承担业务事实与权限事实（`Actionable`/`NextStatus` 等）。
  随口径调整：门禁判据 2 删除，新增判据 3（写死调色板类名）与判据 4（档位越表）。
- **2026-10-11**：新增「渲染出口」一节 —— 整页 / 片段的摊平与失败档收进 `internal/adminui` 的
  `PrepareView` / `ViewMap` / `RenderPage` / `RenderFragment` / `FillForm`（视图实现 `PageResult` 决定状态码与
  `no-store`），控制器只剩「读参数 → 边界校验 → 调 service → 渲染」；视图形状改放模块 `dto/`，
  配只减不增的门禁 `TestViewShapesLiveInDTO`。
