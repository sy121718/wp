# 后台改造工作清单（逐页执行底稿）

> 本文是**动手前的最后一份文档**：把三轮并行检测的结果去重、交叉验证后，整理成
> 「按优先级排序、每条带证据与修法」的执行清单。
>
> 配套文档：`02-H`（页壳规范）、`02-I`（全站模型 + 度量表）、`02-J`（检测清单 + 方法论）、
> `02-K`（Ant Design 组件参考）。
>
> **证据可信度**：所有度量来自独立 Chrome 实例（puppeteer-core + 独立 user-data-dir）实测，
> 每页读数前断言 `location.pathname`。被并行任务抢占或会话轮换的读数已丢弃重测。

## 0. 已完成（本轮）

| # | 项 | 改动 | 验证 |
|---|---|---|---|
| 1 | **库存空态文案 P0** | 迁移 `317_fix_inventory_moves_empty_i18n.sql` + `register_inventory_moves_empty_i18n.go` | 库里两语言值已更正；幂等判定返回 1（已达标跳过）；`go test ./public/migrations/` 通过 |
| 2 | **`/admin/articles/new` 403 P0** | `internal/middleware/builtin/casbin.go` 新增 `CasbinMiddlewareForPathAs(obj, act)` + `article_router.go` 调用点显式声明 `http.MethodPost` | 编译通过；`TestArticleNewPageHandler` + 新增 `TestArticleNewPageCasbinForPathAsValidatesAction` 通过；第三方实测该页返回 200 |
| 3 | **`--ui-sp-xxl` 悬空引用（存量缺陷）** | `ui.css` 的 `:root` 补 `--ui-sp-xxl: var(--sp-xxl, 32px)`；`theme.css:982/994` 两处引用补兜底 | **浏览器实证修复前 `.empty-state` computed padding = `0px`**（84 处空态、51 个文件全无内边距）；探针设 `padding:var(--ui-sp-xxl)` 解析为 `0px` 而 `--ui-sp-xl` 正常为 `24px` |
| 4 | **模型文档度量纠正** | `02-I` §5.1 整列重算、§5.2 作废 3 条、新增 §5.2.1~5.2.5（30 条新缺陷 + 模式 A 全量核验） | 三份独立报告交叉核实 |
| 5 | **检测方法论固化** | `02-J` §H 六条 + §F1 选择器补充 | 三条误判的共同根因已记录 |
| 6 | **navigations 布局改造**（你指出的「布局操作起来很奇怪」） | 两个「添加」区块从常驻卡 → **页头主行动 + 抽屉**（复用该页已有 `data-drawer-*` 机制）；补 `nav-source-*` 的缺失样式；来源分组默认展开；候选显示 `Title` 而非 `Label` | 实测：页高 1.25→**1.00 屏**；表格顶边 0.55→**0.22 屏**；首屏可见行 6→**10/10**；常驻卡 3→**1**；抽屉交互实测可开、7 字段齐全、原生 select 保留增强、`legacyInlineForms:0` |

## 0.1 内容域审核已完成（独立 Chrome 实测，父 agent 已复现 3 个 P0）

| # | 级别 | 位置 | 问题 | 父 agent 复现结果 |
|---|---|---|---|---|
| C-A | **P0** | `navigation_page_handle.go:309/334/387/420/493`、`navigation_panel_page.go:53` | **navigations 写失败返回裸 JSON**：`POST /admin/navigations/add-source` 空勾选提交 → `400` + `{"code":400,"message":"必填字段不能为空"}`，DOM 仅 2 节点、无页壳、URL 停在 POST 路径 | **已复现**：`status:400, isJson:true, len:33`。同域 articles/pages/blocks/i18n 全都做对了（302 + `?err=` + 页壳）—— **同模块内实现漂移** |
| C-B | **P0** | `page_translations_handle.go:214` | 保存失败**裸出未翻译 i18n key** `MsgFieldRequired` | **已复现**：`400` + body `MsgFieldRequired`（16 字节，`isBareKey:true`）。`02-I` §5.2.5 只记了 `datarule_edit` 一处，**实际 2 处** |
| C-C | **P0** | `rich-editor.css:18-30` | **Trix 编辑器无 `max-height`** → 正文越长编辑器越高，保存按钮被推到 12 屏外 | **已复现**：`screens:12.21`、`trixH:**9779px**`、`maxHeight:none`、`overflowY:visible`、保存按钮 `saveScrollNeeded:**12.11 屏**` |
| C-D | **P0** | `navigation/outbound/source/resolver.go:83→95` | **导航来源读不到页面标题**（父 agent 发现）：`pageCandidates` 用 `List` → `ListAll` 刻意 `Omit("draft_document")` → `pageDocTitle(空)` 恒返回 `""` → **静默回退成路径**。库里 `seo_title="About VapeStoreOZ"`，界面显示 `/about` | **已复现**：抽屉 13 个页面候选全显示路径；DB 侧带 RLS 作用域查询确认标题存在 |

### 对既有清单的两处更正（第三方实测）

| 原记录 | 实测更正 |
|---|---|
| `02-L` P1-4：blocks 三段表列头「6/6/8 列」 | **实为 5/5/7 列**（`blocks.html:163`/`:194` 各 5 列，`:225` 为 7 列） |
| `02-L` P1-8：article_edit「发布按钮在第 11 屏」 | **真正在 11~12 屏的是「保存」按钮**；「发布」卡在右栏 aside，只在 **1.55 屏**。屏数实测 **12.21**（随正文长度浮动，原记 13.75） |
| `02-L` P1-20：双 `lang` 下拉是 i18n 独有 | **根因在 `layout.html:83` 的全局语言切换**，波及 3 页（i18n + `article_translations.html:32` + `navigation_translations.html:33`）—— 全局 `<select name="lang">` 与页面筛选同名、同屏、均无可见 label |
| `02-L` P0-1：裸文本出口「6 处」 | **严重低估。`admin_pages_handle.go` 一个文件就有 26 处未修**：19 × `c.String(400, pagesMsgFieldRequired)` + 7 × `c.String(500, pagesMsgAdminGenericFailed)`。另有 8+ 处同判据散布在 workbench / analytics / contenttemplate / project / masterdata / plugin。父 agent 已实测确认 `POST /admin/datarules/delete` 缺 id 仍返回 400 + 裸 `MsgFieldRequired`（16 字节、无页壳） |

## 0.2 内容域第一批修复：已完成并**父 agent 独立验证通过**

| 批 | 内容 | 父 agent 复现结果 |
|---|---|---|
| 批1 | 写失败出口改 303+`?err=`（navigations 5 处 + page_translations 1 处 + admin_pages 896） | `add-source` 空勾选 → 200 + `?err=请至少勾选…` + 页壳；`delete` 缺 id → `?err=请求参数无效`；`page/translations/save` 空 → `/admin/pages?err=必填字段不能为空`；`datarules/edit` 缺 id → `/admin/datarules?err=请求参数错误` + 页壳 + h1。**全部通过** |
| 批2 | `rich-editor.css` 加 `max-height: min(60vh,560px)` + `overflow-y:auto`；保存卡 sticky + `.admin-content:has(> .article-edit-page){overflow-y:visible}` | 页高 **12.21→2.16 屏**；Trix **9779→545px**；`maxHeight` none→**544.8px**；`overflowY` visible→**auto**；保存按钮滚动需求 **12.11→0.93 屏**；`position:sticky`；滚到中部 `stickyWorks:true`；`overflowX:0`。**全部通过** |
| 批3 | 导航来源标题改 SQL 侧投影（`ListPageTitles`）+ resolver 消费 | 抽屉显示 **About VapeStoreOZ / Vaping Guides & News / Contact Us**（原为路径）；`/test`（库中无标题）正确回退路径；`/admin/pages` 未受影响（13 行、overflowX 0）；数据库层直接执行该 SQL 确认正确。**全部通过** |

