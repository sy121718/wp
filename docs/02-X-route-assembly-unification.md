# 后台页面路由装配统一（方案）

> 状态：**第一批已落地（2026-10-07）** —— 10 个非 router 文件的注册已全部搬进对应的
> `*_router.go` / `*_page_router.go` / `*_public_router.go`，门禁
> `scripts/check-route-registration-placement.sh` 已建成并接入 `check-all.sh`。
> 本文其余部分（§2~§7 的论证与约束）仍是后续批次的依据。触发点是「Jet 渲染是不是应该写在路由里」
> 的一次讨论，核实后结论与直觉相反 —— 渲染的落点全仓一致，真正不统一的是**路由注册的落点**
> 与**页面装配入口的形态**。本文给出事实基线、不可动的约束、逐文件工作清单与验收判据。
>
> **与 `02-Z-admin-menu-code-and-page-authz.md` 是同一个目标的两半**：本文管「注册写在哪」（落点与命名），
> 02-Z 管「一次注册写几样东西」（path / 菜单码 / 权限点 / handler 一次声明）。
> 两者叠加，共同目标是**精简控制器与代码链路**（同 order 模块那一轮 HTTP 层 5088 → 3564 行）。
> 本文是 02-Z 的前置：注册落点没统一之前，02-Z 的改动没有单一的落点可改。

## 1. 事实基线（实测）

### 1.1 改前（2026-10-07 首次基线）

| 项 | 实测值 |
|---|---|
| 含 `c.HTML(` 的文件 | 100 个 |
| 其中 `*_router.go`（共 32 个）里的渲染调用 | **0 处** |
| 真正注册路由、但文件不叫 router 的 | **10 个**（清单见 §5） |
| 顶层装配里模块专属的页面 setup 调用 | 14 条（`assembly_publish.go` 的 `mountAdminPages`） |
| 路由快照条目数 | 672（`internal/routers/testdata/routes.snapshot`） |
| 按**文件路径**登记的豁免条目 | 55 条（`scripts/page-endpoint-authz-allow.txt`） |

**修正记录（自查）**：初查我报「13 个文件」，其中 `workbench/inbound/http/dashboard_range.go`
与 `page/inbound/http/page_schedule_handle.go` 只是**注释里提到**路由语句，`user/inbound/http/router_customer.go`
本身就是 router 命名 —— 实际是 10 个。按 AGENTS.md「计数与命名不是判据」，这份清单已逐条回读上下文。

### 1.2 重新基线（2026-10-07，动手前）

动手前工作区已叠了一批未提交的模块重构（order / ai / mail / page / product / …），§5 的清单有 4 项
已被那批改动顺带完成（`theme_http.go`、`block_page_handle.go`、`navigation_page_handle.go`、
`contenttemplate_pages.go`、`product_translation_page.go`、`article_router.go`、`mail_tracking.go`
已不存在或已改名）。重新测量后**真实待改 10 处**：

| # | 文件 | 违规 | 处置 |
|---|---|---|---|
| 1 | `block/inbound/http/block_http.go` | 6 条注册 + `SetupBlockRoutes` + 6 个 handler + 3 个错误助手 | 拆成 `block_router.go` + `block_handle.go` |
| 2 | `block/inbound/http/block_page.go` | 6 条注册 + `SetupBlockPages` | 注册与装配搬进 `block_page_router.go` |
| 3 | `contenttemplate/.../contenttemplate_page.go` | 4 条注册 + `SetupContentTemplatePages` | 搬进 `contenttemplate_page_router.go` |
| 4 | `mail/inbound/http/mail_page.go` | 3 条 `/_t/*` 注册 + `SetupTrackingRoutes`（**公开面**） | 搬进 `mail_public_router.go`（文件头写明与后台链路的差别） |
| 5 | `media/.../media_page_handle.go` | 1 条注册 + `SetupMediaPages` | 搬进 `media_page_router.go` |
| 6 | `navigation/.../navigation_page.go` | 12 条注册 + `SetupNavigationPages` | 搬进 `navigation_page_router.go` |
| 7 | `plugin/.../plugin_page_handle.go` | 4 条注册 + `SetupPluginPages` | 搬进 `plugin_page_router.go` |
| 8 | `product/inbound/http/product_page.go` | 2 条翻译页注册 + `SetupProductTranslationRoutes` | 拆 `product_router.go`（API）/ `product_page_router.go`（页面），翻译页注册一并移入 |
| 9 | `project/inbound/http/project_handle.go` | 6 条 `/api/theme/*` 注册 + `SetupThemeRoutes` | `SetupThemeRoutes` 搬进 `project_router.go`；`SetupProjectPages` 拆到 `project_page_router.go` |
| 10 | `user/.../user_customer_admin_handle.go` | 4 条 `/api/customer/*` 注册 + `SetupCustomerAdminRoutes` | 搬进 `user_router.go`；`SetupCustomerPages` 拆到 `user_page_router.go` |

