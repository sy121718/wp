# 后台菜单码、页面鉴权与路由归属（方案）

> 状态：**第一刀全部落地（2026-10-07）**。已完成：删 `/api/admin/routes`、高亮改「按路径段
> 最长前缀归属」并删掉 `navPathAlias`、页面翻译工作台改名 `/admin/pages/translations`、
> 路由快照更新、三条死链菜单清空 path + 新门禁、7 条权限点漂移清零（含 4 处补声明）、
> **按钮码落地**（模板层不再出现权限码）。
>
> 本文**取代** `02-Y-permission-source-of-truth.md` 第 6 节的「不给页面加 Casbin」——那条结论是错的，
> 实测与代码都相反（见 §2.1）。02-Y 关于「鉴权只看路径、权限码只是标签」的方向仍然有效，
> 本文只补「页面这一侧」和「菜单 ↔ 路由 ↔ 权限」三者的连接方式。
>
> **本文与 `02-X-route-assembly-unification.md` 是同一个目标的两半**（见 §1.1）：
> 02-X 管「注册落点与命名」，本文管「注册内容一次声明」。02-X 先做，本文才好做。

## 1. 触发

一次关于三件事的讨论：

1. 页面 GET 到底走不走 Casbin；
2. 菜单与权限怎么连接、前台菜单能不能带权限短码；
3. 后台 URL 能不能改（二进制部署下运营改不了代码）。

但真正要解决的问题不是权限，而是**控制器与代码链路的精简** —— 和 order 模块那一轮
（HTTP 层 5088 → 3564 行）是同一类工作：把「Go 替模板做的事」和「每个模块重复声明的事」删掉。
权限只是其中一个判据。

### 1.1 与 02-X 的关系（同一个目标的两半）

| 文档 | 管什么 | 状态 |
|---|---|---|
| `02-X-route-assembly-unification.md` | 路由注册的**落点与命名**：注册只出现在 `*_router.go`；一个模块最多两个入口（`SetupXxxRoutes` / `SetupXxxPages`）；参数只收契约；门禁 `check-route-registration-placement.sh` 防回流 | 方案，未做 |
| 本文（02-Z） | 路由注册的**内容**：一次注册同时声明 `path` / 菜单码 / 权限点 / handler，删掉三处重复声明与派生表 | 方案，未做 |

两者是叠加关系，不是替代：02-X 先把「注册写在哪」收敛到一处（否则本文要在 20 个散落位置改），
本文再把「一次注册要写几样东西」收敛到一样。

**一处重叠**：02-X §7 把「模板名绑定上提到路由」列为单独立项（要改 100 个 `c.HTML` 调用点的形态）。
本文的 handler 注册表（`code → handler`）正是它的落地形态 —— 该项可以并入本文，不必单开。

### 1.2 精简账（本文要删掉的重复，实测）

| 重复的东西 | 实测处数 |
|---|---|
| `builtin.CasbinMiddlewareForPath(...)` 调用点（页面路由的鉴权声明，手写） | **283 处** |
| `/admin` 页面路由注册（每条都要手写 path + 权限 obj + handler） | **308 条**（92 GET + 216 POST） |
| 模板里的 `isset(.PermSet["权限码"])`（38 个模板文件） | **144 处** |
| `navPathAlias` 手写的子页面归属 | **11 条** |
| 各模块自写的权限集合读取 helper（如 `ai_page.go` 的 `sessionCan`） | 每模块一份 |

其中最集中的几个文件：`admin_pages_router.go` 47 处、`product_router.go` 44 处、`mail_router.go` 26 处、
`ai_router.go` 24 处、`order_router.go` 20 处、`inventory_router.go` 20 处、`page_router.go` 18 处。

同一条页面路由今天要在**三处**各写一遍（注册行 + `CasbinMiddlewareForPath` 参数 + 菜单 seed 的 path），
三处漂移的结果就是本文 §2.1 与 §2.4 那些缺陷。收敛成一处声明，这些缺陷从设计上消失。