## 0.3 管理域审核已完成（9 页，父 agent 已复现最严重的 P0）

### P0 清单（按严重度）

| # | 位置 | 问题 | 父 agent 复现 |
|---|---|---|---|
| M-1 | `static/js/admin.js` 全选绑定（约 `:549-553`） | **数据破坏路径**：全选循环不过滤 `hidden`，客户端过滤后点全选会勾中隐藏行 | **已复现**：过滤「商品」→ 可见 **17/117** 行 → 点全选 → `checkedTotal=**117**`、`checkedInHidden=**100**`、界面「已选 **117** 项」、批量表单将提交 **117 个 id**。用户意图删 17 个，实际删 117 个菜单（权限体系骨架）。**静默越界** |
| M-2 | `admin_pages_handle.go` 全域 | **写失败 100% 无回跳**：19 × `c.String(400, MsgFieldRequired)`（16 字节裸 key）+ 13 × 裸 JSON（40 字节）+ 3 × 静默 303 + 1 × 404 裸文本 | 审核代理实测 35 个出口；父 agent 复现 `POST /admin/datarules/delete` 缺 id → 400 + 裸 `MsgFieldRequired`。**读侧白名单已完全就绪，只需换出口** |
| M-3 | `administrators.html` / `roles.html` / `datarules.html` | **筛选栏是死控件**：模板有输入框+按钮+回显位、注释写「条件进 SQL」，但 handler 从不读 query | 审核代理实测：`?name=ZZPROBE` / `?keyword=` / `?domain=` → 全部 `echoed:false` 且行数不变。**正确样本只有 `permissions`** |
| M-4 | `role_permissions.html` | 保存按钮在 **4.08 屏**、`saveScrollNeeded=3.13`、116 节点默认全展开 | 审核代理实测 |
| M-5 | `datarule_edit.html` | 全文**无 `{{if isset(.Err)}}` 错误槽位**（`errSlot:false`）+ handler 的「id 不存在」分支仍是 404 裸文本 | 审核代理实测 |
| M-6 | `admin_pages_handle.go:285-288` | `role_permissions` 缺 `role_id` **静默 303**（本域唯一静默 GET） | 审核代理实测 |

### 全域共性（6 条，可合批修）

① **6 页共用 `{{if len(.Rows)==0}}空态{{else}}<table>{{end}}`** → 空数据连表头都不渲染，空态 6/6 无 `.empty-actions`；
② **分页缺失/静默截断**：`administrators`/`roles`/`datarules` 硬编码 `Limit:100` 且 `pagination.html` 是**死 include**（依赖 `PaginationLinks` 键，缺键永不渲染），`menus`/`departments` 完全无分页；
③ **全域无排序**（`sortableHeads=0`），而 `AdminListReq` 已声明 `SortField/SortOrder` 白名单 —— 能力具备、未接 UI；
④ **抽屉原生表单 = 失败丢全部输入**（6 页所有增删改表单都是 template 里的原生 `<form method=post>`，无 hx-post）；
⑤ **每行一份抽屉模板**（`templates = 行数+1`，menus 480KB/4603 节点）；
⑥ **破坏性确认一律「确认删除？」**，批量也不说删几个。

### 建议批次与文件冲突

| 批 | 内容 | 文件 | 并行性 |
|---|---|---|---|
| A | 写失败出口收口（19+13+3+1 处） | `admin_pages_handle.go` | 独占 |
| B | 死控件筛选（3 页接参数+回显） | 同上 | A 之后 |
| C | menus 全选越界 + 两页 `data-filter-empty` | `admin.js` + `menus.html` + `departments.html` | 可与 A 并行 |
| D | 空态表头统一（4 模板） | `administrators/roles/permissions/datarules.html` | 并行 |
| E | 分页统一 | 同上（Go） | B 之后 |
| F | role_permissions sticky + datarule_edit 错误槽位 | `role_permissions.html` + `datarule_edit.html` | 并行 |
| G | P2 打磨（确认文案说后果、行操作加「配置」、menus 编辑抽屉父级 select、tab 写 URL） | 多模板 | 最后 |

> **已派发**：C（M-1，最严重）、A（M-2）、D、F 四批并行（文件所有权已切分避免冲突）。

## 0.4 管理域第一批修复：已完成并**父 agent 独立验证通过**

| 批 | 内容 | 父 agent 复现结果 |
|---|---|---|
| C（M-1） | `admin.js` 全选跳过隐藏行 + `refresh` 分母改可见行 + 筛选变化重算全选态；`menus`/`departments` 补 `[data-filter-empty]` | **三方向全绿**：过滤后全选 `checkedInHidden` 100→**0**、formIds 117→**17**；清空过滤后全选 117/117 无漏选；**额外缺陷也修好**（过滤态全选→清空过滤后 `checked=false / indeterminate=true`，再点得 117，此前会「点它反而全取消」）。回归 `menus 117/117`、`products 20/20`、`permissions 20/20`、`blocks 3/3`（`checkScopeOk` 仍 true）。**全部通过** |
| A（M-2） | 35 个写失败出口 → `303 + ?err=`（含 `datarules/update` 双入口分流、6 个 bulk-delete 空 ids、2 处静默、1 处 404 裸文本） | **10/10 抽样复现通过**：6 个领域 create 全部 `?err=请求参数错误` + 页壳；bulk-delete 空 ids 不再静默；`GET roles/permissions` 缺参不再静默；`GET datarules/edit?id=不存在` 不再 404 裸文本。**它上报的 `departments.html` 缺 CSRF 隐藏域经我实测证伪**（抽屉表单 `hasCsrf:true, csrfLen:64`，它只读了未展开的顶层 DOM） |
| F（M-4/M-5） | `role_permissions` sticky 底栏 + 页头提交按钮 + 已选计数 + 目录层折叠；`datarule_edit` 错误槽位 | 页高 **4.15→1.00 屏**；`action-row` **sticky**；页头提交 `form="perm-form"`；计数「已选 0 项」；**作用域隔离**（本页 `overflow:visible`，roles/permissions/articles 三页仍 `auto`、sticky 计数 0）；错误槽位带 `?err=` 可见；**XSS 转义生效**（`<img onerror>` 未注入）。**全部通过** |
| D（M-1 附带） | 四页空态/表头统一（`administrators`/`roles`/`permissions`/`datarules`）：`<table>` 移出 `{{if}}`、空态进 `tbody` 的 `colspan` 行、补 `.empty-actions`、批量条加 `len(.Rows) > 0` | **核心判据通过**：`permissions?code=zzzznomatch` → `tablePresent:true, theadCols:9`（**空态也渲染表头**）；`emptyColspan:9` 与表头列数严格一致；三段式齐全；主行动按钮点击后抽屉真打开（8 字段）；四页 `has点右上:false`（库值已更新）；`overflowX:0` |

### 附带完成（父 agent 直接做）

- **`admin.depts.filter_empty` / `admin.menus.filter_empty` 缺 seed** —— 两个代理都把它报为阻塞项（`TestGroupFI18nTemplateKeysMatchSeed` 失败）。父 agent 补了迁移 **400** + `register_client_filter_empty_i18n.go` + 注册，门禁现已全绿。
- **注册重复已消除**：批D 的 `register_list_empty_i18n.go` 自带 `func init()` 自注册，而父 agent 曾在 `register.go` 也加了一次显式调用。两处都调虽被 `sync.Once` 兜住不重复执行，但会让「谁负责注册」出现两个真源 —— 已移除 `register.go` 里的那行，**改为「每个注册文件自带 `init()`」或「`register.go` 显式调用」二选一**（项目现状混用：31 处显式调用 + 3 个 init）。

### 新增待办