`workbench/inbound/http/router.go` 是 `router*.go` 命名，按 §4 规则 1 免检，不动。

**验收实测**：`go build ./...` ✓、`go vet ./...` ✓、受动模块 `go test` 全绿、
路由清单「消失 0 条」（搬移零丢路由）。**路由快照本身是红的**（快照 672 / 当前 691），
但 19 条差异全部来自上面那批未提交的模块重构（AI / mail contacts / customers / orders overview / `/mcp`），
与本次搬移无关 —— 快照需要在那个批次里单独更新。

### 1.3 规则 2 的收尾（2026-10-07 同日）

§4 规则 2 要求「`<模块>_router.go` = API 装配、`<模块>_page_router.go` = 后台页面装配」。
基线时有 **11 个 router 文件把两者混在一起**（`ai` / `analytics` / `comment` / `content` /
`inventory` / `mail` / `masterdata` / `membership` / `order` / `page` / `sysconfig`），已全部拆开：

| 处理方式 | 模块 |
|---|---|
| 页面段原本就是独立函数，只搬文件 | `content`（`SetupContentPages`）、`mail`（`setupMailPageRoutes` + `declareMailPageObjects`）、`analytics`（`setupAnalyticsPageRoutes`）、`page`（`setupSiteSlotPageRoutes`） |
| 从 API 函数里抽出 `if pages != nil {…}` 段，成 `SetupXxxPages` 并在**原位置**调用 | `ai` / `comment` / `inventory` / `masterdata` / `membership` / `order` / `sysconfig` |

**关键纪律**：页面段的调用点必须在**原位置**（02-X §3 的「不重排装配」）——
所以做法是「抽函数 + 在原处调用」，而不是把页面注册挪到装配层的另一处。
外部签名与调用点一个都没变。

**踩到的坑（值得记住）**：抽出函数后忘了删原位置的调用 → **gin 对重复路由直接 panic**，
而 `go build` / `go test ./internal/...` **抓不到**（路由快照测试默认 `t.Skip`，
只有 `WP_DUMP_ROUTES=1` 才跑完整装配）。所以这类改动**必须**跑一次
`WP_DUMP_ROUTES=1 go test ./internal/routers/ -run TestRouteSnapshotStable`。

**连带项（同批改，否则门禁红）**：`scripts/page-endpoint-authz-allow.txt` 按**文件路径 + 路由**
登记，页面注册搬文件后共 14 条需要重指（product 6 / project 3 / content 3 / page 1 / inventory 1 已在前两批改完）。
另有 3 个**读源码的测试**跟着改：`workbench_reuse_insert_test.go`（block）、
`seo_page_handle_test.go`（analytics）、`inventory_reason_page_test.go` +
`public/test/inventory/feature/inventory_external_sku_edit_test.go`（inventory）。

## 2. 为什么渲染不能搬进路由

`internal/module/CLAUDE.md` 的分工是：`router.go` 只做「获取 db、创建 model 与 service、注册路由」，
`handle` 负责「参数绑定、调用 service、**输出响应**」。渲染 HTML 就是输出响应。

更硬的理由是数据形态：`render` / `c.HTML` 的入参全是 **per-request** 计算的 —— 从 PostForm 重建的明细行、
哪些字段标红、候选视图、`FormReady` 判据、国家下拉选中态。router 只有装配语境（进程启动期），拿不到
`*gin.Context` 里的请求状态。把渲染上提，等于让路由层去查库、读表单。

**其中唯一可以上提的是「这条路由渲染哪个模板」这一条信息**（模板名绑定），它是独立的议题，见 §7。

## 3. 不可动的约束（先读这三条，再改任何东西）

1. **装配顺序是隐式的依赖契约，不是历史堆积。** `mountAdminPages` 的文件头已写明两种形态的成因：
   「页面只依赖本模块 svc」→ 在模块 Setup 内自注册；「页面依赖**晚装配契约**」→ 独立入口、契约齐备后调用。
   实证：`SetupCustomerPages` 收 `a.orderSvc`，而 orderSvc 在 `assembly.go:695` 才构造；`SetupProductPages`
   收 `a.presentationSvc` / `a.contentTemplateSvc`，两者分别在 `assembly_publish.go:82` 与
   `assembly.go:561` 构造。