## 2. 事实基线（实测，2026-10-07）

### 2.1 页面 GET 的鉴权现状

`/admin` 组**组级**只挂会话、CSRF 与权限上下文，没有 Casbin：

```go
// internal/routers/assembly.go:491
a.adminPages = router.Group("/admin",
    builtin.SessionAuthMiddleware(), builtin.CSRFMiddleware(),
    shell.PermContextMiddleware(adminAuthzSvc))
```

页面 GET 的鉴权是**逐路由**挂的（`builtin.CasbinMiddlewareForPath("/api/xxx")`，借对应业务 API 的权限点，
因为页面路径本身不在权限点表里）。静态扫页面组 GET 注册：

| 项 | 实测 |
|---|---|
| 页面组 GET 注册 | 98 条 |
| 已挂 Casbin | 28 条 |
| 未挂 | 70 条（其中 7 条实为 `/api/admin/*` 与 `/api/captcha`，脚本启发式误收）→ **真正未挂约 63 条** |

未挂的覆盖 analytics、content/articles、block、comment、contenttemplate、order/orders·coupons·returns、
user/customers\*、inventory/\*、mail/\*、masterdata、media、membership、navigation、page/pages·site-slots、
plugin、product/\*、admin/lang 等。表现是**任何登录账号直接输 URL 就能进**。

`internal/module/admin/inbound/http/admin_pages_router.go` 的文件头把「只读页也要鉴权」的理由写得很清楚：
「菜单按权限渲染，但**菜单隐藏不是访问控制**——直接输入 URL 就能绕过」，并记了一次真实事故
（`/admin/administrators` 曾把全部管理员的用户名 / 姓名 / 邮箱 / 手机号露给任何登录账号）。

**这块没有门禁**：`scripts/check-page-endpoint-authz.py` 只查 POST（脚本第 19 行明说「GET 不构成越权写面」），
`scripts/check-permission-gaps.sh` 只比 `/api` 路由与权限点。两者都不红，页面 GET 仍然可以裸奔。

### 2.2 菜单与权限的连接

| 项 | 实测 |
|---|---|
| `sys_menus` 行 | 134（9 目录 type=1 / 57 菜单 type=2 / 68 按钮 type=3） |
| 连接点 | `sys_menu_permission(menu_id, permission_code)`（迁移 470） |
| `sys_menus.permission_code` | 迁移 470 起降级为「seed 兼容写入点」，读路径只读新表 |
| 可见性判据 | `matchedMenuCodes`：菜单挂多个码时**任一命中即显示**（`admin/service/menu.go:658`） |
| 管理侧 | `MenuTree` 只按 status / type / search 过滤，**不按权限过滤**（`menu.go:528`）→ 管理面全部可视 |

一个权限码可以对应多个菜单：`user:customer_list` 被 4 条菜单绑（客户列表 / 客户概览 / RFM / 群组留存）。

### 2.3 前台菜单渲染不带权限短码

侧栏视图类型里只有菜单身份，一个权限码都没有（`internal/web/shell/nav.go:24` 与 `:40`）；
`BuildNav` / `buildNavNodes` 不向下传 `MenuTreeNode.PermissionCodes`；模板
`admin/partials/sidebar.html` 与 `nav-nodes.html` 只读 Title / Path / Children / Active / Open。

模板里有 144 处 `isset(.PermSet["权限码"])`（38 个模板文件），但**只作条件求值，从不输出**——
全仓没有 `data-perm` 之类把码写进 HTML 的写法。Jet 是服务端模板，`.jet` 源文件不下发。

### 2.4 违规与漂移