- **6 处 `c.String(500, pagesMsgAdminGenericFailed)`**（`admin_pages_handle.go:175/283/517/682/840/941`，6 个列表页 GET 的**取数失败**出口）仍在。
  **父 agent 已核实正确处理方式**：这 6 处发生在**渲染前**（模板需要数据才能渲染），此时没有页壳可套；
  而 `shell.PageError` 本身也是 `response.ErrorWithMessage`（**JSON，不渲染页壳**）—— 直接换它**不解决问题**。
  正解是**渲染列表页 + `Err` 键**（走批A 已建立的 `?err=` 机制），即「取数失败也给出带导航的可读页面」。
  这需要各 handler 在取数失败时仍组织一份最小渲染数据（空列表 + Err 文案），属设计决策，**未擅自实施**。
- `pagesMsgRuleConfigInvalid` 是既有死常量（早于本轮），未清理；
- `menus/delete` 的存在性预检会多一次 `MenuTree` 查询（几十节点级）；
- `roles` / `datarules` 的 handler 硬编码 `Page:1, Limit:100` 且**不读筛选参数**（M-3 死控件），因此这两页的空态无法在浏览器里制造 —— 批D 的空态结论来自 Go 侧渲染。**M-3 修复后应补浏览器级复验**；
- **建议加一条机械判据**（批D 提出）：「空数据时 `table.data-table thead` 必须存在 + 空态行 `colspan` == 表头列数」放进 `admin_ui_contract_test.go`，否则这类回归下次还会出现。**这条判据值得沉淀**。

## 0.5 系统域审核已完成（10 页）—— 2 个新 P0 + 10 条对既有清单的更正

### 新 P0（父 agent 已独立复现）

| # | 位置 | 问题 | 父 agent 复现结果 |
|---|---|---|---|
| S-1 | `plugin_page_handle.go:86/92/97/106/112/121/127`（**7 处** `c.String`）+ `plugin/enums/plugin_enums.go:15/19/20` | **写失败裸出英文内部标识符**，且脱离页壳 | **已复现**：`POST /admin/plugins/toggle`（缺 id）→ `400` + **`ErrToggleFailed`**（15 字节、`hasShell:false`）；`uninstall` → `ErrUninstallFailed`；`install`（无文件）→ `ErrInstallParse`。**根因两层**：① `c.String` 直出；② `plugin_enums.go` 的常量值**本身就是英文标识符**（`ErrInstallParse = "ErrInstallParse"`，中文只写在注释里）—— **比裸 i18n key 更差**，且违反 `internal/module/CLAUDE.md`「未接 i18n 时应等于中文常量」的约定 |
| S-2 | `sys_menus.id=137`（「邮件活动」，path=`/admin/mail/campaign` **不带参数**） | **菜单死链**：点正式菜单项永远看不到目标页 | **已复现**：`/admin/mail/campaign` → `302` → `/admin/mail/marketing?err=**参数不合法**`。与 §5.2.1 #18「商品详情模板」**完全同型**。父 agent 已查库确认：`id=137 \| 邮件活动 \| /admin/mail/campaign \| status=1` |

### 对既有清单的 10 条更正（实测推翻）

| 原记录 | 实测更正 |
|---|---|
| #41：`node_type_N` × **6 选项** = 72 option | **每行 7 个 option，全页 `totalOptions=92`**（根因：`automationRowOptions` 先插一个空选项） |
| §5.1「空态三段式样板：`plugins.html:29`」 | **实为两段式** —— `{title:"还没有安装插件。上传 zip 包安装你的第一个插件。", desc:null, actions:"安装插件"}`，**无 `.empty-desc`**（title 承担了 desc 职责） |
| P0-3「`<table>` 被包进 `{{else}}`：4 处」 | **本域实测 9 处**（漏 `mail.html` 2、`mail_automation` 2、`mail_campaign` 2、`plugins` 1）；且 `mail_marketing` 行号已漂移为 **97/168**（非 98/161） |
| P1-15「空态无 `.empty-actions`：11 页 / 19 处」 | **本域另需补 5 处**：`page_redirects`、`mail_marketing`（联系人）、`mail_automation`（实例）、`mail_campaign` ×2 |
| §5.2.4 #42 / P2-1「`mail_campaign:62` desc 以 title 原句开头」 | **实为两处（`:74-75` 与 `:96-98`）、title 与 desc 逐字完全相同**（不是「以…开头」）；「以 title 原句开头」的是 `mail_automation:136` |
| §5.2.5 canvas 行「缺 id/不存在 → 302 + 流程不存在（中档）」 | **缺 id 与不存在给同一句**，属**文案错位**（缺 id 应说「缺少流程 id」） |
| #21 / P1-19（page_redirects） | **补充：该页不继承 layout** —— 实测 `hasLayoutSidebar=0`、`hasTopbar=0`、`.rail` 数 0，页面唯一出口是 `/admin/pages`；创建表单两个 input 只有 `sr-only` label + 英文 placeholder |
| §0.4「同形态出口未修 26 处」 | **plugins 可精确到 7 处**，且 `pluginenums` 常量值是英文标识符（比裸 key 更差） |
| P1-13「6 页无分页」 | **本域实测 7 处**：`mail`（账号+模板）、`mail_marketing`（联系人+活动）、`mail_automation`（流程+实例）、`mail_campaign`（收件人）；其中 `mail_campaign` 是**假分页**（有「第 N 页」文本、无翻页控件），比「无分页」更差 |
| §5.2「不要动：`mail_automation_canvas` 无 `.page-head`」 | **核验成立**（页面仍在外壳内、有侧栏、双返回入口、`aria-live` 状态反馈齐全）。`login` 无 `.page-head` 同理成立 |

### 系统域共性问题（7 条）

① **空态表头缺失本域 9 处**（最大一块）；② **写失败丢输入**（mail 全域原生 POST + 302，账号 11 字段 / 自动化 12 行节点；`mail_automation_edit` 更严重 —— 错误文案精确到「第 N 行」但返回后该行已空，**用户实测确认**）；③ **空态三段不齐 + 文案不分档**（无 actions 6 处、title/desc 同句 5 处、筛选后不分档 2 处）；④ **列表可用性缺口**（6 个列表全无排序/分页；`mail_campaign` 假分页；`mail_automation` 两表无批量）；⑤ **缺参档位错位**（三处模式 A 仍未修 + 菜单死链）；⑥ **i18n 库值覆盖模板 default**（第三次复现）；⑦ **写失败出口形态不统一**（mail 域用三件套 ✓，plugins 域 `c.String` 直出英文 ✗）。

### 值得推广的正面样本（系统域）

- **`login` 的错误就近提示** —— 实测：`role=alert` + `aria-live=polite` + **焦点自动转移**（`document.activeElement.id === 'login-msg'`）+ **用户名/密码保留** + 验证码自动刷新。**全站最好**，建议作为 `02-K` §2.2 的落地样板。
- **mail 域的破坏性确认文案** —— 4 处都说了后果（「正在用它发信的邮件会失败」「投递记录与报表会一并消失」「进行中的实例会先停止」），优于管理域的「确认删除？」。**唯一例外是 plugins 的卸载**（只说「删除插件文件」，实际会 `DROP SCHEMA … CASCADE` 连数据一起删）。
- **`mail_err.go` 的错误三件套**（白名单 + 归口 + 结构化日志）—— plugins 域该抄它。

> **已派发**：S-1（plugins 8 处出口）、S-2 + 模式A 三处（mail 域缺参档位）。**两者均已完成并经父 agent 复验**（见下）。

### 0.5.1 S-1 / S-2 修复结果（父 agent 独立复验）