2. **因此不合并「API 入口」与「页面入口」。** 合并要求把页面 setup 挪到各模块的 API 装配点，
   那是**重排装配**；CQ-008 的结论是「只切段、不重排」，因为顺序漂移编译期看不出来。
3. **快照有盲区。** `routes.snapshot` 只记录 `METHOD PATH`，**抓不到「同一路径换了 handler」**。
   所以本方案的纪律是「函数整体搬移、不重打一遍注册」—— 快照能证明路径与分组零漂移，
   证明不了接线没接错，后者只能靠真实 HTTP 冒烟。

## 4. 目标形态（四条规则）

1. **落点**：路由注册（`.GET(` / `.POST(` 调用）只出现在 `*_router.go` 与 `router_*.go`；
   `*_handle.go` 只放 handler。
2. **命名**：`<模块>_router.go` = API 装配；`<模块>_page_router.go` = 后台页面装配；
   `<模块>_handle.go` / `<模块>_page_handle.go` = handler。**废弃两个语义不明的后缀**：
   `*_http.go`、`*_pages.go`（存量各 1~2 个）。
3. **入口唯一**：每个模块最多两条对外入口 —— `SetupXxxRoutes`（API）与 `SetupXxxPages`（后台页面），
   都在 router 文件里声明；页面入口的**调用点只有 `mountAdminPages` 一处**。
4. **参数只收契约**：页面入口只接收 contract 接口，不接收 `*gorm.DB`、不收对方的 model
   （现存页面入口已符合，保持不回退）。

## 5. 逐文件工作清单（改前基线，已被 §1.2 取代）

> 下表是**动手前**的清单，其中 #2、#3、#4、#6、#7、#9、#11 已被上游的模块重构顺带完成
> （对应文件已改名或删除）。实际执行的是 §1.2 的 10 处，已全部完成。

| # | 现状文件 | 现状 | 目标动作 | 阻断点 / 备注 |
|---|---|---|---|---|
| 1 | `block/inbound/http/block_http.go` | `SetupBlockRoutes` + 10 个 handler + 3 个错误助手，186 行 | 拆成 `block_router.go` + `block_handle.go` | **block 模块没有 router 文件**，是本批唯一的模块级缺口；`blockErrorStatus` / `blockErrorMessage` / `paramBindFail` 随 handler 走 |
| 2 | `project/inbound/http/theme_http.go` | `SetupThemeRoutes` + 6 个 handler，163 行 | 拆成 `theme_router.go` + `theme_handle.go` | 被 `project_router.go:32` 调用，调用关系不动 |
| 3 | `block_page_handle.go:532` `SetupBlockPages` | 装配函数住在 handler 文件 | 移到 `block_page_router.go` | 迁移 447 注释引用了本文件 `line 29`（title 常量），29 < 532，搬走尾部函数不移行号 |
| 4 | `media_page_handle.go:19` `SetupMediaPages` | 同上 | 移到 `media_page_router.go` | 全模块只有 1 条页面路由 |
| 5 | `plugin_page_handle.go:160` `SetupPluginPages` | 同上 | 移到 `plugin_page_router.go` | — |
| 6 | `navigation_page_handle.go:661` `SetupNavigationPages` | 同上 | 移到 `navigation_page_router.go` | 迁移 447 引用 `line 33`，同样在 661 之前，不受影响 |
| 7 | `product_translation_page.go:83` `SetupProductTranslationRoutes` | 同上 | 移进已存在的 `product_page_router.go` | 该函数已被 `product_page_router.go:200` 调用，只是搬定义 |
| 8 | `user_customer_admin_handle.go:51` `SetupCustomerAdminRoutes` | **API** setup 住在 handler 文件 | 移到 `user_router.go` | 与 `router_customer.go`（访客客户页）的分工需在文件头写明 |
| 9 | `contenttemplate/inbound/http/contenttemplate_pages.go` | 50 行、纯注册、无 handler | 改名 `contenttemplate_page_router.go` | 纯改名，零逻辑 |
| 10 | `content/inbound/http/article_router.go` | 103 行、纯注册 | **暂不改名** | 已被 `page-endpoint-authz-allow.txt` 按路径登记 **3 条**；改名必须同批改豁免清单，否则该门禁按「登记项不再命中」判失败 |
| 11 | `mail/inbound/http/mail_tracking.go:137` `SetupTrackingRoutes` | 公开面 setup 住在 handler 文件 | 移到 `mail_public_router.go`（或 `mail_router.go`） | 公开面（无会话、无 Casbin），与 `authorizedAPI` 链路不同，文件头要写明这层差别 |