| 项 | 实测 |
|---|---|
| `GET /api/admin/routes` | 下发 `permission_codes` + `roles` + 每个菜单的 `meta.auths`（都是权限码），**仓内零调用方**（Vue SPA 时代残留） |
| 配置面出现权限码 | 4 处：`system/menus.html:110`、`system/menu_perm_field.html:34`、`system/role_permissions.html:101`、`system/permissions.html`；**均有权限门**（`menu:list` / `menu:update` / `role:menu_list` / `permission:list`），不构成泄露 |
| 菜单 path 死链 | 3 条：`/project`、`/artifact`、`/publication`（都是 `is_hidden=1`，所以未暴露；实测 GET 全部 404） |
| 菜单 path 语义错位 | 菜单 95「重定向管理」path = `/api/page/redirect`（页面确实挂在 `/api` 前缀下，见 `page/inbound/http/pages_handle.go:24` 的取舍说明） |
| 菜单码与页面码不一致 | 菜单 151「大模型管理」绑 `ai:provider_list` + `ai:session_list`（任一命中即显示），页面只 enforce `ai:session_list` → 只被授前者的账号**菜单可见、点进去 403** |
| 硬编码子页面归属 | `internal/web/shell/nav.go:143` 的 `navPathAlias`，11 条子页面 → 父菜单映射（高亮用） |

### 2.5 按钮行的身份现状（关键）

`sys_menus` 里按钮行（type=3）**除权限码绑定外全是空的**：

| 类型 | 行数 | 有 title_key | 有 path | 有 component | 有 permission_code |
|---|---|---|---|---|---|
| 目录 | 9 | 0 | 0 | 0 | 0 |
| 菜单 | 57 | 4 | 57 | 30 | 56 |
| 按钮 | 68 | **0** | **0** | **0** | 68 |

所以「按钮能显隐、但模板不写权限码」不是「挑一个现成字段用」的问题——按钮**没有菜单侧身份**，
必须补一个 key；好在三列在按钮行上全空，不需要新增列。

## 3. 目标（本轮定的口径）

1. **菜单 = 权限的外部展示**：管理侧（菜单管理页）全部可视；**权限 = 内部校验**（Casbin 按路径 enforce）；
   **连接点 = 权限短码**（`sys_menu_permission`）。
2. **前台菜单渲染不带权限短码**，只带菜单身份。
3. **按钮必须能显隐**，但模板里不写权限码（直接写权限码会让「菜单」和「权限」两个概念在模板层混起来）。
4. **页面 GET 必须鉴权**，且 enforce 的码要与该页菜单绑的码一致。
5. **后台 URL 可改**（数据），表结构不净增、最好能减。

## 4. 方案

### 4.1 菜单码：一列，`title_key` 改名 `code`

一列承担三件事，菜单行与按钮行共用同一命名空间（它们都是 `sys_menus` 的行）：

- **身份**：按钮行 → 模板判显隐；菜单行 → 服务端查页面实现（仅在「URL 可改」下需要）。
- **文案**：它本来就是 i18n key，模板取词用。缺词条时 `Translate` 回落到模板里的中文兜底
  （`pkg/i18n/translate.go:33`），不会变空。
- **层级**：不归它管，`parent_id` 已有。

改名是一次 rename，**不是新增列**，schema 净零；列注释写明「菜单码：身份与文案 key 同源」。

代价（要认）：改这个 key 等于改身份。所以要靠门禁把「身份丢了」挡在启动期 / CI（见 §5），
不能等上线后某个按钮不见了才发现。

**否决的备选**：复用已退役的 `component`（语义更贴、与文案解耦，但迁移 587 刚把它退役，
复活要改 28 个 seed 的注释语义）。

### 4.2 URL 与路由归属

`sys_menus.path` 保持「就是浏览器 URL」，**URL 与页面实现之间只有 `code` 这一个 join**，不再有别的间接层：

- **路由从菜单表注册**：装配期读 type=2、status=1 的行，按 `path` 注册路由，handler 由 `code`
  查代码侧的注册表（`code → handler`）。改 URL = 改一行数据 + 重启。