| 请求 | 改前 | 改后（父 agent 实测） |
|---|---|---|
| `POST /admin/plugins/toggle`（缺 id） | `400` + `ErrToggleFailed`（无页壳） | **`200` + `?err=请求参数无效` + 页壳** |
| `POST /admin/plugins/uninstall`（缺 id） | `400` + `ErrUninstallFailed` | **`200` + `?err=请求参数无效` + 页壳** |
| `POST /admin/plugins/install`（无文件） | `400` + `ErrInstallParse` | **`200` + `?err=没有收到插件包文件；浏览器不会重传已选文件，请重新选择文件后再提交`** |
| 点菜单「邮件活动」 | `?err=参数不合法` | **`?err=请先从活动列表选择一条活动，再查看它的报表。`** |

> 修复代理**额外发现 1 处**（我列的 7 处漏了 `PluginsInstall` 开头的未装配检查 `:81`），实际收口 **8 处**；
> 并新建 `plugin_err.go` 承载「白名单 + 归口 + 结构化日志 + 读侧收敛」三件套。

### ⚠️ 父 agent 的一处指令错误（已纠正，值得记录）

**我要求「把 `plugin_enums.go` 的常量值改成中文」是错的**，修复代理拒绝执行并给出实测依据，经我核实**它是对的**：

`pkg/response.IsBusinessError` 的判据只有**两种形态**（源码 `pkg/response/response.go:90/100/124`）：
```go
var businessErrKey      = regexp.MustCompile(`^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*\.[a-zA-Z0-9_]+(\|.*)?$`)
var businessErrConstant = regexp.MustCompile(`^(Err|Msg)[A-Z][A-Za-z0-9]*$`)
```
**纯中文短文案两个正则都不匹配**。代理实测：把 14 个常量值换成中文后 `go test ./pkg/response/` 报
14 条「不命中任何一层判据」；后果是 `plugin_handle.go:42/68` 的 `ErrorAuto` 把业务错误从 **400 + 业务文案**
降级成 **500 + 通用文案**，并让 `TestIsBusinessErrorCoversAllModuleEnums` 变红。
代理验证后**已还原**（md5 一致、`git diff` 无该文件）。

**且 `pkg/response` 的注释（`:94-96`）明确写着**：「其余模块仍是『常量名即消息』——
media 的 `ErrAttachmentNotFound = "ErrAttachmentNotFound"`」→ **plugin 遵循的是项目既定约定**。

**用户原先看到 `ErrInstallParse` 的根因是「出口形态」（`c.String` 直出、不经翻译层），不是常量值。**
出口改经 `shell.TranslateFor` 取词后，常量名照样渲染成中文 —— 修复代理的处理正确。

**这暴露出一处文档冲突（待修）**：`internal/module/CLAUDE.md` 的 enums 一节写
「未接好 `i18n` 时，`ErrXxx` / `MsgXxx` 直接等于**中文常量**」，
而 `pkg/response` 的设计要求未迁模块用 **`Err|Msg` 名形态**（否则 `ErrorAuto` 判不出业务错误）。
两处必须对齐 —— **建议改 `internal/module/CLAUDE.md`**（它是被 `pkg/response` 的实现推翻的那一方）。

### 不要动的地方（审核代理确认正确）

`masterdata_changes` 的 `.tabs`（全站多视图样板）；`permissions` 的筛选+分页（本域唯一正确的服务端筛选样板，批 B 照抄它）；
6 页的行内表单外置（`form="id"` + 表格外 `.hidden-form`，`tdForms=0`）；`partials` 三片段契约；
`data-drawer-*` 机制；`role_permissions` 的父子联动 JS（只向上补齐、取消父项连坐）；
`datarule_edit` 的 `hx-post` 域切换；`datarules` 两处 `.help` 悬浮说明；
`login` 的就近错误提示；mail 域的破坏性确认文案；`mail_automation_canvas` 无 `.page-head`（刻意例外）；
**plugin 模块的 enums 常量名形态**（见上「父 agent 的一处指令错误」—— 那是项目既定约定，不要改）。

### 方法论（`02-J` §H7~H12 已全部写入）

- **H7** 侧栏 `.pin-hint` 污染 `hintReal`（假报 0.127）；
- **H8** 客户端过滤 + 批量选择**必须实测两个方向**（同一段代码里一个对一个错）；
- **H9** 服务端筛选可能是**死控件**（UI 完整、按钮可点、无报错，handler 从不读 query）；
- **H10**「同一函数里修一半」是常见遗漏形态（`DatarulesEditPage` 的缺 id 分支修了、id 不存在分支还是 404 裸文本）；
- **H11 · `data-depth` 起始值是 1 不是 0** —— 权限树顶层是 `depth=1`（分布 1:8 / 2:43 / 3:65），写死 `!== 0` 会**静默不折叠**（`hiddenRows:0` 却无任何报错）。判据：**先求 rows 里的最小 depth 再折叠，不要写死数字**。
- **H12 · 验证失败出口前应先证明请求到达 handler** —— 401/403/404 都属中间件/路由层。批A 的代理被这一点误导过两次（把 `departments.html` 顶层 DOM 无 csrf 误报为缺陷，实际抽屉表单有 `csrfLen:64`）。

## 0.6 本轮（第七批）：admin 六页装载失败降级渲染 + 邮件菜单死链收口

### 0.6.1 列表装载失败不再拿走整个页面

六个管理页（管理员 / 角色 / 权限点 / 菜单 / 部门 / 数据规则）在**列表查询失败**时是
`c.String(500, "MsgAdminGenericFailed")`：浏览器里没有页面，只有一块写着 i18n key 的裸文本 ——
侧边栏、筛选框、分页全部消失，运营看到的是 key 而不是人话，也无从判断「是我筛错了还是系统坏了」。
列表查询失败是**服务端**问题，不该把页面本身一起拿走。

| 项 | 改动 | 验证 |
|---|---|---|
| 归口 helper | `admin_err.go` 新增 `adminErrOrLoad(c, err)`：装载失败优先于 `?err=` 回带；文案与 `adminErrParam` 同源（同一份 `AdminFacingMessages` 白名单 + 同一处带 `user_id` 的结构化日志） | — |
| 六页降级 | `admin_pages_handle.go` 六处 `c.String(500, …)` → 空结果（`&dto.XxxListResp{}` / `nil` 树）+ `"Err": adminErrOrLoad(c, err)`，页面照常渲染 | 新增 2 条 feature 用例 |
| 死常量清理 | `pagesMsgAdminGenericFailed` / `pagesMsgRuleConfigInvalid` 删除（改后无引用） | `go build ./...` + `go vet` 干净 |

**反向验证**（证明用例没有把断言写软）：临时把角色页改回 `c.String(500, "MsgAdminGenericFailed")`，
两条用例立刻变红且报的就是原缺陷形态 ——
`装载失败应降级渲染页面（200），got 500 body=MsgAdminGenericFailed` → 随后还原。

新增用例（`public/test/admin/feature/admin_page_err_render_test.go`）：

| 用例 | 断言 |
|---|---|
| `TestAdminRolesPageListLoadFailureKeepsPage` | 状态码 200 + 页面骨架仍在（筛选框 `name="keyword"` 存在）+ 提示条是 `ErrInternal` 归口文案（**不是** 驱动原文、不是 i18n key）+ 整页无驱动原文片段 |
| `TestAdminRolesPageListLoadFailureBeatsStaleErr` | 手上带一条**合法**的旧 `?err=` 时，装载失败仍盖过它（当前这次请求真实发生的事优先） |

泄漏断言用同包既有的 `pageLeakMarkers` / `assertPageNoLeak`，**不用** `leakMarkers` ——
后者是照 JSON 响应体设计的，其中 `sort_order` / `cannot unmarshal` 恰好也是**正常页面**的一部分
（角色页就有 `name="sort_order"` 输入框），对整页断言会误报成泄漏。

### 0.6.2 邮件菜单死链收口（迁移 401）