`page/inbound/http/router_site_slot.go`、`user/inbound/http/router_customer.go`、
`runtimefragment/router.go`、`workbench/inbound/http/router.go` 虽然不叫 `*_router.go`，
但都是 router 命名（`router_*` / `router.go`），符合规则 1，**不动**。

## 6. 安全网与验收

每个模块一次提交，每批都必须过：

```bash
# 路径级零漂移（672 条逐字节一致）
WP_DUMP_ROUTES=1 go test ./internal/routers/ -run TestRouteSnapshotStable -count=1 -v
go build ./... && go vet ./... && gofmt -l cmd internal pkg public
go test ./internal/routers/... ./internal/templates/...
go test ./public/test/<改动模块>/feature -v
```

**必须补的一步（快照抓不到）**：登录后台，逐个真实 GET 受影响的页面 ——
`/admin/blocks`、`/admin/media`、`/admin/navigations`、`/admin/plugins`、`/admin/customers`、
`/admin/content-templates`、`/admin/themes`，断言 200 且标题正确。搬移函数是零运行时风险的改动，
但「注册调用接错 handler」是快照的盲区，只能这样验。

## 7. 明确不做

- **不把渲染搬进 router**（理由见 §2）。
- **不合并 API 与页面两段装配**（理由见 §3）。
- **不重排 `mountAdminPages` 内的调用顺序** —— 只允许在原位置搬函数、补注释。
- **「模板名绑定上提到路由」单独立项**：它要改 100 个 `c.HTML` 调用点的形态，
  收益是「一个页面的模板名集中可见」。要先确认这个收益值不值，再谈怎么做。

## 8. 门禁（已落地，防回流）

`scripts/check-route-registration-placement.sh`：

- 扫描 `internal/module/**/inbound/http/*.go`，跳过 `*_test.go` 与 `*_router.go` / `router*.go`；
- 命中**非注释行**的 `.GET("` / `.POST("` 即失败，打印 `文件:行` 与替换指引
  （`scaffold` 产物目录天然在扫描范围外；实测 `plugin/scaffold` 生成的骨架已经是
  `inbound/http/<name>_router.go` + `<name>_handle.go`，符合本规则）；
- 带豁免清单 `scripts/route-registration-allow.txt`（格式：`文件<TAB>理由`），
  且**「曾登记、如今不再命中」同样判失败** —— 与 `scripts/contract-deps-allow.txt` 同规，
  避免清单退化成注释（AGENTS.md §数据库对登记表的要求）。清单当前为空（10 处全部修完）；
- **自带双向自检**：造一个含 `.GET("` 的非 router 文件必须被拦下（负向），
  造一个含 `.GET("` 的 `*_router.go` 必须免检（正向）—— 任一条不成立脚本自己判失败（exit 2），
  防止判据退化成空转；
- 已接入 `scripts/check-all.sh` 的 `PLAIN_SCRIPTS`，CI 经该入口自动纳入
  （`.github/workflows/go-test.yml` 的 Static checks 步）。

**同批必须改的连带项**（实测踩到）：`scripts/page-endpoint-authz-allow.txt` 的条目按
**文件路径**登记，搬移注册必须同批改这 9 条（product 6 条 + project 3 条），
否则该门禁会以「过期豁免」判失败。

## 9. 排期与进度

| 批次 | 内容 | 状态 |
|---|---|---|
| 1 | 搬函数 + 拆文件（§1.2 的 10 处，含 `block_http.go` 的拆分） | **已完成（2026-10-07）** |
| 2 | §8 门禁脚本 + `check-all.sh` 接入 | **已完成（2026-10-07）** |
| 3 | §4 规则 2 收尾：11 个混合 router 文件拆成 API / 页面两个文件（§1.3） | **已完成（2026-10-07）** |
| 待定 | §5 #10（`article_router.go` 改名）—— 该文件已被上游重构删除，条目作废 | — |
| 待定 | §5 #11（公开面 router 命名）—— 已按 `mail_public_router.go` 落地 | **已完成** |