- **前端不拼 URL**：侧栏 href 由服务端渲染时填好，点击是普通 GET（服务端渲染的多页导航）。
- **URL 必须是输入不是输出**：不做「点击发 code、服务端渲染、成功后拼 URL」那种链路——
  它会让中键新标签打开、刷新、前进后退、无 JS、收藏、告警链接全部失效，并让 403/404/500
  从「服务端渲染的整页错误」退化成客户端提示（htmx 对 5xx 默认不 swap，表现是「点了没反应」）。
- **不做客户端路由**：真 SPA 要把菜单树与路由表下发到浏览器（就是本文要删的 `/api/admin/routes` 形态），
  且 92 个页面全是 Jet SSR + HTMX 片段，等于全部重写。

**为什么必须有「页面 key」**：URL 归了数据、handler 仍在代码，两者要接上就需要一个 join。
不能复用权限码（一个码对多个页面，实测 `user:customer_list` 被 4 条菜单绑），也不能用菜单 id
（由种子顺序决定、二开不可读、跨环境不稳）。`code` 是最省的 join，因为二开写文案时本来就要写它。

**代价**：路由表变成数据驱动，`routes.snapshot` 的语义从「代码产生路由表」变成「代码 + 菜单数据产生」，
CI 里靠确定性种子保证稳定。要免重启就得上 catch-all 分发器，代价是那三个门禁改读表——**本方案不做**。

### 4.3 页面 GET 鉴权

- 补齐约 63 条未挂 Casbin 的页面 GET（`CasbinMiddlewareForPath` 指向该页对应的读权限点）。
- **enforce 的码必须 ∈ 该页菜单绑的码集**。否则就是 §2.4 的 AI 菜单那种「菜单可见、点进去 403」。
  一个菜单挂多个码的合并入口，要么收敛成单一读码，要么页面显式按「任一码命中」放行。

### 4.4 按钮显隐

- 模板改为按菜单码判断：`{{if .Buttons["admin.order.btn.create"]}}`，标签用**同一个 key** 取词
  `{{ tr("admin.order.btn.create", "新建") }}`。
- 服务端按 `sys_menu_permission` 算出「已授权的按钮码集合」交给模板。
  **注意**：这份数据（`buttonAuths[parentID]`）现在算在 `BuildAuthorizedRoutes` 里，而那是 §4.6 要删的
  路由投影 —— 删端点时必须把它挪到 `BuildAuthorizedTree` 这条路径上，不能跟着一起删。
- 模板层从此不出现权限码；权限码只活在 `sys_menu_permission`。

### 4.5 高亮：删掉 `navPathAlias`

改成「**当前路径按路径段做最长前缀匹配**某个菜单 path」（必须按段比，不能按字符串前缀——
`/admin/mail/campaign` 是 `/admin/mail/campaigns` 的字符串前缀，段匹配才不会误高亮）。

11 条别名里 10 条天生满足（`/admin/orders/new` 天然挂在 `/admin/orders` 下），
只有 `/admin/page/translations` 不满足（父菜单是 `/admin/pages`，单复数不一致）——
把这条路由改名成 `/admin/pages/translations` 即可。归属从此自动、零声明、零硬编码。

### 4.6 清理

- 删 `GET /api/admin/routes` 端点及其 `AdminRoutesResp` / `RouteNode` / `RouteMeta` /
  `AdminRoutes` / `BuildAuthorizedRoutes` 与相关测试、文档引用。
- 清 3 条死链菜单（`/project`、`/artifact`、`/publication`）。
- 菜单 95 的 path 是否从 `/api/page/redirect` 搬回 `/admin/page-redirects`（待定）。
- **净减列**：`permission_code`（470 之后已降级为兼容写入点，改掉写它的 seed 后即可删；
  迁移 470 的注释记为 24~27 条）、`component`（587 已退役）——两列删掉，满足「表结构最后还能减少」。

## 5. 门禁（判据）