`sys_menus` 的「邮件活动」指向 `/admin/mail/campaign` —— 那是**单条活动的报表页**，
以 `?id=` 为必需参数，而菜单项不带参数：运营点进来必然落到引导重定向，一个点不通的菜单项。

| 项 | 内容 |
|---|---|
| 修法 | `public/migrations/401_hide_mail_campaign_menu.sql` —— `is_hidden = 1`（**保留行不删**，对齐 224 第 7 段范式：角色授权按 `menu_id` 收集 `permission_code`，删行会让已授权角色静默缩权） |
| 为什么不少权限 | 该项的 `permission_code` 与「邮件营销」**完全相同**，两条菜单登记同一个码；保留其中一条即可 |
| 报表入口仍在 | `/admin/mail/marketing` 的活动列表每行都有「活动名 → `?id=N`」与「报表」按钮 |
| 注册 | `register_core.go` 追加 `register(Migration{...})`，`CheckSQL` 表达「已完成」（目标行 `is_hidden=0` 计数为 0 即跳过），并按 `migrator_test.go` 要求带 `CAST(? AS text) IS NOT NULL` |
| 应用与验证 | `run_migrations: false`，按迁移器的同一份 SQL 手动执行：`is_hidden` 0→1（`UPDATE 1`）；独立 Chrome 实测侧栏 `hasCampaign=false` / `hasMarketing=true` / `hasAutomation=true`；不带 id 访问 `/admin/mail/campaign` 仍落到营销页并显示引导 |

**顺带发现的悬置项**：该引导走 `?err=` 通道时被套上了邮件域的提示前缀，页面上显示成
「**上一次操作未完成**：请先从活动列表选择一条活动，再查看它的报表。」——
前缀说的是「上一次操作」，而这次根本没有上一次操作。同一形状的前缀（`*.lastError` =「上一次操作未完成：」）
遍布各模块列表页，凡 `?err=` 承载**引导类**而非**回执类**文案时都会错位。修它需要给通知通道分类型
（回执 vs 引导），属跨模块改动，**列入待决**。

## 0.7 本轮附带判定（只读分析，结论用于定范围）

### 0.7.1 「空态吃掉表头」全站清点与门禁

静态扫描用**栈解析** `{{if}}/{{range}}/{{block}}` … `{{else}}/{{end}}`（纯文本正则会因嵌套与
一行多 token 漏判），命中 **57 处 / 36 个文件**，比上一轮的 43 处多 —— 多出来的是
`articles.html` / `content_templates.html` / `orders.html` 这几个正则跨不过嵌套块的文件。

| 项 | 内容 |
|---|---|
| 判据 | if 段渲染 `.empty-state` ∧ if 段无 `<table` ∧ if 段无 `colspan=` ∧ else 段有 `<table>` |
| 为什么带 `colspan=` | 那是**已正确形态的识别特征**（空态进 `<tbody>` 的 `<td colspan="列数">` 行）；带它之后 `permissions.html` / `administrators.html` / `roles.html` / `datarules.html` 这些已修样板不再误报 |
| 门禁 | `scripts/check-empty-state-table-head.sh`（命中即失败、豁免条目不再命中即失败，与 `check-no-internal-error-leak.sh` 同范式） |
| 豁免清单 | `scripts/empty-state-table-head-allow.txt`（当前为空） |
| 校准证据 | 改后四页空态实测 `thead=1` 且 `colspan` 与 `theadCols` 精确相等（8/7/9/7）；未修的 `/admin/departments` 空态 `tables=0` |

### 0.7.2 裸文本失败出口：判定与范围

`internal/module/**/inbound/http` 里 `c.String(` 共 113 处（不含测试）。按「该文件是否注册
`/admin` 页面路由」分两类：

| 类别 | 位置 | 判定 |
|---|---|---|
| **页面失败出口**（真缺陷） | `project` 20 处（`theme_admin_pages.go` 7 / `site_settings_admin_pages.go` 7 / `theme_settings_admin_pages.go` 6）**→ 已收口**；`block` 3、`page` 3、`inventory` 2、`order` 2 | 无页壳、文案是裸中文或裸 i18n key。与 admin 六页同型（本轮已按 `303 + ?err=` + 读侧受控收口） |

> **project 域已收口（2026-09）**：20 处 `c.String` 之外还发现第 21 处
> （`buildSiteSettingsData` 写 500 JSON 响应后再 `c.HTML` 渲染同一请求），一并修掉。
> 修法是「判定表只有一份、JSON 出口与页面出口共用」（`project_err.go`），
> 读侧走整体白名单而非 `strings.Contains`。**本表其余域的处数已过期** ——
> 收口后重测全站：`c.String(http.Status*)` 共 80 处，减去 1 处正常返回 HTML 的
> `StatusOK`，**失败出口 79 处**；其中 12 个注册了 `/admin/*` 页面路由的文件里
> 还有 **14 处是真缺陷**（与 project 同型，含 5 处裸归口 key），其余 65 处是接口/片段出口
> （workbench 51 / runtimefragment 8 / admin dev_login 4 / mail 退订页 2）——
> `runtimefragment` 里有 **2 处 `perr.Error()` 真泄漏**（访问面、公开可达）。
| **片段/接口出口**（非本轮判据） | `workbench` 51 处、`runtimefragment` 8 处、`mail_tracking` 3 处 | 这些是被 htmx / fetch 消费的接口响应。**用户可见**（`admin.js:317` 监听 `htmx:responseError` 并把失败报到通知里），但形态是「接口错误文案」而非「脱离页壳的页面响应」—— 它们的 `project` 是文案未走 `enums`/i18n，属规范问题（P2），与「页面失败出口」不是同一条判据 |
| 历史注释 | `admin_pages_handle.go` 3 处 | 只是注释里提到的旧写法，**该文件已彻底收口** |

### 0.7.3 决定不做的一项：错误提示前缀错位

27 个模板的 `?err=` 提示条统一加「上一次操作未完成：」前缀。该通道同时承载
**引导类**文案（缺参引导，如「请先从商品列表选择一件商品…」），读起来是
「上一次操作未完成：请先从商品列表选择一件商品…」—— 前缀说的是「上一次操作」，
而这次根本没有上一次操作。

**本轮判定：不改**，理由三条：
1. 前缀在**主路径**（写操作失败回跳列表页）上是准确的，改中性文案会让主路径变模糊；
2. 引导类触发面极小（`/admin/mail/campaign` 的菜单项已由迁移 401 隐藏；
   `/admin/products/template` 的菜单入口已改为带引导回带），只剩手输 URL；
3. 真正该做的不是修饰文案，而是**消除缺参入口本身** —— `/admin/products/template`
   缺 `product` 时应**就地渲染商品选择**（页面已有工程选择器，缺的是商品维度），
   而不是把用户弹回列表。这才是「菜单项成为可用入口」的正解，列入后续批次。

## 0.8 空态表头全站收口（本批主体）

静态门禁 `scripts/check-empty-state-table-head.sh` 命中 **57 处 / 36 文件**，按域分四路修复，
每一路都由父 agent 用独立 Chrome 实例复验（不采信子代理自报）：

| 批 | 域 | 文件数 | 父 agent 复验读数（空态 `thead` / `colspan`） |
|---|---|---|---|
| 管理域 | departments / menus / i18n / navigations / dashboard | 5 | departments（真 0 行）与 i18n（零写库构造）`thead=1`、`colspan` 与列数精确相等（7/7）；menus 117 行、navigations 12 行、dashboard（`/admin`）8 行不变 |
| 商品库存域 | products / attributes / categories / brands / tags / detail_template / inventory / warehouses / reasons / sources / purchases | 11 | sources 空态/筛选态、inventory 空态、warehouses 全部 `thead=1`；有数据页行数与列数不变、`ovfX=0` |
| 跨域补漏 | articles / content_templates / orders / page_translations / product_translations / site_slots | 6 | content_templates 空态 `thead=1 cols=8 colspan=8`、site_slots `thead=1 cols=4 colspan=4`、orders `thead=1 cols=8 colspan=8`、articles 有数据 50 行 ×8 列 |
| 其它模块域 | coupons / customers / returns / blocks / pages / analytics / theme / plugins / masterdata_changes / page_redirects / product_detail / product_pricing / mail* | 17 | 完成 —— 父 agent 复验：analytics 5 表（`thead=5`、rows 5/14/2/2/2、cols 3/3/3/3/3）、mail 2 表（rows 1/4）、masterdata 2 表（rows 20/50）、coupons / customers / page_redirects 空态均 `thead=1`；`ovfX` 全 0 |

**收尾读数**：`bash scripts/check-empty-state-table-head.sh` → `✓ 未发现「空态吃掉表头」的列表页（已扫描 74 个后台模板；8 个已登记的豁免仍待接手）`，exit 0。
`go build ./...`、`go test ./internal/templates/`、`./public/migrations/`、`./public/test/admin/...` 全绿。

### 0.8.0 八条豁免的类别（不是「放过」，是判据对它们不适用）

| 类别 | 文件 | 为什么表头不该出现 |
|---|---|---|
| 整页前置引导（无站点工程） | settings / navigations / blocks / theme | 没有工程时整页只剩一张引导卡（`theme` 连 `page-head` 都不渲染），页面此时没有列表语义 |
| 整页异常 / 缺参引导 | product_edit、product_detail（商品不存在）、product_detail_template（能力未装配） | 主对象取不到，页面所有列表都无从渲染 |
| `{{else}}` 段不是列表 | product_pricing（`{{if len(.History) == 0}}`） | else 段是**逐批 `<details>` 披露块**，其中的 `<table>` 是某一批的下钻内容，不是列表本身 |

每条都写明了「本文件真正的列表空态已按样板修复」，这样豁免不会把该文件的列表缺陷一起藏起来。

### 0.8.1 门禁脚本自身的一个 bug（已修）

判据用栈解析 Jet 的 `{{if}}/{{range}}/{{block}}` … `{{else}}/{{end}}`。**注释里的字面 token 会把栈带偏**：
某代理在注释里写了「表头在 `{{range}}` 里提不出来」，`TOKEN` 正则把注释里的 `{{range}}` 当成真 token
压栈，此后每个 `{{end}}` 都少配一层、`{{else}}` 配到伪节点上 —— 实测结果是**同一个文件先被误报、
另一个文件反而侥幸漏报**（`product_translations` 误报 `:80`，`page_translations` 漏报）。

修法：`strip_comments()` 在配对前把 `{* … *}` 换成**等长空白并保留换行**（行号与原文一一对应，
报错行号仍可直接定位），注释里的 HTML 关键字也不再参与判定。修完命中 12 处 → **10 处**，
且 `mail_marketing.html` 的报错行从 `:97` 变成 `:173`（配平修正后的准确位置）。

这条是「**判据工具本身也会犯错**」的实例：门禁绿不等于没有缺陷，门禁红也要先看它是不是读错了地方。

### 0.8.2 迁移 402 的落库真相（子代理结论有误，父 agent 已纠正）

商品库存域那一路新建了 `402_i18n_product_inventory_empty.sql`（7 key × 2 语言，`ON CONFLICT DO NOTHING`）
并报告「已随服务重启落库、页面显示库值」。**实测不成立**：本地 `config.yaml` 的 `run_migrations: false`，
迁移不会自动跑；按 7 个精确 key 查库返回空（该代理很可能是用 `LIKE '%inventory%'` 查的，
命中的全是 191 等旧 seed 的行，把「库里有很多 inventory 词条」读成了「我这批已落库」）。

影响是**有限的**：模板里的中文是 `t()` 兜底，且兜底值与词条值逐字相同，所以中文界面看不出差异；
真正的差异是**英文界面回落中文**、以及 i18n 词条页看不到这 7 个 key。

处理：按迁移器的同一份语句手动应用（`INSERT 0 14`，zh-CN/en-US 各 7），幂等重放 `INSERT 0 0`，
`ConditionSQL` 判定返回 1（迁移器会跳过）—— 与 401 同一套做法。
**教训**：子代理报告「已落库 / 已生效」时必须用**精确 key**回查，别用模糊匹配自己给自己作证。

### 0.8.3 长期守卫与豁免

| 项 | 内容 |
|---|---|
| 渲染级守卫 | `internal/templates/admin_list_empty_state_test.go` —— 覆盖运行期构造不到空态的页面（menus / navigations / dashboard），并钉住 **colspan 必须等于列数**（静态门禁管不到这一条）。反向验证：把 departments 的 colspan 从 7 改成 8 → 立即报 `期望 colspan="7" 未出现（空态行会与表头对不齐）`，md5 确认还原无误 |
| 豁免清单 | `scripts/empty-state-table-head-allow.txt` 4 条，全是「整页引导态」：settings（无工程）、product_edit（商品不存在）、navigations（无工程）、product_detail_template（能力未装配）。每条写明「为何这一页此时没有列表语义」 |
| 豁免的尺度 | 反面标准也写进清单头部：**空数据时用户仍需要知道有哪些列、能按什么筛的，表头就必须在** |

## 0.9 待办与提醒

> **本清单的条目状态已全量核对过（102 条）** —— 结论：**P0 五条全部完成**、
> P1 为 2✅/2⚠️/18❌；`02-M` 7✅/6❌；`02-O` 45 条为 7✅/4⚠️/34❌。
> 另有「未完成按主题聚类」的排期输入、**2 条应关闭的失效条目**（P2-9 / P2-16），
> 以及本节所有行号**已整体失效**的说明（引用请按语义定位）。
>
> **「失败出口收口」已全站铺开并完成第一批**：跨 9 个域共修 42 处 + 判定不改 4 处（有据）。
> 该批遗留按风险排序：① 261 条 enums key 词条缺失（最大的单一来源）
> ② order 域 `?err=` 通道渲染裸 key ③ workbench 批 1/批 4（前置已就绪）④ 门禁扩围。

> **项目域页面错误出口已收口并验证** —— 含修法判据、6 条守卫测试、浏览器实测证据，
> 以及**全站摸底（80 处待处理）与两个门禁盲区**。建议的后续顺序（按风险）：
> `runtimefragment` 的 2 处 `.Error()` → 门禁扩扫描范围 → `workbench` 51 处分类 → 门禁扩判据。

- **同形态出口未修 26 处**（见 §0.1 表更正）—— 需单开一批，逐页定回跳 URL + 登记读侧白名单；
- 另有 8+ 处 `c.String(500, <归口 key>)` 同判据命中，未实测触发路径；
- 批2 已知取舍：粘性保存条遮挡视口底部 96~112px（sticky 底栏固有代价，零遮挡需 JS 或主行动移到页头）；
- 批2 附带观察：工具条新插入的 Trix attachment figure **不带 `sre-attachment` 类**（hydrate 的旧内容带），新插入的表格/折叠块卡片缺虚线外观。与本次改动无关。

### 工作树状态提醒（非本轮引入）

`public/test/{analytics,blueprint,page,navigation}` 当前**编译失败**：仍用 5 参 `CompilePreview`，
而契约已是 6 参（含 `canvasFrames bool`）。这些文件不在任何代理的 diff 里，属工作树中**已有的未提交改动**。
连带后果：`public/test/navigation` / `public/test/page` 跑不起来，本轮修复无法在那两个包加回归用例。

### 内容域新增 P1（9 条，详见审核报告）