| # | 判据 | 防的是什么 |
|---|---|---|
| 1 | 每条 `/admin` GET 页面路由都必须挂 Casbin，且其码 ∈ 该路径对应菜单绑的码集 | 页面裸奔；「菜单可见、点进去 403」 |
| 2 | 每条 type=2 菜单的 `path` 必须是运行时路由表里的 GET 页面路由 | 死链菜单（实测已有 3 条） |
| 3 | 菜单行的 `code` 必须在 handler 注册表里；按钮行的 `code` 必须被某个模板引用 | 改 key 导致页面查不到 / 按钮静默消失 |
| 4 | 模板引用的按钮 `code` 必须在菜单表里存在 | 模板与菜单数据双向漂移 |
| 5 | （沿用）`check-permission-gaps.sh` 两个方向 | `/api` 路由与权限点漂移 |

判据 1、2 都是**双向**的（路由 ↔ 菜单两边都要能对上），单向判据会给出假绿。

## 6. 迁移顺序（两刀）

**第零步（前置，已完成 2026-10-07）**：`02-X` 的注册落点统一 —— 10 处非 router 文件的注册已全部
搬进 `*_router.go` / `*_page_router.go` / `*_public_router.go`，门禁
`scripts/check-route-registration-placement.sh` 已接入 `check-all.sh`。

**第一刀（纯增量、风险最低）**

1. `title_key` → `code` 改名（含列注释）。
2. 按钮行补 `code`（68 条，按现有权限码派生一次，之后两者独立）。
3. 高亮改前缀匹配 + `/admin/page/translations` 改名 → 删 `navPathAlias`。
4. 门禁 2 / 3 / 4 落地。
5. 清 3 条死链菜单、删 `/api/admin/routes`。

**第二刀（动路由表语义）**

6. 页面 GET 补齐 Casbin（约 63 条）+ 门禁 1。
7. 路由改为按菜单表注册（`code` 查 handler 注册表）；`routes.snapshot` 语义调整。
8. 删 `permission_code` 与 `component` 两列（改掉写它们的 seed）。

## 7. 边界（明确不做）

- **不做客户端路由**（SPA）：本仓是服务端渲染的多页导航 + 页内 HTMX 片段；
  想要「点击不整页刷新」用 `hx-boost`（htmx 2.0.4 已 vendor），但要先处理它把 4xx / 5xx 判成
  `swap:false` 的问题，否则导航到 403 会停在原页不动。
- **不做 catch-all 分发器**：URL 免重启改动的收益不值三个门禁改读表。
- **不把权限码下发给前端**：判据是「不要把码写进输出」，不是「模板里不许出现码」——
  服务端判断天然要写码。
- **不动 `permission.Exempt`**、**不改 Casbin 策略结构**（`(主体, 路径, 方法, 短码)` 四列同行已够用）。

## 8. 本轮完成与实测（2026-10-07）

### 8.1 已完成

| 项 | 证据 |
|---|---|
| 删 `GET /api/admin/routes`（端点 + `AdminRoutesResp` / `RouteNode` / `RouteMeta` / `AdminRoutes` / `BuildAuthorizedRoutes` / `buildRouteNodes` + 前端 fake + 门禁豁免 + 文档） | 仓内零调用方；`TestAdminRoutesBuild` 的覆盖搬到 `TestEffectivePermissionCodesMergesRoleAndDirect`（原函数此前无覆盖） |
| 高亮改「按**路径段**最长前缀归属到菜单项」（`navOwnerPath`），删 `navPathAlias`（11 条手写）与 `NavPathFor` | 新增 `nav_owner_test.go` 9 条判据，含「按段比而非字符前缀」（`/admin/mail/campaign` vs `/admin/mail/campaigns`）与「隐藏项不认领」 |
| 页面翻译工作台改名 `/admin/page/translations` → `/admin/pages/translations`（唯一不满足「子页面挂在父菜单 URL 段下」的一处） | 全仓 30 处引用同批改；改名后归属自动成立，不需要任何声明 |
| 路由快照更新 | 672 → 690（+19 上游重构的路由、−3 本次删除、+2 改名） |

### 8.2 按钮码：已落地