navigations 空态文案说「用上面的表单」但表单已进抽屉（G2 违规）；i18n 删除确认未说后果（3 处）；
blocks 第三段空态文案与事实矛盾（已有 3 个块却说「还没有区块」）；content_templates 无新建入口且页头缺 `.page-actions`；
两个翻译页空态无 `.empty-actions`；article_new 只有「保存并继续编辑」一个 submit；
media 行级操作只有「详情」（删除藏在详情抽屉里）；media 无空态（`.empty-state` 计数 0）。

## 1. P0 —— 干不了活（优先做）

| # | 位置 | 问题 | 修法 | 成本 |
|---|---|---|---|---|
| P0-1 | **裸文本脱页壳 6 处**：`theme_settings_admin_pages.go:148/153/278/283`、`theme_admin_pages.go:145/243`、`admin_pages_handle.go:896/901` | `c.String` 直出，DOM 仅 5 节点、无 `<title>`、无页壳。**其中 `datarule_edit:896` 裸出 i18n key `MsgFieldRequired`（未翻译的内部标识符）** | 改 `shell.PageErrorBadRequest(c, "<scene>", err)` —— **出口现成**（`internal/web/shell/errors.go:45`，`inventory_page_handle.go:81` 已在用） | 极低（逐处 1 行） |
| P0-2 | `admin_pages_handle.go:285-287` | **`role_permissions` 唯一静默失败**：缺 `role_id` → 302 回列表，无 `err=` | 改 302 带 `?err=`（该页 `RolesPage` **已渲染 `Err` 字段**，零改动成本） | 极低 |
| P0-3 | `administrators.html:46` / `departments.html:38` / `mail_marketing.html:98,161` | **`<table>` 被包进 `{{else}}`** → 空数据时**表头不渲染** | `<table>` 移出 `{{if}}`，空态进 `colspan` 行 | 低（4 处） |
| P0-4 | `sys_menus`「商品详情模板」 | **菜单死链**：不带 `?product=` 时静默 302 回商品列表 → UI 无路径可达 | 菜单改指向 `/admin/products`，行操作列加入口 | 低 |
| P0-5 | `mail_campaign` / `mail_automation_run` / `mail_automation_canvas` | 无 `id==0` 前置判定，靠下游失败兜底；**文案档位不一致**（canvas 给「流程不存在」，另两个给「参数不合法」） | 补前置判定 + 按「缺少/不存在/无权限」三档给文案 | 低 |

## 2. P1 —— 干活低效

### 2.1 模板/表单结构类

| # | 位置 | 问题 | 修法 |
|---|---|---|---|
| P1-1 | `inventory_reasons.html:66-84` | 9 行 × `<td><form>`，且无批量勾选。**同域 10 处同类页面全部合规** | 横抄 `product_attributes` 的 `form="attr-del-{ID}"` + 表格外 `.hidden-form`；补 `data-check-all` + `.bulk-bar` |
| P1-2 | `menus` / `i18n` / `navigations` / `permissions` | **N 行 = N 份抽屉 DOM**：menus 490KB/118 模板/4603 节点；i18n 247KB/51/1326；navigations 102KB/10/400；permissions 103KB/21 | 走 `02-I` §6.1 方案 A（`hx-get` 按需取片段）。**需先定架构** |
| P1-3 | `product_bundle.html` | 290KB、`bundle/save` 146 个字段名平铺 | 同上（已有 `_fragments/bundleConfigurator` 先例） |
| P1-4 | `blocks.html` | 三段表格列头不一致（6/6/8 列）；三层嵌套 `{{if}}` 排他条件 | 合并一张表 + 「类型」列徽章；嵌套条件自然消失 |
| P1-5 | `page_translations.html` | 8 张 thead 完全相同的表，5.17 屏 | 合并一张表 + 分组列，或 `.tabs` |
| P1-6 | `analytics` / `masterdata_changes` | 5 张 / 2 张同构表并列 | `.tabs`（组件已内置） |
| P1-7 | `product_tags.html` | 卡2「命中的商品」首屏是空壳卡 | 改 `.tabs` 第二面板；无选中时 `hidden` |

### 2.2 一页多职能（需拆页/折叠）

| # | 位置 | 问题 | 修法 |
|---|---|---|---|
| P1-8 | `article_edit.html` | **7 卡 / 13.75 屏**（全站最高） | 实时预览/SEO 评测/可视化编辑收进 `.tabs`；「发布」提到 `.page-actions` |
| P1-9 | `product_edit.html` | 5 卡 / 2.88 屏；与 `product_detail` 职责重叠；两张 list-card 无 `.card-title` | 变体·评分的**读**归 detail、**写**留 edit；SEO 检查与模板段折叠 |
| P1-10 | `blocks.html` | 只读「待重建影响面」卡占据列表上方主位（13 条清单顶掉 3 行数据） | 搬 `/admin/pages` 或降级为 `.help` + 徽章 |

### 2.3 列表可用性

| # | 位置 | 问题 | 修法 |
|---|---|---|---|
| P1-11 | `media.html` | 全站唯一未套骨架：无 `.page-head`/h1/`.filter-bar`；搜索框是裸 input；24 行勾选但**无全选、无 `.bulk-bar`、批量只有「下载」** | 套骨架 + 接 `admin.js` 成熟批量机制（注意 media 是 AJAX 渲染，需在 `renderGrid/renderTable` 后手动 `refresh(scope)`） |
| P1-12 | 11 页 | 缺 `.filter-bar`：`articles`(50 行!)、`pages`、`blocks`、`navigations`、`article_translations`、`navigation_translations`、`customer_detail`、`product_attributes`、`product_categories`、`product_brands`、`product_tags` | 抄 `content_templates.html:48` |
| P1-13 | 6 页 | **无分页**（handler 硬编码上限）：`administrators`(`Limit:100`)、`departments`、`datarules`、`menus`、`mail_marketing`、`mail` | 接 `partials/pagination.html` |
| P1-14 | `articles.html` | 50 篇上限、无分页，第 51 篇永远点不到 | 同上 |
| P1-15 | **11 页 / 19 处** | **空态无 `.empty-actions`**（覆盖面远超原估的 3 页） | 抄 `dashboard.html:67` / `plugins.html:29` / `mail.html` 的三段式 |
| P1-16 | `role_permissions.html` | 页高 **4.15 屏 / 962 节点**，逐节点平铺仅靠客户端折叠 | 默认折叠到目录层，或加 sticky「已勾选 N 项」摘要 |
| P1-17 | `plugins.html` | **4 张只读巡检表占主导**，把「安装插件」推到第 5 屏。**注意：不适用 tabs**（列结构不同），应**折叠** | 包进一个 `<details class="section-fold card">` |
| P1-18 | `site_slots.html:107` | 每行内嵌含 13 option 的 `<select>`，10 行 = 130 option 节点；下拉占位塞口径说明逐行重复 | `<select>` 移出 `.col-actions`；或 `hx-get` 按需加载 |

### 2.4 语义与控件

| # | 位置 | 问题 | 修法 |
|---|---|---|---|
| P1-19 | `page_redirects.html:101` | **写操作伪装成筛选栏**（`<form method=post action=create class="filter-bar">`，回车即创建、无确认） | 创建表单移出，改页头 `.page-actions` + 抽屉/模态 |
| P1-20 | `i18n.html` | 两个同名 `lang` 下拉（一个切界面语言、一个筛词条语言，选项集不同） | 界面语言切换移出筛选区 |
| P1-21 | `product_pricing.html` | 首屏 0 行数据 + 空态无主行动 + `.hint` 被当字段标签用 | 标签改 `.form-label`；空态补 `.empty-actions` |
| P1-22 | `product_categories.html` | 层级数据用平铺表格，层级只靠缩进字符串 | 改树表（复用 `partials/nav-nodes.html`） |

### 2.5 多表视图（tabs 适用性已甄别）

| 页面 | 表数 | 是否同一数据的多种切法 | 判定 |
|---|---|---|---|
| `analytics` | 5 | **是**（5 维度看同一份数据） | **应改 tabs** —— 照抄 `masterdata_changes.html:106`（全站唯一正确样板，勿自创） |
| `masterdata_changes` | 2 | 是 | **已用 tabs，正确样板，不要动** |
| `plugins` | 4 张巡检表 | **否**（列结构不同） | 应**折叠**，非 tabs |
| `page_translations` | 8 | 是（8 段结构完全相同的表） | 合并一张表 + 分组列 |

## 3. P2 —— 打磨

| # | 位置 | 问题 |
|---|---|---|
| P2-1 | `mail_marketing.html:95` / `mail_automation.html:136-139` | i18n 库值与模板默认值脱节，空态 title 与 desc **输出同一句话**。**同类缺陷第二次出现** —— 建议加「模板 default 与库值一致性」回归测试 |
| P2-2 | `inventory_sources.html:107` | 空态说「用上面的表单建一个」，但该页无新建表单（新建走抽屉）。**模板真源本身写错**（区别于 P0-1 的库值覆盖） |
| P2-3 | `products.html` 状态列 | 英文裸值 `published`（20 行），同页库存列已本地化 |
| P2-4 | `product_attributes.html:109` / `products.html:212` | 单元格输出句子：「4 个属性值」「4.5 · 12 条」 |
| P2-5 | `product_detail.html:164` vs `product_edit.html:223` | 同一张变体表列头不同（「库存明细」vs「操作」）；i18n key 与文案不符（`col.stockLink`） |
| P2-6 | `inventory.html:122` 等 3 处 | `.filter-bar` 承载两种语义（筛选栏 vs 标题行） |
| P2-7 | 多页 | 统计混进列表标题：「仓库列表（1）」「变动原因（9）」「优惠码（共 0 张）」（为空时不该显示） |
| P2-8 | `coupons.html:54/114` | 空数据时同屏两个「＋ 新建优惠码」按钮 |
| P2-9 | `plugins.html` | 「安装插件」是锚点占位 `href="#plugin-install"`，页头 `.page-actions` 为空 |
| P2-10 | 4 页 | SEO 字段缺 `data-counter` 字数提示（`product_edit` / `product_categories` / `product_brands` / `product_detail_template`） |
| P2-11 | `product_categories.html:105` | 空值写「未填」，同域其余页统一 `—` |
| P2-12 | `product_detail_template.html:151` | 列表展示内部「模板 id」（对外实体用 uuid 本就是为了不可枚举） |
| P2-13 | `site_slots.html` | 行内说明逐行重复（下拉占位符塞口径说明，10 行重复 10 次） |
| P2-14 | `content_templates.html` | `entityType` 下拉维度混装（结构模板 + 内容实体）→ 加 `<optgroup>` |
| P2-15 | `inventory_purchases.html:138` | 「货源」列两个值同格（货源名 + 类型标签） |
| P2-16 | `orders` / `returns` / `customers` | 空态无主行动（`coupons` 是正确样本） |
| P2-17 | `products.html:254` | **死模板** `tpl-product-create`（257 节点）仍在，注释 `inventory_warehouse_test.go:638` 也已过时 |

## 4. 待决策的两个架构问题

### 4.1 每行一份抽屉模板怎么瘦身（`02-I` §6.1）

影响 4 页（menus 490KB / i18n 247KB / navigations 102KB / permissions 103KB / product_bundle 290KB）。
**现状机制**：`ui/drawer.js:44` 克隆页面里已存在的 `<template>` —— 所以「每行一份」是架构必然。
**方案 A**（推荐）：`hx-get` 按需取片段。项目已有先例（`product_tags.html:123`、`_fragments/bundleConfigurator`）。

### 4.2 `CasbinMiddlewareForPathAs` 是否推广

已为 `/admin/articles/new` 引入。需确认：是否还有其它「页面入口复用 API 权限点但动词不一致」的场景。
当前 grep 全模块**只有这一处**（`pages.GET(...CasbinMiddlewareForPath...)`）。保持现状即可。

## 5. 执行顺序建议（按「用户受伤 ÷ 成本」）

```text
第 1 批（P0，纯 handler/模板分支，不碰架构）
  P0-1 裸文本 6 处 → shell.PageErrorBadRequest（出口现成）
  P0-2 role_permissions 静默 302 → 带 ?err=（该页已渲染 Err，零成本）
  P0-3 <table> 移出 {{if}}（4 处）
  P0-4 商品详情模板菜单死链
  P0-5 mail 三页补 id==0 判定 + 文案分档

第 2 批（P1，低成本语义修正）
  P1-15 空态补 .empty-actions（11 页 19 处）
  P1-1  inventory_reasons（横抄同域合规样本）
  P1-19 page_redirects 创建表单移位
  P1-20 i18n 双 lang 下拉
  P1-11 media 套骨架 + 批量
  P2-17 删死模板 tpl-product-create（连注释一起）

第 3 批（P1，列表可用性统一收口）
  P1-12 + P1-13 + P1-14 共 11 页补 .filter-bar / 分页

第 4 批（P1，拆页/视图改造）
  P1-5  page_translations 8 表合并
  P1-9  analytics 5 表改 tabs（照抄 masterdata_changes）
  P1-17 plugins 4 表折叠
  P1-16 role_permissions 默认折叠
  P1-8  article_edit 7 卡
  P1-9' product_edit 5 卡

第 5 批（需先定架构）
  P1-2 / P1-3 抽屉模板瘦身（02-I §6.1 方案 A）
```

## 5.1 可复用样板（照抄，不要自创）

| 场景 | 样板位置 | 说明 |
|---|---|---|
| **条件未满足的反馈** | `inventory_purchases.html:33` | disabled 主行动 + `title` 说明原因 + 空态 `.empty-actions` 指修复页。**全站最好** |
| **空态三段式** | `dashboard.html:67` / `plugins.html:29` / `mail.html` | title + desc（不重复标题）+ actions |
| **多视图切换** | `masterdata_changes.html:106` | `.tabs[data-tabs]` 完整 WAI-ARIA + 服务端渲染双面板 + 零请求 |
| **错误就地提示** | `login.html:121` | `.login-msg` + `role="alert" aria-live="polite"`（**全站唯一做对的错误就近提示**）|
| **行内表单外置** | `product_attributes.html` 等 10 处 | `form="attr-del-{ID}"` + 表格外 `.hidden-form` |
| **列表最优形态** | `products.html` | `data-check-item` + 表格外隐藏表单：43 forms / **0 tdForms** / **0 每行模板** |
| **带计数可点击徽章筛选** | `customers.html:67` | 链接带 `aria-current`，信息与操作合一 |
| **内容域骨架** | `content_templates.html` | 有 filter-bar/checkAll/col-actions/首屏 5 行 |

## 5.2 不要动的地方（已确认正确）

- `masterdata_changes` 的 tabs（多表页正确样板）
- `login` 的 `.login-msg` 就地反馈
- `mail_automation_canvas` 无 `.page-head`（画布类页面刻意例外，注释已说明理由）
- `login` 无 `.page-head`（未认证页面不应渲染导航）
- `settings` 的语言折叠区默认展开（注释说明充分，是刻意设计）

## 6. 每页改造的固定流程

1. **度量现状**（`02-J` §F1 修正口径 + §H 断言规则）；
2. **读源码结构**（**注意 §H1/H2：解析 form 边界、跟进 include**）；
3. **对账判据**（`02-J` A–G + `02-H` §5 验收表）；
4. **一次一页**，改完立即截图核对（视觉检查是技能的一部分，不是可选项）；
5. **跑测试**：`go test ./internal/templates/... ./internal/routers/ ./public/migrations/`；
6. **改断言**：期望标记随功能移走 → 改断言并注明；标记无故消失 → 实现有 bug；
7. **更新本文与 `02-I` §5 度量表**。