模板层的词汇从权限码换成了**按钮码**（= type=3 菜单节点的 `title_key`）：

| 步 | 内容 |
|---|---|
| 数据 | 迁移 `589_menu_button_code.sql`（+ register）：给已有 68 个 type=3 节点填码 + 补 **56 个**缺失节点。新节点的 `title` 取 `sys_permission.permission_name`（授权界面上给运营看的那个名字，不另编一套中文），父菜单取「该模块的列表类菜单」，因此它们在「菜单管理」里落在正确位置 |
| 服务 | 契约加 `AuthorizedButtonCodes`（admin service 实现，与 `matchedMenuCodes` 同源：节点绑的任一码命中即算可用） |
| 注入 | `shell.PermContextMiddleware` 算出按钮码集合写进 context，`Prepare` 注入 `Buttons`（与 `PermSet` 同源、不同键空间） |
| 模板 | **143 处** `isset(.PermSet["x:y"])` → `isset(.Buttons["x.y"])`，37 个文件；模板层从此不出现权限码 |
| 测试 | 40 个测试文件同步（数据键形态 + `c.Set` 形态 + 4 处变量形态 + 1 处裸 `"perm_set"` 键） |
| 门禁 | 新脚本 `check-button-code-binding.sh`：① 模板每个按钮码必须在 `sys_menus` 里有 type=3 节点；② 模板里不得再出现 `PermSet[` 或 `Buttons["a:b"]`（权限码形态）。两个方向都先剔注释 |

**为什么放 `title_key` 而不是新增列**：这一列本来就是「节点的稳定标识」—— 菜单节点上它是标题的
i18n key，按钮节点上它是模板引用的按钮码，两者同义（「这个节点的 key」）。加一列会让同一件事
有两个存放点；改名成 `code` 则要处理 057 的 `CheckSQL` 与 3 条 seed 的列清单（见 §8.3）。

**代价（已认）**：按钮码在库里没有节点时，按钮**永远不显示且不报错** —— 这正是上面门禁 ① 存在的理由。

### 8.3 改名 `title_key` → `code` 为什么不做

**按钮码的覆盖缺口（修正后实测，已于 §8.2 落地）**：模板里用到 **92 个**权限码（145 处 `PermSet[...]`，
另有 2 处是文档注释里的假样本 `<权限码>` / `x`），而库里 type=3 按钮节点只绑了 **68 个**码 ——
**缺口是 56 个**（不是早先按模块粗估的 26 个）。分布：ai 11、admin 5、membership 5、product 5、
role 4、datarule 3、dept 3、menu 3、order 3、permission 3、inventory 2、navigation 2，其余各 1。

要改，就得：补 **56 个按钮节点**（每个要有标题、父菜单、权限绑定 —— 它们会出现在「菜单管理」界面里，
属于内容工作不是机械工作）→ 给全部 124 个节点填码 → 改 **145 处模板** → 加两条双向门禁。
收益是「模板里不出现权限码」；代价是新增 56 行菜单数据 + 一套映射与门禁，且按钮节点漏登记即静默消失。

**`title_key` 改名的迁移陷阱**（实测）：
- `057_sys_menus_title_key.sql` 是 **Migration**，其 `CheckSQL` 按 `column_name = 'title_key'` 判定 ——
  改名后新库上这条检查为假，**会把旧列名再加回来**（与新列并存）；
- `463` / `467` / `532` 是 **Seed**（每次启动都跑），它们的 `INSERT/UPDATE` 列清单里写着 `title_key` ——
  改名后每次启动都会 `column "title_key" does not exist`。

所以改名不是「一次 rename」，而是「新迁移 + 改 057 的 CheckSQL 与 SQL + 改 3 条 seed 的列清单」。

**结论**：这两件事的净效果是**增加机制**（一列身份 + 一张映射 + 两条门禁），而本轮目标是
**精简代码链路**。按 AGENTS.md「不做无收益的抽象」，本轮不做；需要时按 §4.1 / §4.4 单独开工。

### 8.4 死链菜单：已处置（清空 path + 新门禁）

三条隐藏菜单（`/project` 项目管理、`/artifact` 构建产物、`/publication` 发布管理）
**各自是 `project:list` / `artifact:detail` / `publication:receipts_pending` 的唯一承载节点**，
删掉会让这三个码在授权界面上勾不到（`codes → menu_ids` 反查不到节点）而它们仍被 Casbin 强制执行 ——
后果是「角色编辑一次就静默丢权限」。所以保留节点、清掉假的 path：

- 迁移 `588_menu_dead_path_clear.sql`（+ `register_menu_dead_path.go`）：把这三条的 `path` 清空，
  判据枚举本批自己的对象（上界封闭）。
- 新门禁 `scripts/check-menu-page-binding.sh`（DB 组）：**path 非空的菜单，其 path 必须是运行时
  路由表里的 GET 页面路由**。path 为空的行放过 —— 那是「能力节点」，承载可授权能力、没有页面入口。
  实测：54 条菜单 path 全部命中 253 条 GET 页面路由。

### 8.5 权限点漂移：已清零（原 7 条）

原先 `check-permission-gaps.sh` 方向 B 报的 7 条，实测**全部是「页面借用的 Casbin 对象」**——
路径存在只作为 Casbin obj，没有对应的 API 路由：

| 对象 | 借用方 | 处置 |
|---|---|---|
| `GET /api/i18n/list`、`POST /api/i18n/save` | 词条页 `/admin/i18n*` | 补 `I18nView` / `I18nManage` 常量 + 在注册处 `permission.Declare` |
| `POST /api/contenttemplate/delete` | 模板页的批量删除 | 在 `contenttemplate_page_router.go` 补声明 |
| `POST /api/seo/audit` | `/api/publication/seo-audit` 额外校验的第二条策略 | 补 `SEOAudit` 常量 + 声明 |
| `POST /api/mail/contact/{save,delete,tag}` | 联系人页写动作 | 已有 `declareMailPageObjects()`，只需门禁认得它 |

门禁方向 B 的判据随之收紧为「权限点的 api_path 必须在**运行时路由 ∪ 已声明的 Casbin 对象**里」——
「页面借用的对象」确实有人用（页面），不能只拿路由表比；**缺了声明的那几条仍然会被抓住**。
副产品：这 4 条的 `permission.RoutesOf` 从空变有值，**AI 工具对它们的 fail-closed 误判消失**。

### 8.6 原「死链菜单不能删」的证据（保留）

`/project`（项目管理）、`/artifact`（构建产物）、`/publication`（发布管理）三条隐藏菜单
**各自是 `project:list` / `artifact:detail` / `publication:receipts_pending` 的唯一承载节点**：

```
artifact:detail              → 菜单 6「构建产物」
project:list                 → 菜单 2「项目管理」
publication:receipts_pending → 菜单 7「发布管理」
```

删掉它们会让这三个码在授权界面上**无法勾选**（`codes → menu_ids` 反查不到），而它们仍被 Casbin 强制执行 ——
典型后果是「角色编辑一次就静默丢权限」。正确处置是**清空它们的 `path`**（它们是「能力节点」而非页面），
并在门禁里把判据写成「path 非空的菜单，其 path 必须是运行时路由表里的 GET 页面路由」。
本轮未做（需要一条迁移 + 一条新门禁），列入未决。

## 9. 未决

1. 菜单 95 的 path 是否从 `/api/page/redirect` 搬到 `/admin/page-redirects`。
2. 菜单码（`title_key` → `code`）与按钮码改造是否要做 —— 见 §8.2 的实测代价。
3. 三条死链菜单清空 `path` + 新增「菜单 path 必须是真实页面路由」门禁 —— 见 §8.3。
4. 第二刀（路由数据化）是否真的要做——第一刀做完，死链与鉴权缺口都已堵住，
   第二刀换来的是「改 URL 不用改代码」+「净减两列」。
