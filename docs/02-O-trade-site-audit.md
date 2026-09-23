# 交易域 + 站点域 操作逻辑审核（12 页 / 45 个任务）

> **归档说明**：本报告原为 `/tmp/verify/lcs-trade/REPORT.md`（会话期临时落点，随时可能被清理），
> 2026-09 由主会话逐字归档到仓库，内容未改一字。判据来源、检测环境与复现命令见正文头部与文末附录。
>
> 覆盖：orders / returns / coupons / customers / customer_detail / dashboard / theme / theme_settings / settings /
> seo / analytics / site_slots。**截至归档时这 45 个任务一个都没有实施**（本轮只做了「空态吃掉表头」那一类）。
> theme_settings 是本域的 P0 集中页（2.21 屏、0 行数据）。
>
> 与其它审核报告的关系：第四轮逐页改造见 `02-J-admin-page-refit-round4.md`（55 页四维评审）、
> 商品与库存域见 `02-M-product-inventory-audit.md`、逐域修复记录见 `02-L-admin-rework-worklist.md`。


> 检测环境：独立 Chrome 实例（`/tmp/verify/harness.js` + 自建 `lcs-trade/{cases,dump,extra}.js`，未改 harness.js）
> 视口 1646×908。每页读数前断言 `location.pathname`；被会话抢占的读数已丢弃重测（customers 重测 1 次）。
> **未修改任何项目文件。**
> 判据来源：`02-I` §5.2.1/§5.2.5、`02-J` A–G + §H1–H10、`02-K` §1–§8、`admin-ui-logic` §1–§9。

---

## 0. 本轮结论速览

| 页 | 屏数 | 行数/首屏 | 筛栏 | 写入口 | 空态三段 | 破坏确认说后果 | 判定 |
|---|---|---|---|---|---|---|---|
| orders | 1.00 | 0/0 | ✓ 生效 | ✗ 无 | ✗ 缺 actions | — | 有 P0（G2 文案） |
| returns | 1.00 | 0/0 | ✓ 生效 | ✗ 无（合理） | ✗ 缺 actions | — | P1 |
| coupons | 1.00 | 0/0 | ✓ 生效 | ✓ 页头+空态 | ✓ **正确样本** | ✓ 说清 | P1（控件选型） |
| customers | 1.00 | 0/0 | ✓ 生效 | ✗ 不能新建 | ✗ 死主行动 | ✓ 批量说清 | P1 |
| customer_detail | — | — | ✗ | ✓ | ✓ | — | **仅源码审查** |
| dashboard | 1.00 | 8/8 | ✗ | ✓ | ✓ | — | 1×P2 |
| theme | 1.00 | 1/1 | ✗ | ✓ | ✗ 缺 desc | ✗ 没说后果 | P1 |
| theme_settings | **2.21** | 0 | ✗ | ✓ | — | — | **P0 集中页** |
| settings | 1.32 | 5/**0** | ✗ | ✓ | ✓ | — | 记录需更正 |
| seo | 1.05 | 10/9 | ✗ | ✓ | ✗ 缺 2 段 | — | P1 |
| analytics | **2.17** | 25/8 | ✓ 生效 | ✗ 只读 | ✗ 6 处 | — | P1 |
| site_slots | 1.00 | 10/7 | ✗ | ✓ | ✗ 缺 title | ✓ 说清 | P1 |

**筛选栏死控件实测结论（§H9 判据）：本域 5 个筛选栏全部不是死控件** —— 回显 + handler 读 query + SQL 进条件三项齐（证据见各页任务单）。`analytics` 是本域唯一能实测「行数变化」的筛选栏（25→0）。

---

## 1. orders（/admin/orders，模板 `internal/templates/admin/orders.html`）

**度量**：`screens=1 hintReal=0 cards=1 tables=0 rows=0 visRows=0 tableAt=null forms=2 tdForms=0 templates=0(0) htmlKB=20 elements=249 checkAll=0 colActions=0 filterBar=1 pageHead=1 overflowX=0`
**筛选实测**：`?keyword=ZZPROBEXYZZ` → 输入回显 ✓；`?paymentMethod=zzprobepay` → 回显 ✓；`?status=pending` → 「✓ 待付款」高亮 ✓。源码：`order_page_handle.go:163` 读 query、`order_model.go:283/288/297-299` 条件进 SQL（`status = ?` / `payment_method = ?` / `ILIKE`）→ **非死控件**。行数变化无法实测（`orders` 表 0 行）。
**职能**：订单列表（徽章筛选）+ 同页展开详情 + 状态流转 / 取消 / 退款 / 后台备注。

**任务 1：删掉空态里承诺了却不存在的「后台代客建单」，或补上该入口**
- 级别：**P0**
- 位置：`internal/templates/admin/orders.html:100`
- 现状：空态 desc 原文「台前下单后（**或后台代客建单**）就会出现在这里。」实测 `drawers=0, templates=0, postForms=0, createLinks=[]`、`.page-actions` 内 HTML 为纯空白 → 全页无任何建单入口；`order` 模块也无 create 页面路由。
- 问题：运营读到「后台可以代客建单」后会去找入口，翻遍页头、列表、行操作都找不到 → 转而怀疑权限或版本，或去商品页/购物车页乱试。**这是 G2 违规**（文案提到的控件必须真的存在）。
- 改法：二选一 —— ① 删掉括注（`orders.html:100` 改为「台前下单后就会出现在这里。…」）；② 若代客建单是规划能力，改成明确的前置说明（「后台暂不支持代客建单」），不要写成已存在。**推荐 ①**（当前无该能力，写清楚比留暗示好）。
- 验证：`curl -s http://127.0.0.1:8080/admin/orders | grep -c 代客建单` 应为 0；或浏览器 `document.querySelector('.empty-desc').innerText` 不含该词。
- 风险：该文案有 i18n key（`admin.orders.list.empty_tail`）→ **改模板兜底必须同批写迁移**（`ON CONFLICT DO NOTHING` 的 seed 不会覆盖库值，见 `02-I` §5.2 #1 的教训）。先查库值：若库里已是同一句，需 `UPDATE` 迁移。

**任务 2：给「有工程无订单」空态补主行动（并统一两个空态分支）**
- 级别：P1
- 位置：`internal/templates/admin/orders.html:99-101`
- 现状：`emptyStates[0] = {title:"这个工程还没有订单", desc:"…", hasActions:false}`；而**无工程分支**（`:95-97`）有 `.empty-actions`（→ `/admin/pages`）。同页两个空态分支，一个有主行动一个没有。
- 问题：订单不能后台新建 → 确实没有「新建」动作。但空态 desc 说「检查上面的筛选条件是不是过窄了」，却**不给「清掉筛选」按钮**（`returns.html:286` 给了）→ 用户要自己找到筛选栏、手动清空关键词框再点「筛选」，多 3 步。
- 改法：抄 `returns.html:285-287` —— 在 `{{if .FilterKeyword != "" || .FilterPayment != "" || .FilterStatus != ""}}` 时渲染 `<div class="empty-actions"><a class="btn" href="/admin/orders?project={{selectedProject}}">清掉筛选</a></div>`。文案复用 `admin.orders.filter.reset`（「重置」，已有 key）。
- 验证：`/admin/orders?keyword=ZZPROBEXYZZ` → `document.querySelectorAll('.empty-actions a').length` 应为 1 且 `href` 不含 `keyword`；`/admin/orders` 裸访问应为 0。
- 风险：`href` 必须带上 `project`（`selectedProject`），否则多工程下会掉回默认工程 —— `returns.html:260` 的重置链接就是这个形状，照抄。

**任务 3：空态时保留表头（表格移出 `{{if}}`）**
- 级别：P1
- 位置：`internal/templates/admin/orders.html:92`（`{{if len(rows) == 0}}`）/ `:103`（`{{else}}`，`<form>`+`<table>` 在此分支内）
- 现状：`tables=0`（空数据时整张表不渲染）→ 用户看不到「订单号 / 状态 / 客户 / 金额 / 支付方式 / 下单时间 / 操作」有哪些列。
- 问题：这是 `02-L` P0-3 的**同族第 5 个案例**（原记 administrators/departments/mail_marketing 4 处）。空态删表头 = 用户不知道这个列表能给他什么。
- 改法：`<table>` 移出 `{{if}}`，空态改成 `<tbody>` 内的 `<tr><td colspan="8" class="empty-state">…</td></tr>`；抄 `02-L` §0.5 批 D 的四页做法（`permissions` 已实测 `tablePresent:true, theadCols:9`）。注意本页 `<form>` 包表格，移出时 form 边界要跟着调（`form` 仍须包住 `<table>`）。
- 验证：`/admin/orders?keyword=ZZPROBEXYZZ` → `document.querySelectorAll('table.data-table thead th').length` 应为 **8**（与表头列数一致）；`document.querySelector('td.empty-state, tbody .empty-state')` 存在。
- 风险：`colspan` 必须等于表头列数（8）；`data-check-all` 在空态下仍在 `<thead>` 里 → 需确认 `admin.js` 对 0 行不报错（`refresh()` 分母为 0，参考 `02-L` 批 C 已修的分母逻辑）。

**任务 4：非法 `status` 值不再静默进 SQL，给出与 returns 一致的提示**
- 级别：P2
- 位置：`internal/module/order/inbound/http/order_page_handle.go:163`（读 status）+ `internal/templates/admin/orders.html:99-100`
- 现状：`?status=zzbogus` → 7 个徽章**全部不高亮**（含「全部」）、空态与基线**逐字相同**、无任何提示；而 `?status=pending` 正常高亮。同域 `returns` 对同场景给了明确提示（`returns.html:283-284`：「当前还带着状态筛选「zzbogus」—— 点上面的「全部」可以清掉。」）。
- 问题：用户从别处带来的链接（或手改 URL）带一个失效状态 → 页面看起来「就是没有订单」，且徽章全灰让人以为筛选栏坏了。**域内实现漂移**：returns 做对了，orders 没有。
- 改法：抄 `returns.html:283-284` 的写法 —— 在空态 desc 里追加「当前还带着状态筛选「{{.FilterStatus}}」—— 点上面的「全部」可以清掉。」；`FilterStatus` 已在 `order_page_handle.go:219` 注入，模板直接可用。
- 验证：`/admin/orders?status=zzbogus` → `document.querySelector('.empty-desc').innerText` 应含 `zzbogus`。
- 风险：新增文案需要 i18n key（`admin.orders.list.status_filter_lead/tail`），**同批 seed**（`02-H` §4：新增 `Msg*` 必须同批 seed，否则顶栏露 key）。

**任务 5：`?orderId=` 指向不存在的单时给一句提示**
- 级别：P2
- 位置：`internal/module/order/inbound/http/order_page_handle.go`（`orderQueryID` 解析后查不到即不渲染详情）
- 现状：`/admin/orders?orderId=00000000-0000-0000-0000-000000000000` → 页面与基线**完全一致**（无 `?err=`、无 badge、`HasDetail=false`）。
- 问题：从收藏夹/历史链接进入（订单已被删）→ 用户以为页面加载失败或自己点错了，**没有任何可依据的反馈**。
- 改法：查不到时注入 `Err`（「订单不存在或已被删除」）走页面顶部已有的 `{{if .Err}}` 槽位（`orders.html:47-49`）。**该槽位已现成，零模板改动**。
- 验证：同上 URL → `document.querySelector('.badge-warning').innerText` 应含「不存在」。
- 风险：文案要过 `orderFacingError` 白名单（`order_page_query.go:48-60`）—— 不要直接拼 `err.Error()`（`AGENTS.md` 硬约束：禁止直出内部错误）。

---

## 2. returns（/admin/returns，模板 `internal/templates/admin/returns.html`）

**度量**：`screens=1 hintReal=0 cards=1 tables=0 rows=0 visRows=0 forms=2 tdForms=0 templates=0 htmlKB=20 elements=253 checkAll=0 colActions=0 filterBar=1 pageHead=1 overflowX=0`
**筛选实测**：`?keyword=ZZPROBEXYZZ` 回显 ✓（`return_page_handle.go:117` 读 query）；`?status=zzbogus` → 徽章全不亮 **但空态 desc 明确说出当前筛选 + 一键清除** ✓。行数变化无法实测（表 0 行）。
**职能**：退货申请列表（RMA）+ 审核 → 先入库后退款 → 补退款。

**任务 1：空态时保留表头**
- 级别：P1
- 位置：`internal/templates/admin/returns.html:272`（`{{if len(rows) == 0}}`）/ `:290`（`{{else}}`，`<form>`+`<table>` 在内）
- 现状：`tables=0`。表头 8 列（勾选/退货单号/订单号/客户/状态/退款金额/申请时间/操作）在空数据时全不可见。
- 问题：同 orders 任务 3 —— `02-L` P0-3 同族第 6 个案例。
- 改法：`<table>` 移出 `{{if}}`，空态进 `colspan="8"` 行（同 orders 任务 3）。
- 验证：`/admin/returns?keyword=ZZPROBEXYZZ` → `thead th` 数 = 8。
- 风险：本页 `<form>` 从 `:295` 开到 `:337`，表格移出时 form 边界需同步；`data-check-all` 与 `data-check-item` 必须仍同属该 form（否则 `checkScopeOk` 失效，`02-J` §H3）。

**任务 2：详情卡的 `{{detail.Note}}` 正文说明进 `.help` 或降为字段**
- 级别：P2
- 位置：`internal/templates/admin/returns.html:138`（`<p class="hint">{{detail.Note}}</p>`）；数据源 `internal/module/order/inbound/http/return_page_query.go:90-103`（`returnStatusNote`）
- 现状：`returnStatusNote` 按状态返回整句操作说明，例如待审核态：「客户已提交，等待审核：同意后可以勾「立即完成入库 + 退款」（货已经在手上时），也可以等货到仓库再点确认收货。」——**这是一段操作指引，不是数据**，平铺在「操作」标题下方正文位置。
- 问题：违反 `admin-ui-logic` §2.1（页面正文不放说明文字）。对已经会用的人是每次访问都付的噪声；它紧贴的「操作」区块本身已有 4 个 `.help` 悬浮（`:133-136`），说明位置不统一。
- 改法：把 6 个状态的说明合并进「操作」标题的 `.help-pop`（`:135` 已有 `<span class="help-pop" role="tooltip">`，把 `returnStatusNote` 的 6 句按状态列进去），正文只留操作表单。**注意**：状态相关文案随之变成「所有状态的说明都在悬浮里」→ 用户要在悬浮里找自己那一句，若嫌长可改为只在 `.help` 里放**当前状态**那一句（用 `.Note` 的值拼进 `tooltip`，模板侧 `{{if}}` 分支）。
- 验证：`/admin/returns?returnId=<真实id>`（需先有退货单）→ `document.querySelectorAll('.card-body > p.hint').length` 应减少 1；`hintReal`（`02-J` §F1 修正口径）不升高。
- 风险：`returnStatusNote` 是 Go 侧硬编码中文（不在 `enums`、不过 i18n）→ 移进 `.help` 前建议一并 key 化，否则英文界面露中文。

**任务 3：`?returnId=` 不存在时给一句提示**
- 级别：P2
- 位置：`internal/module/order/inbound/http/return_page_handle.go`（`returnId` 查不到即 `HasDetail=false`）
- 现状：`/admin/returns?returnId=00000000-…` → 与基线逐字相同，无提示。
- 问题：同 orders 任务 5。
- 改法：注入 `Err`，走 `returns.html:52-54` 已有槽位。
- 验证：同上 URL → `.badge-warning` 存在。
- 风险：同 orders（走 `orderFacingError` 白名单）。

---

## 3. coupons（/admin/coupons，模板 `internal/templates/admin/coupons.html`）

**度量**：`screens=1 hintReal=0 cards=1 tables=0 rows=0 visRows=0 forms=2 tdForms=0 templates=1(43) htmlKB=25 elements=278 checkAll=0 colActions=0 filterBar=1 pageHead=1 overflowX=0`
**筛选实测**：`?keyword=ZZPROBEXYZZ` → 回显 ✓ + 空态 title 变「没有匹配「ZZPROBEXYZZ」的优惠码」✓（`coupon_page_handle.go:182` 读 query）；`?status=zzbogus` → 空态变「这个状态下没有优惠码」但**下拉 value 回退成空**（见任务 2）。
**职能**：优惠码 CRUD + 启停 + 核销记录查看。

**任务 1：时间窗改用日期范围控件，不再让用户手打格式**
- 级别：P1
- 位置：`internal/templates/admin/coupons.html:303` / `:307`（编辑抽屉 `startsAt`/`endsAt`）、`:368` / `:372`（新建抽屉同名字段）
- 现状：四个 `<input type="text" class="form-input">`，占位符写「开始时间（留空 = 不限），如 2026-01-01 或 2026-01-01 09:00」，另有一段 `.help`（`:366`）解释两种写法。**同域 `customers.html:88/90` 与 `analytics.html:61/65` 已用 `<input type="date">`**。
- 问题：判据 `02-K` §3「日期 → DatePicker（范围选择器）」。用户必须记格式（`2006-01-02` 或 `2006-01-02 15:04`，不接受带时区），打错被服务端拒绝（服务端只回页顶一句 `?err=`，不定位到字段）→ 试错 2~3 轮。这是**同域内实现漂移**。
- 改法：① 只到日的场景直接用 `<input type="date">`（与 customers 对齐）；② 需要时分时用 `datetime-local`（原生，无需新组件）；③ 若两者都要支持（向后兼容库里已有的带时分值），用 `type="date"` + 独立「时/分」两个 select，或按 `02-K` §3 的建议做成一个范围控件（`02-K` §7 序 2 列为「低」成本）。**推荐 ①**：先与同域对齐，时分需求另开一条。
- 验证：抽屉打开后 `document.querySelectorAll('#tpl-coupon-create input[type=date]').length` 应为 2；提交一个日期后 `?err=` 不出现格式错误。
- 风险：服务端 `CouponCreateReq`/`UpdateReq` 的时间字段解析（`utils.JSONTime` 宽松解析支持 `2006-01-02`）**必须同步确认**：`<input type="date">` 只提交 `YYYY-MM-DD`，若服务端要求时分精度会静默变成当天 00:00 —— 改前先读 `order/dto` 的时间字段 tag 与 `utils.JSONTime` 的解析分支。

**任务 2：非法 `status` 时下拉必须回显生效值（否则控件与空态文案自相矛盾）**
- 级别：P2
- 位置：`internal/templates/admin/coupons.html:85-90`（status 下拉，`{{if o.Value == filterStatus}}selected{{end}}`）+ `:107`（空态 title 分支 `{{else if filterStatus}}`）
- 现状：`?status=zzbogus` → 空态 title =「这个状态下没有优惠码」（说明 `filterStatus != ""` 为真），但**下拉显示「（状态：全部）」**（无 option 匹配 → `value=""`，实测 `fields.status.value=""`）。
- 问题：控件说「全部」、空态说「这个状态下没有」→ 用户无法判断当前到底筛了什么、也不知道该清掉什么。判据「筛选控件必须回显生效值」。
- 改法：在 `<option value="">（状态：全部）</option>` 之后加一个 `{{if filterStatus != "" && !filterStatusValid}}` 的「（未知状态：{{filterStatus}}）」选项（由 handler 给出 `filterStatusValid` 布尔），或干脆在 handler 侧把非法 status 归一成空串（与 customers 的处理一致：`?status=zzbogus` 时 `FilterActive=false`）。
- 验证：`/admin/coupons?status=zzbogus` → `document.querySelector('#coupons-filter-status').value` 应等于 `filterStatus`（或空态 title 退回「这个工程还没有优惠码」），两者不得矛盾。
- 风险：归一化会改变现有行为（非法值从「筛出 0 行」变成「不筛」）→ 若要保持「非法值 = 筛 0 行」的现状，就必须走「新增未知状态 option」这条路。

**任务 3：每行一份抽屉模板改为按需加载（或至少先登记为架构债）**
- 级别：P2
- 位置：`internal/templates/admin/coupons.html:250`（`<template id="tpl-coupon-edit-{{r.Form.ID}}">`，闭合于 `:318`）
- 现状：实测当前 0 行 → `templates=1(43 节点)`；每多一张券多一份 43 节点的完整编辑表单。对照 `02-I` §6.1：`menus` 490KB/4603 节点、`i18n` 258KB/1326 节点都是同一模式。
- 问题：`02-I` §5.2 #4 只登记了 menus/i18n，**coupons 是同族但未记录**。券数量增长后页面体积线性膨胀（按 43 节点/张，100 张 = 4300 节点 + 100 个 form）。
- 改法：**不要单独改**（`02-I` §6.1 已定性为待决策架构问题，方案 A 需新增 `GET /admin/coupons/edit-form?id=` 片段端点 + 权限点）。本轮**只在清单里补一条记录**，与 menus/i18n/product_bundle 同批处理。
- 验证：改架构后 `document.querySelectorAll('template[id^=tpl-]').length` 应降到 1（仅新建抽屉）。
- 风险：`ui/drawer.js:44` 的 `openDrawer` 只在打开瞬间克隆模板 → 改 htmx 后必须确认 `htmx.process` + `WBUI.scan` 在内容插入后执行（`drawer.js` 已有这段，别绕过）。

**任务 4：`.filter-bar` 里不再承载列表标题（含「共 N 张」统计）**
- 级别：P2
- 位置：`internal/templates/admin/coupons.html:72-79`（`.filter-bar` → `.filter-row` → `span.card-title`「优惠码（共 {{.Total}} 张）」+ `.help`）
- 现状：`.filter-bar` 的第一个子块是列表标题 + 统计，第二个才是筛选表单；实测 `filterBar=1`、`filterFields=3`。
- 问题：`02-I` #29 已记 3 处同类（`inventory.html:122` / `inventory_warehouses.html:53` / `inventory_reasons.html:44`），**coupons 与 dashboard 是第 4、5 处**。后续按 `.filter-bar` 批量调样式会误伤标题行。另 `02-L` P2-7 已记「统计混进列表标题」。
- 改法：标题与统计移出 `.filter-bar`，进 `.card-header`（`analytics.html:81-83` 的 `<div class="card-header"><span class="card-title">` 是现成形状）；统计（共 N 张）为空时按 `02-L` P2-7 的判据隐藏。`.filter-bar` 只留 `.filter-fields` + `.filter-actions`。
- 验证：`document.querySelector('.filter-bar .card-title')` 应为 `null`；`document.querySelector('.card-header .card-title')` 存在。
- 风险：`.filter-bar` 目前给了 `.filter-row` 的横向布局，标题移走后需确认筛选表单仍横向排列（`02-H` 的 `.filter-fields` 若在 flex 父元素里必须 `flex: 1 1 100%`，见 `admin-ui-logs` §2.2 注）。

**任务 5：编辑抽屉里两个数字字段补 label**
- 级别：P2
- 位置：`internal/templates/admin/coupons.html:293-298`
- 现状：`maxUses` 与 `perUserLimit` 两个 `<input type="number">` **没有 `<label>`**，只有 `placeholder`（「总次数上限（0 = 不限）」/「每人限次（0 = 不限）」）；同抽屉里其它字段（券码/名称/折扣/状态/门槛/折扣值/开始/结束/备注）都有 `<label class="form-label">`。
- 问题：placeholder 是「输入后消失」的信息 → 用户填了值再回头看，就分不清哪个是总次数、哪个是每人限次（两个都是数字、单位都是次）。判据「识别优于回忆」。
- 改法：补 `<label class="form-label">`，抄同文件 `:284`（「使用门槛（分）」）的写法。
- 验证：`document.querySelectorAll('#tpl-coupon-create label, [id^=tpl-coupon-edit] label').length` 与 input 数一致。
- 风险：`.form-row` 的等宽列布局对 label 出现敏感（label 占一行高度）→ 补完确认 3 列仍等宽、窄屏仍堆叠。

---

## 4. customers（/admin/customers，模板 `internal/templates/admin/customers.html`）

**度量**：`screens=1 hintReal=0 cards=1 tables=0 rows=0 visRows=0 forms=2 tdForms=0 templates=0 htmlKB=21 elements=270 checkAll=0 colActions=0 filterBar=1 pageHead=1 overflowX=0`；`badgesTop=7`（✓ 全部 0 / 正常 0 / 已停用 0 / 待激活 0 / 已锁定 0 / 邮箱已验证 0 / 邮箱未验证 0）
**筛选实测**：`?keyword=ZZPROBEXYZZ` 回显 ✓ + 空态切到「该筛选条件下暂时没有账号」✓；`?registeredFrom=1990-01-01&registeredTo=1990-01-02` → 两个日期回显 ✓；`?status=zzbogus` → 回显空、`FilterActive=false`（空态文案不变）。
**职能**：站点访客账号列表 + 停用/启用 + 解除锁定 + 组合筛选。

**任务 1：修掉空态里那个点了没反应的「重置」主行动**
- 级别：P1
- 位置：`internal/templates/admin/customers.html:110`
- 现状：`<div class="empty-actions"><a class="btn" href="/admin/customers">重置</a></div>` —— **无筛选时它也渲染**。实测基线 `resetHref="/admin/customers"` 且 `currentPath="/admin/customers"` → `sameAsCurrent: true`（点击后 URL 与页面逐字不变）。
- 问题：判据 1「操作流闭环：点了没反应」。用户看到空态说「先点『重置』看一眼全部账号」，点下去什么都没发生 → 他会怀疑按钮坏了，或反复点。**这比没有按钮更糟**（`02-L` P2-16 把 customers 归为「空态无主行动」，实测是「有主行动但无效」）。
- 改法：把 `.empty-actions` 移进 `{{if .FilterActive}}` 分支（模板 `:103` 已有该布尔）—— 筛出来为空时给「重置」，真的一个都没有时不给（或改给「查看前台注册页」这类真正有用的动作）。**参考 `coupons.html:116-118`**：它在 `.empty-actions` 里用 `{{if filterKeyword != "" || filterStatus != ""}}` 守卫「清掉筛选」，无筛选时不渲染。
- 验证：`/admin/customers`（无筛选）→ `document.querySelectorAll('.empty-actions a').length` 应为 0；`/admin/customers?keyword=ZZPROBEXYZZ` → 应为 1 且 href 不含 `keyword`。
- 风险：`{{if .FilterActive}}` 与 `.empty-actions` 是两个独立分支，注意别把 `.empty-desc` 也一起包进去（desc 两种状态各有一句，`customers.html:104-108` 已有 `{{if .FilterActive}}` 分叉）。

**任务 2：把被库值覆盖掉的「后台不能直接新建」还回空态**
- 级别：P1
- 位置：`internal/templates/admin/customers.html:108`（模板兜底）+ 库值来源 `public/migrations/232_i18n_seed_admin_pages_round4.sql:65`
- 现状（dbx 级证据）：
  - 模板兜底（`customers.html:108`）：「客户是访客在站点上自己注册出来的，**后台不能直接新建** —— 完成注册后会出现在这里。」
  - 库值（`232_…round4.sql:65`）：「站点还没有访客注册，或者上面的筛选条件太窄了 —— 先点「重置」看一眼全部账号。」
  - 实测页面输出 = **库值**（`emptyDesc` 逐字匹配）。
  - 同类：`:107` 的 `empty_heading` 库值（`229_i18n_seed_admin_page_shell.sql:26`）=「没有符合条件的客户」，模板兜底 =「还没有客户」。
- 问题：与 `02-I` §5.2 #12 / §5.2.4 #42（mail 两处「库值与模板默认脱节」）**同族第 3、4 处**，且这次是**信息丢失型**：模板里那句「后台不能直接新建」回答了用户唯一的疑问（我能不能在这儿建一个客户？），被库值换成了「点重置」——而重置按钮在当前场景下无效（任务 1）。同时 `empty_heading` 的库值「没有符合条件的客户」在**无筛选**时是错的（没有条件可"符合"）。
- 改法：写一条 `UPDATE` 迁移（带旧值前置条件，不覆盖运营手工修改，样板见 `317_fix_inventory_moves_empty_i18n.sql` + `register_inventory_moves_empty_i18n.go`），把两个 key 的库值对齐成模板兜底；或反过来把模板兜底改成库值并删掉「后台不能直接新建」的承诺（**不推荐**，那句是真实且有价值的约束说明）。**推荐前者**。
- 验证：`/admin/customers` → `document.querySelector('.empty-desc').innerText` 应含「后台不能直接新建」；`empty_heading` 应为「还没有客户」。跑 `go test ./public/migrations/` 与 `internal/templates/...`（有 i18n 一致性门禁 `admin_group_f_i18n_test.go`）。
- 风险：迁移必须**先查库里现值再定 UPDATE 的 WHERE**（`02-I` §5.2 #1 的教训：seed 用 `ON CONFLICT DO NOTHING`，改模板文案不写迁移等于没改）；注册文件与 `register.go` 的调用方式二选一（`02-L` §0.5「附带完成」已定：自带 `init()` 或显式调用，不要两处都写）。

**任务 3：清掉孤儿词条 `admin.customers.empty`**
- 级别：P2
- 位置：`public/migrations/190_i18n_seed_marketing.sql:398-399`
- 现状：该 key 的值「没有符合条件的客户。站点还没有访客注册，或者上面的筛选条件太窄了 —— 先点「重置」看一眼全部账号。」与模板**已不再引用**的旧 key（模板现用 `admin.customers.list.empty_heading` / `.empty_desc`）。
- 问题：孤儿词条会让「哪些词条还在用」无法从库里判断，改文案时容易改错那一条（这次任务 2 的坑正是同一个来源：190 的旧句被 232 的新句继承下来了）。
- 改法：`grep -rn "admin.customers.empty\b" internal/` 确认无引用后，写迁移删除该 key 的两语言行。**注意** `AGENTS.md` 的教训：**删能力要连 seed 的 SQL 与幂等条件一起收口**（`register_retired_permission_test.go` 那条判据的同类）。
- 验证：`grep -rn "admin.customers.empty'" public/migrations/*.sql internal/` 应为 0；`go test ./public/migrations/` 通过。
- 风险：190 是历史 seed，**不要改 190 本身**（历史迁移保持原样），用新迁移删。

**任务 4：行内 `colspan` 提示行不再作为正文说明（或至少纳入度量口径）**
- 级别：P2
- 位置：`internal/templates/admin/customers.html:184-189`（`{{if r["PendingHint"]}}` / `{{if r["LockHint"]}}` 各插一行 `<tr><td colspan="7" class="hint">`）；文案源 `internal/module/user/inbound/http/customer_view.go:161/165/168`
- 现状：待激活/已锁定/有失败计数的行**下方多插一整行全宽说明**，例如「待激活：客户还没完成邮箱验证。这类账号本来就登不上去，…」（`customer_view.go:161`）。`colspan="7"` 与表头列数一致 ✓。
- 问题：① 违反 §2.1（说明进 `.help`），且它是**每行重复**的（多个待激活账号 → 同一句话出现多次）；② 它是 `02-J` §F1 修正口径的**盲区** —— `.hint` 在 `<td>` 内被 `closest('table')` 排除（§H4 的排除规则），所以 `hintReal` 读数为 0，但内容是整句说明而不是单元格值。
- 改法：改成一个 `.help` 悬浮挂在**状态徽章**旁（`customers.html:162` 的状态列），说明只在悬浮里出现一次；或保留行内但在文案前加状态前缀并只对「需要处理」的行渲染（当前 `LockHint` 在「有失败计数但未锁定」时也插一行，属于噪声）。
- 验证：`document.querySelectorAll('tbody td[colspan]').length` 应为 0；同时把度量口径补进 `02-J` §F1（排除规则应保留 `td[colspan]` / `td.hint`）。
- 风险：`PendingHint`/`LockHint` 是 Go 侧硬编码中文（`customer_view.go`，不在 enums、不过 i18n）→ 移进 `.help` 时建议一并 key 化，否则英文界面露中文。

**任务 5：注册时间用范围控件（与 analytics 同批）**
- 级别：P2
- 位置：`internal/templates/admin/customers.html:88` / `:90`
- 现状：两个独立 `<input type="date" id="registeredFrom">` / `<input type="date" id="registeredTo">`，label 分别是「注册时间」与「至」（`:87` / `:89`）—— 第二个 label 只写「至」，脱离第一个 label 就不成立。
- 问题：`02-K` §3 要求日期范围是一个控件（antd `RangePicker`）。判据 `02-K` §7 序 2「日期范围控件」成本标为**低**，且这是**两处同形**（customers + analytics）。
- 改法：做成一个共享的 `.date-range` 组件（一个输入框 + 弹出双月历），或在基座 `static/js/ui/` 加一个 `daterange.js`（渐进增强：原生两个 date 保留在 DOM 里，视觉隐藏，`name`/提交照旧 —— 遵循 `WBUI.select` 的既有契约，见 `02-K` §8）。**注意** `02-K` §8：新控件不得因「初始化时没找到元素」提前 return（字段常随抽屉进 DOM）。
- 验证：范围控件渲染后，原生两个 input 仍在 DOM（`document.querySelectorAll('input[name=registeredFrom]').length === 1`）；键盘 Tab 可聚焦、Esc 关闭、窄屏不溢出（375/768/1440 三视口 + 鼠标/滚轮/触屏/键盘四种输入，`AGENTS.md` 硬规则）。
- 风险：这是**基座控件新增**，影响面超出本页 → 若本轮不做，至少把 customers/analytics 两处的 label 文案补完整（「至」→「注册时间（止）」），避免脱离上下文看不懂。

---

## 5. customer_detail（/admin/customers/detail?id=…，模板 `internal/templates/admin/customer_detail.html`）

**度量**：**无法实测** —— `customers` 表 `total:0`，取不到真实 id（任务说明已允许：取不到 id 只做源码审查并标注）。
**模式 A 核验（可实测，已做）**：`/admin/customers/detail` 无 id → **303 → `/admin/customers?err=客户编号不合法，请回到列表页重新操作。`** + 完整页壳（h1=客户管理、nodes=271、badges 齐全）。`?id=00000000-…`（不存在）→ 同样 303 + 同一条 `?err=`。→ **模式 A 处理正确，有出路，是正面样本**。
**职能**：单个访客账号资料 + 登录事实 + 订单摘要 + 停用/解锁。

**PASS — 检测了 A1–A4, B1–B4, C1–C4, D1–D5, E1–E5, F1–F4, G1–G2**（源码维度）
- `.page-head` + h1 + `.help` + `.page-actions`（返回列表）齐 ✓（`:11-24`）
- 三个卡：资料（`.kv`）/ 订单摘要（`.kv` + 徽章）/ 操作（两个 form）→ 单职能内聚，无只读监控区块占主位 ✓
- 错误槽位 `{{if .Err}}` + `{{if .Ok}}` 齐（`:26-31`）✓
- 空态三段式齐：`no_detail` 有 title + desc（`:35-38`）；`no_projects` 有 title + desc + actions（`:94-98`）✓
- 行内表单无嵌套（两个 form 在卡内、非 `<td>`）✓
- 破坏性动作（停用）文案 `{{.StatusActionLabel}}这个账号`（`:127`）→ 动词+对象，可读 ✓

**任务 1：`{{.StatusActionLabel}}这个账号` 拼接句读起来是「停用这个账号」还是「启用这个账号」要确认**
- 级别：P2
- 位置：`internal/templates/admin/customer_detail.html:127`
- 现状：`<button type="submit" class="btn btn-primary">{{.StatusActionLabel}}{{ .["t"]("admin.customer_detail.action.account_suffix", "这个账号") }}</button>` —— 后半句是 i18n key，前半句来自服务端 `StatusActionLabel`。
- 问题：中英混排风险 —— 英文界面下 `StatusActionLabel` 若已是英文（"Disable"），拼上中文兜底串会变成「Disable这个账号」（`returns`/`orders` 的批量文案已专门处理过这类问题，见 `order_page_query.go:100-104` 的「动词也 key 化」注释）。无法实测（无客户数据）。
- 改法：把后缀并进 `StatusActionLabel`（服务端出完整句），或确认 `StatusActionLabel` 在两种语言下都是纯中文/纯英文。参考 `orders.html:145/147` 的写法（整句走一个 key）。
- 验证：切到 `?lang=en-US`（顶栏语言切换）→ 按钮文案不应中英混排。
- 风险：改服务端 `StatusActionLabel` 需同步 `customer_detail_page_render_test.go` 的断言。

---

## 6. dashboard（/admin，模板 `internal/templates/admin/dashboard.html`）

**度量**：`screens=1 hintReal=0 cards=5 tables=1 rows=8 visRows=8 tableAt=0.35 forms=1 tdForms=0 templates=0 htmlKB=18 elements=240 checkAll=0 colActions=9 filterBar=1 pageHead=1 overflowX=0`；`pageActions=页面管理 / 商品管理`
**职能**：站点工程 / 页面发布状态概览（4 张 KPI 卡）+ 最近更新页面列表。

**任务 1：`.filter-bar` 里没有筛选控件，被当标题行用**
- 级别：P2
- 位置：`internal/templates/admin/dashboard.html:58-61`
- 现状：实测 `filterBarChildren = ["H2.card-title", "A.btn btn-sm"]`、`filterFields=0` —— `.filter-bar` 里只有一个 `<h2 class="card-title">最近更新的页面</h2>` 和一个「查看全部」链接。
- 问题：`02-I` #29 同族第 6 处（inventory 3 处 + coupons + dashboard）。类名承诺「筛选栏」但承载的是标题行语义 → 后续按 `.filter-bar` 做统一布局/间距调整时会误伤这一页。
- 改法：改成 `.card-header`（`analytics.html:81-83` 的形状：`<div class="card-header"><span class="card-title">…</span></div>`）；「查看全部」链接若属页级动作则进 `.page-actions`（但 `.page-actions` 已有两个按钮，保持行内更合适）。
- 验证：`document.querySelectorAll('.filter-bar').length` 应为 0；`document.querySelectorAll('.card-header').length` 应为 1。
- 风险：`.card-header` 与 `.filter-bar` 的内边距/对齐不同 → 改完截图核对表头顶边（当前 `tableAt=0.35`，不应劣化）。

**PASS — 检测了 A1–A4, B1–B4, C1–C4, D1–D5, E1–E5, F1–F4, G1–G2**
4 张 KPI 卡（`stat-grid`）+ 1 张列表卡；首屏 8/8 行、表格顶边 0.35 屏；`hintReal=0`（正文零说明，口径说明在 `.help`）；`.col-actions` 9 处（1 th + 8 行）、行内只有「编辑」一个动作且指向 `/workbench?id=`；空态三段式齐（`:63-69` title + desc + actions）；`overflowX=0`；无勾选（概览页只读跳转，批量无意义 → 判据 D4 不适用）。**与 `02-L` 「已确认合规」一致**（补充：实测是 **5 张卡**，不是 4 张 —— 4 KPI + 1 列表卡）。

---

## 7. theme（/admin/themes，模板 `internal/templates/admin/theme.html`）

**度量**：`screens=1 hintReal=0 cards=1 tables=1 rows=1 visRows=1 tableAt=0.17 forms=1 tdForms=0 templates=1(10) htmlKB=20 elements=244 checkAll=0 colActions=2 filterBar=0 pageHead=1 overflowX=0`；`pageActions=系统页面槽位 / ＋新建主题`
**职能**：主题列表（多套并存、单套激活）+ 激活/删除 + 新建（抽屉）。

**任务 1：删除主题的二次确认没说后果**
- 级别：P1
- 位置：`internal/templates/admin/theme.html:102`
- 现状：`data-confirm="{{tr("admin.theme.confirm_delete_prefix", "确定删除主题「")}}{{t.Name|safeJs}}{{tr("admin.theme.confirm_delete_suffix", "」？")}}" data-confirm-danger` → 实际文案「确定删除主题「默认主题（后台风格）」？」。`data-confirm-danger` **有** ✓（实测因唯一主题处于激活态、删除按钮不渲染 → `confirms=[]`，本条为源码判据）。
- 问题：判据 5「确认文案说清后果了吗」。删一个主题的后果不是「删掉一个名字」——挂在它下面的页面会怎样（掉到别的主题？变成无主题？）、已发布的产物会不会失效，全都没说。**同域已有两个正确样本**：`coupons.html:244`「删除该优惠码？有核销记录的券会被拒绝，请改用停用。」、`site_slots.html:137`「解绑这个槽位？指向它的链接将不再生成（页面本身不受影响）。」而 `02-L` §0.3 全域共性⑥只把「确认删除？」记在管理域 —— **站点域这一处未记录**。
- 改法：把后果写进 confirm 文案。先确认服务端删除主题的**真实行为**（读 `theme_admin_pages.go:243` 附近的 DeleteTheme 分支与 `project` service 的主题删除逻辑：页面上的 `theme_id` 是置空、级联、还是拒绝），再照实写，例如「确定删除主题「X」？挂在它下面的页面会失去主题设置（颜色/字体/页眉页脚回到默认），已发布的产物需要重新构建才会变。」**不要凭猜测写后果**（写错的后果说明比不写更糟）。
- 验证：新建第二个主题（非激活态）→ 行内「删除」按钮出现 → 点它，`document.querySelector('dialog')` 内的文案含「页面」二字（说明提到了影响面）。
- 风险：confirm 文案走 i18n（两个 key 拼接 + `safeJs`）→ 改文案要动 `confirm_delete_prefix/suffix` 两个 key 或新增 key，**同批 seed**。

**任务 2：无工程时整页没有 `.page-head`、没有 h1**
- 级别：P1
- 位置：`internal/templates/admin/theme.html:14-19`
- 现状：`{{if len(.Projects) == 0}}` 分支只渲染 `<section class="card card-body"><div class="empty-state"><p class="empty-title">…</p></div></section>` —— **整个 `.page-head` 在 `{{else}}` 分支里**（`:21` 起）。实测因当前有 1 个工程无法触达；源码判据明确。
- 问题：无工程时页面**没有标题、没有导航锚点**，用户不知道自己在哪一页（`document.title` 仍由 `layout.html` 给出，但正文没有 h1）。判据「页面必须有 h1」（`02-J` F 类与 `02-H` 页壳规范）。对比同域 `settings.html:10-19`：无工程分支**有** `empty-state` 三段，但同样没有 `.page-head` —— **两页同形**。
- 改法：把 `.page-head`（标题 + `.help`）移出 `{{if}}`，`{{if len(.Projects) == 0}}` 只包主区内容。抄 `analytics.html:18-40`（`.page-head` 恒渲染，无工程时才渲染 `:46-52` 的空态卡）。
- 验证：临时把 `Projects` 置空（或在 handler 里用一个不存在的 project）→ `document.querySelector('h1').innerText` 应为「主题管理」。**注意**：本项目 `admin.js`/`shell` 无「无工程」模拟开关，验证需 Go 侧单测渲染（`internal/templates/` 有直接渲染模板的测试，可加一条 `data` 不带 `Projects` 的用例）。
- 风险：`theme.html:14` 的 `{{if len(.Projects) == 0}}` 之后紧接着 `{{else}}` 才渲染 `.page-head`，移动时注意 `{{end}}`（`:109`）的配平 —— 模板完整性测试 `admin_template_integrity_test.go` 会拦。

**任务 3：两个空态补段（无主题缺 desc、无工程缺 desc+actions）**
- 级别：P2
- 位置：`internal/templates/admin/theme.html:17`（无工程：只有 title）、`:53-59`（无主题：title + actions，**缺 desc**）
- 现状：`theme.html:54` 的 title 自己就是一句话（「还没有主题，用上方表单创建第一个（首个主题自动激活）。」）——**它内部还提到了「上方表单」**，而新建入口是**页头 `.page-actions` 的抽屉按钮**（`:46`），不是「上方表单」→ **G2 违规**（文案指向的控件不存在）。
- 问题：① 三段式不齐（`02-I` §5.2.4 #35 记了 11 页 19 处，theme 未记）；② 「用上方表单创建」是 `02-L` P2-2 同族（`inventory_sources.html:107` 的空态也说「用上面的表单建一个」，但新建走抽屉）→ **同族第 2 例**。
- 改法：① title 改「还没有主题」；desc 补一句（「主题决定站点前端的全局颜色、字体与页眉页脚；首个主题自动激活。」）；② 删掉「用上方表单创建」—— `.empty-actions` 里的按钮（`:57`）本身就是入口，文案不必再指路。
- 验证：`/admin/themes`（临时清空主题或用 Go 侧单测）→ `.empty-title` 不含「上方表单」；`.empty-desc` 存在。
- 风险：三个 key 都要 seed（`admin.theme.empty` 改值 + 新增 desc key）。

---

## 8. theme_settings（/admin/themes/settings?id=…，模板 `internal/templates/admin/theme_settings.html`）—— **本域 P0 集中页**

**度量**：`screens=2.21 cards=1 tables=0 rows=0 forms=1(52 fields) tdForms=0 templates=0 htmlKB=71 elements=813 sections=0 tabs=0 overflowX=0`；`groups=10`、`h3s=[颜色 / 排版·标题 / 排版·正文 / 排版·链接 / 按钮 / 表面 / 图片 / 动效 / 全局页眉页脚块 / 结构模板]`、`selects=26`、`saveBtnTop=2.12`、`saveScrollNeeded=1.21`、`errSlot=false`、`labelsWithoutClass=49`、`pageActions=返回列表`（**无保存**）
**模式 A 实测（未修）**：`/admin/themes/settings`（无 id）→ `nodes=4, title='', bodyHead='缺少主题 id'`；`?id=00000000-…` → `bodyHead='主题不存在'`。**脱离页壳、无 title、无导航**。
**职能**：单主题全量设计令牌（11 色 + 排版 + 按钮 + 表面 + 动效 + 图片）+ 全局页眉/页脚/公告条块绑定 + 结构模板绑定。

**任务 1：POST 校验失败不再裸出 i18n key（本域最严重出口）**
- 级别：**P0**
- 位置：`internal/module/project/inbound/http/theme_settings_admin_pages.go:372`
- 现状（实测）：`POST /admin/themes/settings/save` 带 `id=<真实id>` + `colors.primary=not-a-css-value;evil` → **`400` + body `MsgThemeSettingsInvalid`（23 字节、非 JSON、无页壳、无 title）**。常量定义在同文件 `:27`（`themeSettingsMsgInvalid = "MsgThemeSettingsInvalid"`），库里有中文值「主题设置不合法」（`058_i18n_seed_enums.sql:272`）——**handler 直出 key 本身，没过 i18n**。
- 问题：**`02-I` §5.2.5「模式 A 全量核验」与 `02-L` P0-1 都没有记录这一处**（只记了 `:148/153/278/283` 四处 GET/POST 的缺参分支）。用户场景极常见：52 个字段里手打错一个颜色值（写 `red` 而非 `#ff0000`、或粘进一段带分号的 CSS）→ 点「保存并应用到该主题页面」→ **整页消失，只剩 `MsgThemeSettingsInvalid` 一行字**，看不到哪里错了，也回不到表单。这是 `02-I` §5.2.4 #31（datarule_edit 裸 key）的**同族第 2 例**。
- 改法：**303 回本页 + `?err=`**（该页模板需同时补错误槽位，见任务 4）。**不要**只换成 `shell.PageErrorBadRequest(c, "<scene>", err)`（`internal/web/shell/errors.go:45`）—— 已核实它内部就是 `response.ErrorWithMessage(c, 400, MsgInternalError)`，**返回 JSON、不渲染页壳**，换成它等于从「裸文本」变成「裸 JSON」，问题只换了个形状（`shell.PageError` 同理，`internal/module/product/inventory/inbound/http/inventory_page_handle.go:81` 用的是它）。保存失败时用户最需要的是「回到表单、看到哪里错、且输入还在」。
- 验证：`curl -s -o /dev/null -w '%{http_code} %{redirect_url}' -X POST -d 'csrf_token=<t>&id=<id>&colors.primary=not-a-css-value;evil' http://127.0.0.1:8080/admin/themes/settings/save` → 期望 `303` + `…/admin/themes/settings?id=<id>&err=…`；浏览器端 `document.querySelector('.badge-warning')` 存在且 `document.title` 非空。
- 风险：`ParseThemeSettings` 的失败原因是**字段级**的（哪个字段、什么值不合法），若要在页面上定位到具体字段，需要把 `err` 拆成字段名 + 提示（任务 4 的加强版）。至少先别丢页壳。

**任务 2：缺 id / 主题不存在时不再裸文本脱页壳（4 处）**
- 级别：**P0**
- 位置：`internal/module/project/inbound/http/theme_settings_admin_pages.go:148`（GET 缺 id）、`:153`（GET 不存在）、`:278`（POST 缺 id）、`:283`（POST 不存在）
- 现状：四处 `c.String(400|404, "缺少主题 id"|"主题不存在")`。实测 GET 侧：`nodes=4, document.title='', bodyHead='缺少主题 id'`。**`02-L` P0-1 / `02-I` §5.2.5 已记录（「最差档」），但至今未修** —— 管理域那批修的是 `admin_pages_handle.go`，`theme_settings_admin_pages.go` 不在其授权范围内（任务背景已点明）。**本轮确认：未修**。
- 问题：用户从收藏夹/分享链接进入（`?id=` 被截断）→ 白页 + 一行字，只能手改地址栏。
- 改法：**同族统一出口** —— GET 侧改 303 回 `/admin/themes?err=…`（列表页已渲染 `Err` 槽位）；POST 侧 303 回 `/admin/themes/settings?id=…&err=…`（若 id 本身缺失则回列表页）。**判据（`02-J` §H10）**：改一个失败出口时把**同一函数里所有失败分支**逐个过 —— 本文件还有 `:366`/`:378`/`:384`（任务 3）没修。
- 验证：`curl -s http://127.0.0.1:8080/admin/themes/settings | wc -c` 应远大于 7；`document.title` 非空；页面含 `<h1>` 或完整导航。四个分支各测一次。
- 风险：**不要只换 `shell.PageErrorBadRequest`** —— 已核实它内部是 `response.ErrorWithMessage`（JSON、不渲染页壳），换它等于「裸文本 → 裸 JSON」。若目标是「给带导航的可读页面」，**必须走 303 + `?err=`**。这一点 `02-L` §0.5「新增待办」已踩过（当时结论：`shell.PageError` 也解决不了这 6 处取数失败出口），别重犯。

**任务 3：JSON / 裸文本三个出口一并收口**
- 级别：**P0**
- 位置：`internal/module/project/inbound/http/theme_settings_admin_pages.go:366`、`:378`（`response.ErrorWithMessage(c, 500, themePageMsgInternal)`）、`:384`（`c.String(code, "主题设置保存成功，但页面刷新失败")`）
- 现状：`:366` 是 `json.Marshal` 失败（几乎不可能触发）；`:378` 是 `UpdateTheme` 失败（**真实可达**：DB 抖动/约束冲突）→ 用户点保存得到 **JSON body**（`{"code":500,"message":"MsgInternalError"}`，且 message 也是裸 key，`themePageMsgInternal = "MsgInternalError"` 见 `theme_admin_pages.go:28`）；`:384` 是保存成功但页面刷新失败 → **裸文本 + 非标准状态码**。
- 问题：`AGENTS.md` 硬约束「后台页面 handler 禁止 `c.String(500, err.Error())` 直出」的**第①种形态**；JSON 出口虽然「不是裸文本」，但对一个原生表单 POST 的页面来说**同样是脱离页壳的死胡同**（用户看到一坨 JSON）。`:384` 更麻烦：**操作其实成功了**（主题设置已落库），却给一个裸文本错误页 —— 用户会重试，而重试是幂等的（无害）但体验上等于「不知道成没成」。
- 改法：`:366`/`:378` → 303 回本页 + `?err=`（`themePageMsgInternal` 经 i18n 翻译后再拼，不要直出 key）；`:384` → 303 回本页 + `?err=`（文案改成「主题设置已保存，但页面标记待重建失败，请稍后重试」——**说清"保存成功了"**，别让用户以为白干）。
- 验证：`grep -n "response.ErrorWithMessage\|c.String" internal/module/project/inbound/http/theme_settings_admin_pages.go` 应为 0（除 `:387` 的正常 303）；`scripts/check-no-internal-error-leak.sh` 通过。
- 风险：`:384` 的 `code` 来自 `h.refreshThemePages` 的返回值，改成 303 后要确认该函数的错误仍被记录到日志（原文只进日志，见 `AGENTS.md`「错误文案三件套」）。

**任务 4：模板补错误槽位（失败后输入不丢 + 能定位出错字段）**
- 级别：P1
- 位置：`internal/templates/admin/theme_settings.html`（全文无 `{{if .Err}}`，实测 `errSlot=false`）
- 现状：52 个字段、26 个 select、71KB 的表单，**没有任何错误回显位**。任务 1~3 修好后，handler 会带回 `?err=`，但模板不渲染 → 用户看不到。
- 问题：判据 3「失败后输入是否丢失」+ 判据 4「失败能否定位到出错字段」。当前实现下，保存失败 = 裸出口（整页没了）→ **输入 100% 丢失**（浏览器回退可恢复，但不可依赖）。即使只修出口不补槽位，`?err=` 也会静默丢弃。
- 改法：① 抄 `datarule_edit.html` 的错误槽位（`02-L` 批 F 已建立，带 `?err=` 可见 + XSS 转义）；② 加强版：`ParseThemeSettings` 的字段级错误映射到对应 `<label>` 下方红字（`02-K` §2.2 的 antd `Form.Item` 契约），并在字段上加 `.is-invalid` 边框 —— 这是 `02-K` §7 序 1「字段级错误提示」列为**最高价值**的一项。**建议先做 ①，②另开一条**（要改 `ParseThemeSettings` 的错误结构）。
- 验证：`POST` 一个非法颜色值 → 页面顶部 `.badge-warning[role=alert]` 出现、文案是中文（非 key）、且表单里原有值仍在（`document.querySelector('input[name="colors.primary"]').value` 不为空）。
- 风险：52 个字段的 `value` 由**内联 JSON + 前端回填脚本**（`theme_settings.html:132-168`）负责 —— 若保存失败走 303 回本页，回填脚本会从**库里的旧值**重建表单（用户刚输入的 52 个值全丢）。**要真正做到「输入不丢」，必须让 handler 在失败时把 `PostForm` 的值回带进 `Groups`**（`buildThemeGroups(rawSettings)` 目前读的是库里 JSON，`theme_settings_admin_pages.go:226-230`）。这是本任务最大的坑，改前先确认这条链路。

**任务 5：保存按钮进页头 / 加 sticky**
- 级别：P1
- 位置：`internal/templates/admin/theme_settings.html:126-128`（`.form-inline.theme-actions`）；页头 `:21-23`（`.page-actions` 只有「返回列表」）
- 现状：实测 `saveBtnTop=2.12 屏`、`saveScrollNeeded=1.21 屏`、`screens=2.21`。判据 §2.2「主行动进 `.page-actions`」+ `02-K` §2.3「长表单的提交按钮固定在头部」。
- 问题：改一个颜色要滚 2 屏才能保存；滚到中部时页头早已离开视口，用户会找不到保存。
- 改法：二选一（或都做）—— ① 页头 `.page-actions` 加 `<button type="submit" form="theme-settings-form" class="btn btn-primary">保存并应用</button>`（`form=` 关联，模板已有 `id="theme-settings-form"`，**零 JS**；样板 `02-L` 批 F 的 `role_permissions` 页头提交 `form="perm-form"`）；② `.theme-actions` 加 `position: sticky; bottom: 0`（注意 `02-L` §0.4 的已知取舍：粘性条遮挡视口底部 96~112px）。
- 验证：`document.querySelector('.page-actions button[form]')` 存在且 `saveScrollNeeded === 0`；点击页头按钮后表单真的提交（`location.search` 含 `?ok=`/`err=`）。
- 风险：**作用域隔离** —— `02-L` 批 F 实测过 `role_permissions` 的 sticky 会污染其它页（`.admin-content` 的 `overflow`），本页要用 `.admin-content:has(> .theme-settings-page)` 之类的选择器限定，改完必须复测 `roles`/`permissions`/`articles` 三页的 `overflow` 仍是 `auto`、sticky 计数为 0（`02-L` §0.5 批 F 的验证口径）。

**任务 6：10 个分组折叠低频项（长表单分节）**
- 级别：P1
- 位置：`internal/templates/admin/theme_settings.html:40-64`（`{{range _, g := .Groups}}` 平铺出 10 个 `<h3>` + `.theme-color-grid`），实测 `sections=0`
- 现状：`screens=2.21`、`elements=813`、**零折叠**。分组标题：颜色 / 排版·标题 / 排版·正文 / 排版·链接 / 按钮 / 表面 / 图片 / 动效 / 全局页眉页脚块 / 结构模板。
- 问题：判据 A4「有几个职能需要折叠才能装下」+ 判据 3「长表单分节了吗」。当前是「一屏半塞满 52 个控件」——用户改一个颜色要在视觉噪声里找。
- 改法：把**低频分组**（图片 / 动效 / 结构模板 —— 结构模板已有 `structure_hint` 说明它「配一次就不动」）包进 `<details class="section-fold">`，默认收起；高频分组（颜色 / 排版 / 按钮 / 页眉页脚块）保持展开。**样板 `settings.html:113-157`**（`<details class="section-fold"><summary><span class="fold-title">高级</span><span class="fold-note">…</span></summary><div class="fold-body">…</div></details>`）。分组数据由 `buildThemeGroups` 给出（`theme_field_groups.go:179`）→ 在 `themeFieldGroupView` 上加一个 `Collapsed bool` 字段，模板按它决定是否包 `<details>`（**数据驱动，别在模板里写死分组名**）。
- 验证：`document.querySelectorAll('details.section-fold').length` 应为 3；`screens` 应降到 ~1.3；默认收起的组里字段仍可提交（`<details>` 折叠不影响表单提交，`settings.html` 的 URL 模式字段就是这么用的）。
- 风险：① 分组标题目前是裸 `<h3>`，包进 `<details>` 后语义变成 `summary`，`02-I` §5.2.4 #36 记过「折叠后勾选计数错乱」的同类问题（那是 `role_permissions`）→ 本页无勾选，风险低；② `theme-settings-json` 回填脚本（`:152`）用 `form.querySelectorAll('input[name*="."]')` 选择器，**折叠不影响 DOM**，回填仍生效 ✓（这条要实测确认）。

**任务 7：49 个裸 label 补 `.form-label`**
- 级别：P2
- 位置：`internal/templates/admin/theme_settings.html:45` / `:51` / `:56` / `:60`（`<label>{{f.Label}}<input …></label>` 包裹式）
- 现状：实测 `labelsWithoutClass=49`（49 个 `<label>` 无任何 class），控件与 label 是包裹关系（无 `for`/`id`）。
- 问题：与全站 `.form-field` + `.form-label` + `<label for>` 的主流结构不一致 → 样式与无障碍（`for` 缺失时读屏靠包裹关系，可行但不利于脚本化定位）都与其它页漂移。`02-I` §5.2.1 #28 记过 `.hint` 被当字段标签用（`product_pricing`），这是同类「表单结构不统一」的另一面。
- 改法：按 `settings.html:53-75` 的 `.form-field` + `.field-label-row` + `<label for>` 结构重写循环体（`:44-62` 的四个分支），`f.Name` 可直接当 `id`（点分名做 id 合法）。
- 验证：`document.querySelectorAll('#theme-settings-form label:not(.form-label)').length` 应为 0；点 label 能聚焦对应控件（`label[for]` 生效）。
- 风险：`.theme-color-grid` 的网格是按「label 包裹 input」的自适应列数算的（注释 `:111` 提过 `.form-row` 的等宽自动列数）→ 改成 `.form-field` 后列数可能变，需截图核对 1440/768/375 三视口。

**任务 8：成功保存后给一条 `?ok=` 提示**
- 级别：P2
- 位置：`internal/module/project/inbound/http/theme_settings_admin_pages.go:387`（`c.Redirect(303, "/admin/themes/settings?id="+themeID)`）
- 现状：成功 → 303 回本页，**URL 里没有任何标志**，页面与保存前逐字相同（除了值变了）。失败 → 裸出口。
- 问题：判据 4「提交后知道成功/失败吗」。这个页面保存的后果是「该主题下全部页面标记待重建」（模板 `.help` 里说明了），用户需要确认这件事发生了。
- 改法：303 改成 `…&ok=<文案 key>`，模板加 `{{if .Ok}}` 槽位（`orders.html:50-52` 的形状）。文案：「主题设置已保存，该主题下页面已标记待重建 —— 重新构建后新样式才会出现在访问面。」
- 验证：保存一次合法值 → `location.search` 含 `ok=`、页面顶部 `.badge-success` 存在。
- 风险：`?ok=` 的值必须走服务端白名单（`AGENTS.md`：`?done=`/`?ok=` 这类回带必须与写侧共用同一份模板字面量，否则手拼 URL 就能伪造系统消息 —— 样板 `order_page_query.go:62-104` 的 `orderDoneTexts`）。

---

## 9. settings（/admin/settings，模板 `internal/templates/admin/settings.html`）

**度量**：`screens=1.32 hintReal=0 cards=2 tables=1 rows=5 visRows=0 tableAt=1.05 forms=3 tdForms=0 templates=0 htmlKB=32 elements=414 checkAll=0 colActions=0 filterBar=0 pageHead=1 overflowX=0`；`folds=[高级(closed), 语言(open)]`；`submits=[保存设置 top=0.86, 保存语言清单 top=1.22]`；`pageActions=''`（空）
**职能**：站点信息 + 搜索引擎/统计集成密钥 + URL 结构（折叠）+ 语言清单（展开）。

**任务 1：`.page-actions` 为空 div（形式合规、实质无动作）**
- 级别：P2
- 位置：`internal/templates/admin/settings.html:32-43`
- 现状：`<div class="page-actions">` 里只有 `{{if len(.Projects) > 1}}` 的工程选择器；单工程时渲染出一个**空的 `.page-actions`**（实测 `pageActions=''`）。
- 问题：判据 §2.2 说主行动进 `.page-actions`；本页确实没有页级动作（两个保存都在各自表单里，这是对的）→ 但空 div 让「本页没有页级动作」和「忘了放」看起来一样，也让自动化判据（`pageActions` 非空 = 有主行动）失效。
- 改法：整个 `.page-actions` 用 `{{if len(.Projects) > 1}}` 包住（与 `seo.html:20-31` 的做法一致 —— 那页实测 `pageActions=null`，div 根本不渲染）。
- 验证：`document.querySelectorAll('.page-actions').length` 在单工程下应为 0。
- 风险：`02-H` 的页壳规范可能要求 `.page-head` 恒有两栏 → 若布局依赖 `.page-actions` 占位，改成 `null` 后标题栏对齐可能变，截图核对。

**PASS — 检测了 A1–A4, B1–B4, C1–C4, D1–D5, E1–E5, F1–F4, G1–G2**（其余维度）
- 正文零说明：`hintReal=0`，所有口径说明都在 `.help`（`:28/62/85/94/105/126/138/223`）✓
- 折叠策略正确：「高级」（404 页 + URL 结构，配一次就不动）默认收起 ✓；「语言」（增删语言清单，功能区）默认展开 ✓ —— `02-L` §5.2 已列为「不要动的地方」，注释理由充分（`:164-170`）
- 空态三段齐（`:12-18` title + desc + actions）✓
- 两个表单职责清晰、各有 CSRF、`projectId` 隐藏域齐 ✓
- 语言行片段走 HTMX（`hx-post="/admin/settings/locales/rows"`，`:194`）✓

**任务 2（更正类）：把「首屏 0 行数据」的成因写对**
- 级别：P2（文档更正，非代码）
- 位置：`docs/02-L-admin-rework-worklist.md` §0.4 与 `docs/02-I-admin-model.md` §5.1（`/admin/settings` 行）
- 现状：`02-L` §0.4 写「**settings**：首屏 0 行数据（语言折叠区默认 `open`）」，`02-I` §5.1 记「1.25 屏 / 5 行 / 首屏 0 行 / **首屏 0 行数据**」，而 `02-L` §5.2 又把「settings 的语言折叠区默认展开」列为**不要动**。
- 问题（实测更正）：本页**唯一的 `<table class="data-table">` 是 URL 结构表，它在默认收起的「高级」折叠区里**（`settings.html:113-157`，表在 `:141-154`），实测 `tableAt=1.05 屏`。语言区（`<details … open>`，`:171`）里装的是**表单行**（`partials/locale_rows.html`），**不是 data-table**。→ 「首屏 0 行」与语言区是否 open **无关**，两处记录把因果搞反了，且与「不要动语言区」并列时会让下一轮误改语言区。
- 改法：`02-L` §0.4 该条改成「settings 是**配置页**（非列表页），唯一的 data-table 在默认收起的「高级」折叠区 → 判据 F3「首屏必须有数据行」**不适用**」；`02-I` §5.1 的 `settings` 行同步改口径。
- 验证：`document.querySelectorAll('table.data-table').length === 1` 且 `document.querySelector('details.section-fold:not([open]) table.data-table') !== null`。
- 风险：无（纯文档）。

**任务 3（补充类）：settings.html:198-200 的下拉选项含实现术语**
- 级别：P2 —— **`02-I` §5.2.4 #40 已记录**（`default_plain` / `all_prefix` / `off` 直接显示在中文描述前），本轮实测确认存在（`select#lang-url-mode` 的 3 个 option 文本以这三个英文枚举开头）。**不重复报告**，仅确认。

---

## 10. seo（/admin/seo，模板 `internal/templates/admin/seo.html`）

**度量**：`screens=1.05 hintReal=0 cards=2 tables=1 rows=10 visRows=9 tableAt=0.47 forms=2 tdForms=0 templates=0 htmlKB=23 elements=302 checkAll=0 colActions=0 filterBar=0 pageHead=1 overflowX=0`；`pageActions=null`（单工程时不渲染 div）
**职能**：产物 SEO 体检（POST `/api/publication/seo-audit`，页面内 fetch）+ 热门路径（近 30 天）。

**任务 1：体检前的占位空态补全三段**
- 级别：P1
- 位置：`internal/templates/admin/seo.html:65-67`
- 现状：`<div class="empty-state"><p class="empty-title">运行体检后，此处显示当前产物中的 SEO 问题。</p></div>` —— 实测 `emptyStates[0] = {title: "运行体检后，此处显示当前产物中的 SEO 问题。", desc: null, actions: false}`。
- 问题：`02-I` §5.2.4 #35 已记「seo 空态无 `.empty-actions`」，**补新细节**：连 `.empty-desc` 也没有，且这个空态的性质与其它页不同 —— 它不是「没数据」，而是**主行动的占位**（按钮就在它上方 40px 处）。三段式在这里的真正缺口是「没说体检会看什么」，而不是「没给行动」。
- 改法：补 `.empty-desc`：「体检会读取已激活的 HTML 产物，检查标题、描述、canonical、图片替代文本、内部链接与 hreflang。」（这段文案**已存在**于 `.help-pop`，`seo.html:43` 的 `admin.seo.audit.intro` —— 直接复用同一个 key，不要另写一句）。`.empty-actions` 不必补（按钮已在同一张卡内，重复给入口是 `02-L` P2-8 那类问题）。
- 验证：`document.querySelector('.empty-state .empty-desc').innerText` 非空且与 `.help-pop` 的 `audit.intro` 同源。
- 风险：复用同一个 key 时注意 `02-L` §0.5 的「i18n key 会被 `admin_group_f_i18n_test.go` 判成孤儿」规则 —— 同一 key 在两处渲染是合法的（词条仍被引用），但要确认测试不会因「同一 key 出现两次」报错。

**任务 2：体检结果的内联 `<script>` 改 htmx**
- 级别：P2
- 位置：`internal/templates/admin/seo.html:108-151`
- 现状：一段 44 行的内联 IIFE，手写 `fetch(form.action, {method:'POST', body: new FormData(form)})` + 字符串拼 `innerHTML`（含自写的 `escapeHTML`）+ `data-msg-*` 属性携带文案。
- 问题：① 违反 `AGENTS.md`「不新增上述几处之外的散落业务 JS」（散落 JS 只允许收敛在 `static/js/ui/`、`rich-editor/`、`media-lib.js`、`workbench/`，存量 `admin.js`/`enhance.js`/`track.js`/`automation/` 属待收敛）；② 同一件事在项目里已有 HTMX 的成熟形态（`hx-post` + `hx-target` + `hx-indicator`），手写 fetch 等于另起一套（CSRF 也要靠 `hx-headers` 之外的路径）；③ 结果表格用 `innerHTML` 拼装，`escapeHTML` 只覆盖 5 个字符（漏 `\`` 等）—— 目前 `issue.Level/Path/Message` 是服务端产出，风险低，但这是**将来接第三方数据时的雷**。
- 改法：表单改 `hx-post="/api/publication/seo-audit" hx-target="[data-seo-audit-result]" hx-swap="innerHTML"`，新增一个服务端片段模板（`fragments/seo_audit_result.jet` 或 `admin/partials/seo_audit_result.html`）渲染结果表 —— 与 `02-I` §6.1 方案 A 同一条思路（片段服务端渲染、零客户端拼装）。**若本轮不做**，至少在 `AGENTS.md` 的「待收敛 JS」清单里补一行 `seo.html` 的内联脚本。
- 验证：改后 `document.querySelectorAll('script').length` 减少；点击按钮后 `[data-seo-audit-result]` 内出现服务端渲染的表格（`table.data-table` 存在）。
- 风险：接口返回的是 `pkg/response` 的 **JSON**（`:131` 的 `response.body.data`）→ 改 htmx 需要服务端按 `HX-Request` 分支返回 HTML 片段（`AGENTS.md`「响应与错误处理」表已规定这个分派），**要动 `publication` 模块的 handler**，影响面超出本页 → 建议单开一条，不塞进本轮 UI 批。

**任务 3：页头 `.page-actions` 在单工程时整块不渲染**
- 级别：P2
- 位置：`internal/templates/admin/seo.html:20-31`
- 现状：`{{if len(.Projects) > 1}}<div class="page-actions">…</div>{{end}}` —— 实测 `pageActions=null`（`div` 不存在）。而「运行当前站点体检」这个本页主行动在卡内（`:51`）。
- 问题：判据 §2.2 字面要求主行动进 `.page-actions`，但本页的主行动**必须**紧跟它的结果区（结果渲染在同一张卡的下方 `:54-68`）→ 移进页头会让「按钮在这里、结果在下面 1 屏处」的对应关系断掉。**判据在只读+单动作页上不适用**。
- 改法：**保持现状**（判定为 PASS，不是缺陷）。若要求形式统一，可只在多工程时保留 `.page-actions` 装工程选择器 —— 当前就是这么做的 ✓。
- 验证：无需验证。
- 风险：无。

**PASS — 检测了 A1–A4, B1–B4, C1–C4, D1–D5, E1–E5, F1–F4, G1–G2**（其余维度）
热门路径表：10 行 / 首屏 9 行 / 表格顶边 0.47 屏 ✓；空态 title + desc 齐（`:101-104`）✓；无横向溢出 ✓；无勾选（只读排行）✓；`.help` 4 处承载全部口径说明 ✓。

---

## 11. analytics（/admin/analytics，模板 `internal/templates/admin/analytics.html`）

**度量**：`screens=2.17 hintReal=0 cards=6 tables=5 rows=25 visRows=8 tableAt=0.42 tableTops=[0.42,0.8,1.55,1.78,2.0] forms=2 tdForms=0 templates=0 htmlKB=25 elements=430 checkAll=0 colActions=0 filterBar=0 pageHead=1 tabs=0 overflowX=0`；`tableHeads` 5 张全为 `[维度, 浏览数, 独立访客]`
**筛选实测（本域唯一可验证行数变化的页）**：`?from=1990-01-01&to=1990-01-02` → `rows 25→0`、两个 date 回显 ✓、badges 变「实际统计窗口：1990-01-01 至 1990-01-02 / 总浏览数 0 / 独立访客 0 / 不同路径 0」；`?from=2026-09-01&to=2026-09-10` → 回显 ✓、窗口徽章跟随 ✓；`?from=zzz&to=qqq` → 回退默认窗口但**窗口徽章如实显示 2026-08-24 至 2026-09-22** ✓（静默回退可被察觉）。
**职能**：站点访问统计（按天 / 路径 / 来源 / 设备 / 语言五个维度 + 时间范围）。

**任务 1：5 张同构表改 `.tabs`**
- 级别：P1 —— **已在清单**（`02-L` P1-6 / §2.5「应改 tabs —— 照抄 `masterdata_changes.html:106`」）
- 位置：`internal/templates/admin/analytics.html:80-210`（5 个 `section.card.list-card`，`:80` 按天 / `:102` 按路径 / `:125` 来源域 / `:154` 设备分类 / `:183` 语言）
- 现状（本轮补新证据）：`tables=5`、`tabs=0`、`screens=2.17`；**`tableTops=[0.42, 0.8, 1.55, 1.78, 2.0]`** → 只有第 1 张表在首屏，第 2~5 张分别在 0.8 / 1.55 / 1.78 / 2.0 屏；5 张表列头**完全同构**（`[维度, 浏览数, 独立访客]`）。
- 问题：同一份数据（窗口内的访问）的五种切法并列 → 用户每次只看一种，却要滚过全部；第 3~5 张表（来源/设备/语言）在 1.55 屏之后，**基本不会被看到**。
- 改法：照抄 `masterdata_changes.html:106` 的 `.tabs[data-tabs]` 完整结构（WAI-ARIA + 服务端渲染全部面板 + 切换零请求）：`<div class="tabs" data-tabs><div class="tab-list" role="tablist">…5 个 role=tab…</div><div class="tab-panel" role="tabpanel">…</div>…</div>`。**不要自创**（`02-L` §5.1 明确「全站唯一正确样板，勿自创」）。「按路径」面板里的分页（`:122`）与「来源/设备/语言」的 `.help` 悬浮（`:128/157/186`）随面板一起搬。
- 验证：`document.querySelectorAll('.tabs [role=tab]').length === 5`；`screens` 应降到 ~1.2；点第 5 个 tab 后 `aria-selected="true"` 且该面板 `hidden` 移除（键盘 ←/→ 也能切，`theme.css` §13 + `admin.js` 已实现）。
- 风险：① 5 个面板全部服务端渲染 → 页面体积不变（25KB 本来就小，不是问题）；② 「按路径」的分页链接会刷新页面 → 刷新后回到第 1 个 tab（`02-J`/skill §7.1 已给方案：用 URL 参数决定初始 `aria-selected`，需要时再做）；③ 空态 6 处（任务 2）要跟着 tabs 重组。

**任务 2：6 处空态收敛（改 tabs 后应只剩 1~2 个）**
- 级别：P2 —— 部分**已在清单**（`02-I` §5.2.4 #35 记「analytics(6 处)」无 `.empty-actions`）
- 位置：`internal/templates/admin/analytics.html:48-49`（无工程）、`:85-86`（按天）、`:107-108`（路径）、`:137-138`（来源）、`:166-167`（设备）、`:195-196`（语言）
- 现状（补新细节）：6 处**全部只有 `.empty-title`**，无 `.empty-desc`、无 `.empty-actions`；其中 **4 处的文案是同一个句式**——「没有路径数据。」/「没有来源数据。」/「没有设备数据。」/「没有语言数据。」（`:108/138/167/196`），它们的根因**完全相同**（窗口内没有访问明细）。
- 问题：这 4 句是同一个事实的四种说法，并列时像是四个不同的问题。改 tabs 后如果还留着 4 个空态，用户切 4 个 tab 看到 4 句几乎相同的话。
- 改法：tabs 化后，把 4 个维度面板的空态**合并成一个共享片段**（`admin/partials/analytics_empty.html` 或直接复用 `.empty-state` 的同一份 HTML），文案统一为「这个窗口里还没有访问明细。」+ desc 指向「发布页面之后才会有数据」（与 `:86` 的按天空态同源）。
- 验证：4 个面板的 `.empty-title` 文本一致。
- 风险：i18n —— 合并意味着 `admin.analytics.{paths,referrers,ua,langs}.empty` 四个 key 变成孤儿 → 要按 `AGENTS.md`「删除能力时连 seed 一起收口」清理（或保留并在注释里说明）。

**任务 3：时间范围改用 `.filter-bar` + 范围控件**
- 级别：P1 —— **已在清单**（`02-L` §2.5 / `02-I` §5.2.1 #9 的相邻条目；任务背景也已点明）
- 位置：`internal/templates/admin/analytics.html:55-78`（`section.card.card-body` 里的 `form.form-inline`）+ `:61` / `:65`（两个独立 `<input type="date">`）
- 现状（补新证据）：`filterBar=0`（时间范围在 `.card` 里，不是 `.filter-bar`）；两个 date 的 `label` 是「起始日期」/「结束日期」（这里比 `customers.html:89` 的裸「至」好）；页面还有 `.action-row` 里 4 个徽章（实际窗口 / 总浏览数 / 独立访客 / 不同路径，`:70-77`）。
- 问题：判据 `02-K` §3（范围控件）+ §7 序 2（成本低）。另：`.card` 承载筛选栏语义 → 与 `02-I` #29 同族。
- 改法：① 时间范围表单包进 `.filter-bar`（`content_templates.html:48` 是骨架样板）；② 两个 date 换成一个范围控件（与 `customers` 任务 5 **同批做**，共用一个基座组件）；③ 4 个徽章留在 `.action-row`（它们是结果摘要，不是筛选控件，位置正确）。
- 验证：`document.querySelectorAll('.filter-bar').length === 1`；范围控件渲染后原生两个 input 仍在 DOM。
- 风险：① `.filter-bar` 的引入会改变「时间范围」卡的视觉分组（当前是独立卡，含标题 `<h2>时间范围</h2>`）→ 搬进列表卡的 `.filter-bar` 后，「时间范围」标题是否保留要定（建议保留为 `.filter-row` 里的 `.card-title`，但这又回到「filter-bar 承载标题」的坑 → **建议：时间范围留在自己的卡里，只把两个 date 换成范围控件**，`filter-bar` 一事单列）。② 与 `customers` 共用组件时，先确认谁先落地（组件在 `static/js/ui/`，两页并行会冲突）。

**PASS — 检测了 A1–A4, B1–B4, C1–C4, D1–D5, E1–E5, F1–F4, G1–G2**（其余维度）
- 服务端筛选**真实生效**（本域唯一实测到行数变化的筛选栏）✓
- 非法日期静默回退但**有「实际统计窗口」徽章如实显示**（`:71-73`）→ 用户能察觉，**不判缺陷** ✓
- 正文零说明（`hintReal=0`），5 处 `.help` 承载全部口径（打点来源 / PV-UV 定义 / UTC 日界 / 各维度口径）✓
- 无横向溢出 ✓

---

## 12. site_slots（/admin/site-slots，模板 `internal/templates/admin/site_slots.html`）

**度量**：`screens=1 hintReal=0 cards=1 tables=1 rows=10 visRows=7 tableAt=0.21 forms=13 tdForms=0 templates=0 htmlKB=64 elements=659 checkAll=0 colActions=11 filterBar=0 pageHead=1 overflowX=0`；`pageActions=概览徽章（已绑定 2/10 · 已绑定但未发布 0 · 绑定的页面已被删除 0）`；`rowSelects=10`、`optionsTotal=140`、`perSelect=[14×10]`、`placeholderSameAsAria=[true×10]`、`unbindConfirmDanger=[false,false]`
**职能**：系统页面槽位（结算页 / 登录页 / 订单页…指向哪个页面）+ 绑定 / 换绑 / 解绑。

**任务 1：行内下拉瘦身（14 option × 10 行 = 140 节点）**
- 级别：P1 —— **已在清单**（`02-L` P1-18 / `02-I` §5.2.4 #38）
- 位置：`internal/templates/admin/site_slots.html:114-119`（`<select class="form-select" form="slot-bind-{{r.Slot}}" name="pageId" required>` + `<option value="">占位</option>` + `{{range r.PageOptions}}`）
- 现状（**更正既有数字**）：实测 `rowSelects=10`、**`optionsTotal=140`**、`perSelect` 全为 **14**（13 个真实候选 + 1 个占位）。`02-L` P1-18 与 `02-I` §5.2.4 #38 记的是「含 13 option、10 行 = **130** option 节点」→ **少算了占位项**。页面 64KB / 659 节点，10 行数据。
- 问题：`.col-actions` 单元格里塞一个 14 项下拉 + 一个按钮（`:114-120`）→ 操作列宽度被下拉撑开，行高与列宽都跟着它走；页面每多一个槽位就多 14 个 option 节点。判据 §C3「单元格是值不是句子」的精神 + §D「操作列只放按钮」。
- 改法：二选一 —— ① `hx-get` 按需加载（打开某个槽位的绑定面板时再拉候选，先例 `product_tags.html:123` 的「命中数按需展开」+ `_fragments/bundleConfigurator`）；② 把下拉移出 `.col-actions`，**给「当前绑定」列加一个「换绑」抽屉**（`data-drawer-open` + `hx-get` 片段），操作列只留「换绑 / 解绑」两个按钮（与 `theme.html:79-83` 的操作列形态一致）。**推荐 ②**：它与「操作列只放动作」的判据一致，且顺手解决任务 2。
- 验证：`document.querySelectorAll('td select').length` 应为 0；`document.querySelectorAll('td .col-actions button').length` = 10 行 × 1~2 个按钮。
- 风险：**HTML5 `form=` 跨树关联**（`:114` 的 `form="slot-bind-{{r.Slot}}"` 指向表格外的隐藏表单）是当前实现的关键 —— 若把下拉搬进抽屉，抽屉里的表单必须自带 `csrf_token`（`AGENTS.md`：原生表单必须显式加隐藏域，`hx-headers` 只对 htmx 请求生效）。`02-L` §0.5 已实测过「未展开的抽屉里看起来没 csrf，实际有」这个误判，改完别只读顶层 DOM 下结论。

**任务 2：占位符与 aria-label 同一句话，一行说两遍**
- 级别：P1 —— 部分**已在清单**（`02-L` P2-13「行内说明逐行重复」）
- 位置：`internal/templates/admin/site_slots.html:114`（`aria-label="{{tr("admin.site_slots.select_page", "（选择页面：显示的是页面草稿路径）")}}"`）+ `:115`（同一个 key 又做 `<option value="">` 的文本）
- 现状（补新细节）：实测 `placeholderSameAsAria = [true × 10]` —— **同一个 i18n key 在同一元素上出现两次**（`option` 文本 + `aria-label`）。原文「（选择页面：显示的是页面草稿路径）」既是下拉的占位项、又是读屏标签，10 行重复 20 次。
- 问题：① 判据 §C3：口径说明塞进控件占位符（"显示的是页面草稿路径"是**口径**，不是占位提示）；② `02-L` P2-13 只记了「逐行重复」，没记「同一元素内重复两次」；③ 同一句话做两件事（视觉占位 + 无障碍标签）→ 改一处容易漏另一处。
- 改法：① 占位项改成纯动作提示「选择页面…」；② 口径说明（草稿路径）搬进页头 `.help`（该 `.help` 已有 6 段说明，`site_slots.html:24` 的最后一段 `foot.*` **已经写了这件事**：「下拉里显示的是**页面的草稿路径**：页面没有「标题」这个概念，路径是它唯一稳定的身份。」）→ **口径说明已经在悬浮里了，占位符里的那句是纯重复**，直接删；③ `aria-label` 改成「为槽位 {{r.SlotName}} 选择页面」。
- 验证：`document.querySelector('td select option[value=""]').textContent` 不应含「草稿路径」；`aria-label` 与占位项文本不相等。
- 风险：改 `admin.site_slots.select_page` 的值会影响 `aria-label`（同一个 key）→ 若两处都要保留，必须拆成两个 key（并同批 seed）。

**任务 3：`selectedProject == ""` 的空态文案指向不存在的控件（G2）**
- 级别：P1
- 位置：`internal/templates/admin/site_slots.html:60-63`（`{{if selectedProject == ""}}` 分支：「先在上面选一个站点工程。」）
- 现状（源码 + handler 实测）：`internal/module/page/inbound/http/site_slot_handle.go:103-105` —— `selected := c.Query("project"); if selected == "" && len(projects) > 0 { selected = projects[0].ID }` → **`selectedProject == ""` 只在 `len(projects) == 0` 时成立**。而页头的工程选择器是 `{{if len(.Projects) > 1}}` 才渲染（`:29-38`）→ **0 个工程时页头根本没有工程选择器**。
- 问题：**G2 违规**（文案说「先在上面选一个」，但「上面」没有可选的控件）。且此时页面已经有 `:52-54` 的「还没有站点工程 —— 先去页面管理建一个工程」badge（含正确链接）→ 这句空态既错又冗余。实测当前有 1 个工程，该分支不可达（`emptyTitles=0, emptyDescs=0`）。
- 改法：删掉 `:60-63` 整个分支，或把它合并进 `:64-67` 的「没有取到槽位清单」兜底（后者本身是**取数失败的兜底文案**，与「未选工程」不是一回事）。**推荐删除** —— 未选工程这个状态在当前 handler 下不存在。
- 验证：Go 侧单测渲染 `siteSlotPageData(nil, "", …)`（0 工程）→ 页面不应出现「先在上面选一个站点工程」；`grep -n "pick_project" internal/templates/admin/site_slots.html` 应为 0。
- 风险：删分支后若将来 handler 改成「不自动选工程」，这个状态会回来 → 在删除处留一行注释说明前提（handler 自动选首个工程）。

**任务 4：解绑确认补 `data-confirm-danger`**
- 级别：P2
- 位置：`internal/templates/admin/site_slots.html:136-137`
- 现状：`<form id="slot-unbind-{{r.Slot}}" … data-confirm="解绑这个槽位？指向它的链接将不再生成（页面本身不受影响）。">` —— **有 `data-confirm`、无 `data-confirm-danger`**。实测 `unbindConfirmDanger=[false, false]`（2 个已绑定槽位）。
- 问题：判据 D3「删除类必须有 `data-confirm` + `data-confirm-danger`」。解绑是破坏性动作（影响访问面链接生成），当前确认框没有危险态视觉（对照 `theme.html:102` 的删除表单**两者都有**）。
- 改法：加 `data-confirm-danger`。
- 验证：`document.querySelectorAll('form[data-confirm]:not([data-confirm-danger])').length` 应为 0。
- 风险：无（纯属性）。

**任务 5：两个空态补 `.empty-title`**
- 级别：P2
- 位置：`internal/templates/admin/site_slots.html:61-63`（配合任务 3 一起处理）、`:65-67`（「没有取到槽位清单 —— 刷新重试；若一直为空，检查后端日志。」只有 `.empty-desc`）
- 现状：两处都只有 `.empty-desc`、**没有 `.empty-title`** → 三段式缺第一段（判据 G1）。
- 问题：`:65-67` 是**取数失败的兜底**，文案把技术细节（「检查后端日志」）给了运营 —— 违反「术语与用户语言一致」（§4）。另外它没有 `.empty-actions`（应给「刷新重试」的链接或按钮）。
- 改法：`:65-67` 补 `.empty-title`「槽位清单读取失败」+ `.empty-actions`（`<a class="btn" href="/admin/site-slots?project={{selectedProject}}">重新加载</a>`）；desc 里的「检查后端日志」改为「若反复出现请联系开发」（运营不该被指引去看日志）。
- 验证：`document.querySelectorAll('.empty-state:not(:has(.empty-title))').length` 应为 0。
- 风险：新增 key 需 seed。

**PASS — 检测了 A1–A4, B1–B4, C1–C4, D1–D5, E1–E5, F1–F4, G1–G2**（其余维度）
- 行内表单**全部外置**（`tdForms=0`，13 个 form 里 12 个是表格外的 `.hidden-form`）✓
- `.col-actions` 11 处（1 th + 10 行）✓
- 解绑确认**说清了后果**（「指向它的链接将不再生成（页面本身不受影响）」）→ **全站最好的确认文案之一** ✓
- 「没有页面」的空态（`:70-76`）三段齐 + 主行动 ✓，且 desc **解释了为什么现在不给下拉**（「一个空下拉只会让你点一个必然失败的提交」）—— 这是 `02-L` §5.1「条件未满足的反馈」的正样本同族 ✓
- 页头概览徽章带**语义色**（未发布 = warning、已删除 = danger，`:41-42`）✓
- 正文零说明（`hintReal=0`），6 段口径说明全在 `.help` ✓

---

# 本域共性问题（可合并成一批的）

| # | 形态 | 命中页 | 判据 | 建议 |
|---|---|---|---|---|
| G1 | **空态时 `<table>` 整张不渲染 → 表头消失** | orders `:92/:103`、returns `:272/:290`、coupons `:105/:122`、customers `:99/:112` | `02-L` P0-3 同族（已记 4 处，**本域新增 4 处**） | 四页同批修：`<table>` 移出 `{{if}}`，空态进 `colspan` 行。`02-L` §0.5 批 D 已有可复制的做法 |
| G2 | **空态三段式不齐**（缺 title 或 desc 或 actions） | theme `:17/:54`、site_slots `:62/:66`、seo `:66`、analytics 6 处 | `02-I` §5.2.4 #35（已记 11 页 19 处，**本域补 10 处细节**） | 与 G1 同批（同一个 `.empty-state` 块） |
| G3 | **`.filter-bar` 承载非筛选语义**（标题行 / 列表标题 / 时间范围卡） | dashboard `:58`、coupons `:72`、analytics（`.card` 装筛选栏） | `02-I` #29（已记 3 处，**本域新增 3 处**） | 统一改 `.card-header`；建议先定「`.filter-bar` 只放 `.filter-fields` + `.filter-actions`」并写进 `02-H` |
| G4 | **i18n 库值覆盖模板兜底（信息丢失型）** | customers `empty_heading` / `empty_desc` 2 处 | `02-I` §5.2 #12 / §5.2.4 #42 同族（**第 3、4 次**） | 一条 `UPDATE` 迁移 + 建议加「模板 default 与库值一致性」回归测试（`02-L` §0.3「新增待办」已提过这条判据） |
| G5 | **日期控件选型漂移**：两个独立 `date` / 纯文本手打 | customers `:88/:90`、analytics `:61/:65`（两个 date）；coupons `:303/:307/:368/:372`（纯文本） | `02-K` §3 + §7 序 2 | 一个基座范围控件（`static/js/ui/daterange.js`），三页共用；coupons 的纯文本先换 `type="date"` |
| G6 | **破坏性确认文案质量参差** | 说清后果 ✓：coupons `:244/:140`、site_slots `:137`、customers `:132-134`；**没说后果 ✗**：theme `:102` | 判据 5 / `02-L` §0.3 全域共性⑥ | theme 补后果说明（**先读服务端真实删除行为**）；`data-confirm-danger` 缺失：site_slots `:137` |
| G7 | **写失败出口脱离页壳** | theme_settings `:148/:153/:278/:283`（裸文本，**已记录未修**）+ `:372`（裸 i18n key，**新**）+ `:366/:378`（JSON）+ `:384`（裸文本） | `02-I` §5.2.5 + `AGENTS.md` 硬约束 | 单批收口（同一文件，独占）；**注意 `shell.PageErrorBadRequest` 是 JSON 出口，要走 303 + `?err=`** |
| G8 | **详情/列表的「id 不存在」静默** | orders（`?orderId=` 不存在 → 静默）、returns（`?returnId=` 不存在 → 静默）；已修 ✓：customer_detail（303 + `?err=`） | `02-I` §5.2.5 模式 A 的**新形态**：靠下游查不到兜底、静默无提示 | 两页各注入 `Err`，走已有槽位（零模板改动）；把这一形态补进 `02-I` §5.2.5 的判据扩展 |

---

# 建议修复批次（按文件冲突分组）

| 批 | 内容 | 涉及文件 | 并行性 |
|---|---|---|---|
| **A1** | theme_settings 写失败出口 4+3 处收口（G7） | `internal/module/project/inbound/http/theme_settings_admin_pages.go` | **独占**；可与除 A2 外任意批并行 |
| **A2** | theme_settings 模板补错误槽位 + 保存按钮进页头/sticky + 分组折叠 + 49 label + `?ok=` | `internal/templates/admin/theme_settings.html`（+ `theme_field_groups.go` 加 `Collapsed` 字段） | **必须 A1 之后**（契约依赖：A1 定 `?err=` 的回跳 URL 与文案形态，A2 才能写槽位）。文件不冲突，但契约必须先定 |
| **B1** | orders + returns 空态（表头 / 主行动 / 非法 status 提示 / 详情 id 不存在提示 / returns Note 进 help） | `orders.html`、`returns.html`（+ `order_page_handle.go`、`return_page_handle.go`） | 与 A1/A2 并行；**B1 独占两个模板 + 两个 handler 文件** |
| **B2** | customers 空态死主行动 + 表头 + colspan 提示行 + 日期控件 | `customers.html` | 与 B1 并行（不同模板）；**与 B3 串行**（同文件） |
| **B3** | customers i18n 库值对齐（G4）+ 孤儿词条清理 | 新迁移 + `register_*.go` + `customers.html`（若同时改兜底） | **必须 B2 之后**（同文件）；迁移文件独占 |
| **B4** | coupons 时间窗控件 + filter-bar 标题 + 两处 label + 非法 status 回显 | `coupons.html`（+ `coupon_page_handle.go` 若做 status 归一化） | 与 B1/B2 并行 |
| **B5** | site_slots 下拉瘦身（或改抽屉）+ 占位符/aria 去重 + 死空态 + `data-confirm-danger` + 空态 title | `site_slots.html`（+ 可能新增片段 handler） | 与 B1~B4 并行 |
| **B6** | analytics 5 表改 tabs + 6 空态收敛 + 时间范围（若含范围控件则与 G5 同批） | `analytics.html` | 与 B1~B5 并行 |
| **B7** | theme 删除确认文案 + 无工程补页头 + 空态补段 | `theme.html` | 与 B1~B6 并行 |
| **C1** | dashboard `.filter-bar` → `.card-header` | `dashboard.html` | 与 B1~B7 并行 |
| **C2** | seo 占位空态补 desc | `seo.html` | 与 B1~B7 并行 |
| **D1** | 基座范围控件 `static/js/ui/daterange.js`（G5） | `static/js/ui/` + `ui.css`/`theme.css` + 三个模板 | **必须在 B2/B4/B6 之前或与其同批**（否则三页各自改一遍）；影响面全站 → 建议单独一批 + 多端四输入验收 |
| **E1** | P2 收尾：`data-confirm-danger`、`.filter-bar` 语义、统计进标题、coupons 两按钮二选一 | 多模板 | **最后做**（与 B/C 批全部冲突） |

**并行规则摘要**：
- A1 → A2 串行（契约）；B1/B4/B5/B6/B7/C1/C2 各自独占模板 → **全并行**；B2 → B3 串行（同文件）；D1 优先于 B2/B4/B6 的控件部分；E1 最后。
- **跨批冲突点**：`customers.html`（B2/B3）、`theme_settings.html`+handler（A1/A2）、`static/js/ui/`（D1 与任何新控件）。

---

# 不要动的地方（已确认正确的样板）

| 样板 | 位置 | 为什么 |
|---|---|---|
| **空态三段式 + 主行动 + 清掉筛选** | `coupons.html:105-121` | 交易域唯一做全的空态：title 按 4 种场景分支（无工程 / 无匹配 / 按状态 / 真为空）、desc、`.empty-actions` 里主行动 + 条件性「清掉筛选」。**B1/B2 照抄它** |
| **带筛选的空态提示** | `returns.html:283-287` | 空态 desc 明确说出「当前还带着状态筛选「X」」+ 一键「清掉它」→ 用户知道为什么空、怎么退出。**orders/customers 应向它对齐** |
| **解绑确认说清后果** | `site_slots.html:137` | 「指向它的链接将不再生成（页面本身不受影响）」—— 既说影响也说边界，全站最好的确认文案之一 |
| **删除确认说清后果** | `coupons.html:244` / `:140` | 「有核销记录的券会被拒绝，请改用停用」—— 说了拒绝条件 + 替代动作 |
| **行内表单外置** | `theme.html:96-106`、`site_slots.html:136-149`、`coupons.html:227-248`、`customers.html:197-224` | 全部 `tdForms=0`，`form="<id>"` + 表格外 `.hidden-form`（`02-I` §5.2.3 已列，本域 4 页全合规） |
| **模式 A 的正确处理** | `customer_detail`（无 id / id 不存在 → 303 + `?err=客户编号不合法，请回到列表页重新操作。`） | **本域唯一修好的模式 A**，其余各页应向它对齐（`theme_settings` 是最差的反例） |
| **条件未满足时不给空下拉** | `site_slots.html:70-76` | 空态 desc 解释了「现在不给选择框，是因为一个空下拉只会让你点一个必然失败的提交」—— 与 `inventory_purchases.html:33` 同族的正确判断 |
| **筛选栏同维度只给一种控件** | `orders.html:67-71` / `returns.html:237-245`（状态只用徽章，下拉已删，注释说明了原因） | `admin-ui-logic` §2「同一维度只给一种筛选控件」的执行样本 |
| **可点击计数徽章** | `customers.html:66-68`（`aria-current` + ✓ + 加粗，不只靠颜色） | `02-I` §5.2.3 已列；本轮确认徽章本身正确（**但计数与空态文案脱节**，见 G4/任务） |
| **非法日期有兜底可见性** | `analytics.html:70-77`（「实际统计窗口」徽章） | 静默回退到默认窗口时，用户仍能从徽章看出真实窗口 → 不判缺陷 |
| **settings 的折叠策略** | `settings.html:113`（高级默认收起）/ `:171`（语言默认展开，注释 `:164-170` 说明充分） | `02-L` §5.2 已列「不要动」；本轮确认理由成立（语言区是功能区不是说明区） |
| **dashboard 的 KPI 卡 + 列表结构** | `dashboard.html:32-107` | 4 张 `stat-card` + 1 张 `list-card`，首屏 8/8 行、`hintReal=0`、空态三段齐 |
| **seo 的体检结果反馈机制** | `seo.html:47-68` + `:108-151` | `aria-live="polite"` + loading/成功/失败三态 + `escapeHTML` —— 反馈链路完整（**仅「内联脚本」这一形态待收敛**，见任务 2） |
| **`?done=` / `?ok=` 的读侧白名单** | `order_page_query.go:62-104` | 回带文案与写侧共用同一份模板字面量，防「手拼 URL 伪造系统消息」—— **theme_settings 加 `?ok=` 时照抄这个机制** |

---

# 对既有清单的更正

| # | 原记录 | 实测更正 |
|---|---|---|
| 1 | `02-L` §0.4「**settings**：首屏 0 行数据（**语言折叠区默认 `open`**）」+ `02-I` §5.1 `settings` 行 | **因果错**。本页唯一的 `table.data-table` 是 URL 结构表，在**默认收起的「高级」折叠区**里（`settings.html:113-157`，表在 `:141-154`），实测 `tableAt=1.05 屏`。语言区（`:171`，`open`）装的是**表单行**（`partials/locale_rows.html`），不是 data-table。→ 首屏 0 行与语言区无关；且 settings 是**配置页不是列表页**，判据 F3 不适用。两处记录需同步改口径（否则与「语言区不要动」并列会误导下一轮去改语言区） |
| 2 | `02-L` P1-18 / `02-I` §5.2.4 #38「`site_slots.html:107` 每行内嵌含 **13 option** 的 `<select>`，10 行 = **130** option 节点」 | **少算占位项**。实测 `perSelect=[14×10]`、`optionsTotal=**140**`（13 个真实候选 + 1 个 `<option value="">` 占位）；`htmlKB=64`、`elements=659`、`rows=10`。另补：**占位项文本与 `aria-label` 是同 10 个字**（`placeholderSameAsAria=[true×10]`）—— 同一句在同一元素里出现两次，原记录只记了「逐行重复」 |
| 3 | `02-L` P2-16「`orders` / `returns` / `customers` 空态无主行动（`coupons` 是正确样本）」 | **需三分**：① `orders`/`returns` 的**无工程分支有 `.empty-actions`**（`orders.html:97` / `returns.html:277`）→ 只有「有工程无数据」分支缺；② **`returns` 缺主行动是合理的**（退货申请由客户发起，后台无新建动作）→ 建议从该条移除；③ `customers` **有**主行动但是**死链接**（`href="/admin/customers"` == 当前 URL，实测 `sameAsCurrent:true`）→ 性质是「无效主行动」而非「无主行动」 |
| 4 | `02-I` §5.2.5 模式 A「**最差档 7 处**」（含 `theme_settings` 的 `:148/153/278/283`） | **低估**。同一文件还有 4 处未记录：`:372`（POST 校验失败 → **裸出 i18n key `MsgThemeSettingsInvalid`**，实测 400 + 23 字节，库里有中文值「主题设置不合法」却没过 i18n）、`:366`/`:378`（`response.ErrorWithMessage` → **JSON 响应**，message 也是裸 key `MsgInternalError`）、`:384`（`c.String(code, "主题设置保存成功，但页面刷新失败")` 裸文本，且**操作其实成功了**）。→ 最差档实际 **≥11 处**，且本域就占 4 处新的 |
| 5 | `02-I` §5.1 度量表：`/admin/settings` **1.25 屏**；`/admin/analytics` **2.15 屏**；`/admin/orders`、`/admin/returns`、`/admin/customers` 均标「**✅**」 | 屏数实测 `/admin/settings` = **1.32**、`/admin/analytics` = **2.17**（视口同为 1646×908，差异属重测正常浮动，但建议按本轮数字更新）。三页「✅」**不成立**：orders 有 P0（G2 文案承诺不存在的功能）+ 空态无 actions + 空态无表头；returns 空态无表头 + Note 在正文；customers 空态主行动失效 + 库值覆盖模板。`/admin/dashboard` 记「1.00 / 8/8 / ✅」**成立**（补：`cards=5` 而非 4，是 4 KPI + 1 列表卡） |
| 6 | `02-I` §5.2.3「值得推广的正面样本：**带计数的可点击徽章筛选** `customers.html:67`」 | 徽章本身**正确**（`aria-current` ✓、✓ 前缀 ✓、0 计数仍可点 ✓，本轮确认）。但**计数与空态文案脱节**：7 个徽章全显示 `0`（含「✓ 全部 0」），而空态同时说「或者上面的筛选条件太窄了 —— 先点「重置」看一眼全部账号」→ 总数是 0 时这句话自相矛盾。建议在该条后补一句限定 |
| 7 | `02-L` P1-12「缺 `.filter-bar`：…**`customer_detail`** …」（11 页） | `customer_detail` 是**详情页**（`.kv` + 操作卡 + 两个 form），`filter-bar` 不适用；它缺的是筛选能力（不需要）而非筛选栏。建议从 P1-12 移除（列表页那 10 页的结论不受影响） |
| 8 | `02-I` §5.2 #16 / `02-L` P2-8「`coupons.html:54` + `:114` 空数据时同屏两个『＋ 新建优惠码』」 | **确认存在**（实测 `pageActions="＋ 新建优惠码"` 且 `emptyActions="＋ 新建优惠码"`）。补：两者**不在同一处**（页头一个、空态一个），判据 §2.2 与 §7 各自都要求主行动 → 属「二选一」而非「bug」；建议保留空态那个（更贴近用户视线焦点），页头保留以满足 §2.2（或反之），并写明取舍 |

---

# 方法论新增候选（建议写进 `02-J` §H）

**H11. `innerText` 对 `<details>` 折叠内容返回空串 —— 会把「折叠」误读成「空」。**
实测：`/admin/settings` 的 URL 结构表（`settings.html:141-154`）模板里明明有 3 个 `<th>`（类型 / 路径模式 / 默认），但 `[...document.querySelectorAll('thead th')].map(th => th.innerText.trim())` 得到 `["","",""]` —— 因为整张表在默认收起的 `<details class="section-fold">` 内，Chromium 的 `innerText` 对不可见内容返回空。
**判定方法**：度量任何「表格列头 / 文本内容」之前，先 `document.querySelectorAll('details:not([open])').forEach(d => d.open = true)` 再读数；或改用 `textContent`（不受可见性影响，但要自行排除 `<script>`/`<template>`）。

**H12. `<td colspan>` 全宽提示行是 F1 口径的盲区。**
实测：`customers.html:184-189` 的 `{{if r["PendingHint"]}}` / `{{if r["LockHint"]}}` 各插一行 `<tr><td colspan="7" class="hint">整句说明</td></tr>`（文案源 `customer_view.go:161/165/168`）。按 `02-J` §F1 修正口径，`.hint` 的 `closest('table')` 为真 → 被当单元格排除 → `hintReal` 读数为 0；但它的内容是**整句操作说明**，且**每行重复**，属 §2.1 的真实违反。
**判定方法**：F1 的排除规则里 `!e.closest('table')` 应改成 `!e.closest('table') || !!e.closest('td[colspan]') || !!e.closest('td.hint')` —— 即「单元格内的值」排除，「跨列提示行」不排除。

**H13. 「有主行动」不等于「主行动有效」——必须点一次。**
实测：`customers.html:110` 的 `.empty-actions` 在**无筛选**时也渲染，`href="/admin/customers"` 与当前 URL 逐字相同 → 点击后 URL 与页面均不变（`sameAsCurrent: true`）。`02-J` §G1 只判「空态是否含主行动」→ 会把这种死链接判为通过。
**判定方法**：G1 追加一步 —— 取 `.empty-actions a` 的 `href`，与 `location.pathname + location.search` 比较；相等即「无效主行动」。若 `.empty-actions` 里是按钮，则检查它是否 `disabled` 或指向当前页。

---

## 附：本轮实测命令与产出（可复现）

```bash
# 基础度量（harness 默认口径）
cd /tmp/verify && cat > lcs-trade/pages.json <<'EOF'
[{"name":"orders","path":"/admin/orders"},{"name":"returns","path":"/admin/returns"},
 {"name":"coupons","path":"/admin/coupons"},{"name":"customers","path":"/admin/customers"},
 {"name":"dashboard","path":"/admin"},{"name":"theme","path":"/admin/themes"},
 {"name":"settings","path":"/admin/settings"},{"name":"seo","path":"/admin/seo"},
 {"name":"analytics","path":"/admin/analytics"},{"name":"site_slots","path":"/admin/site-slots"}]
EOF
node harness.js lcs-trade/pages.json

# 逐页独立 Chrome（避免 dev-login 轮换会话互相踢掉）
node lcs-trade/cases.js <页名>        # orders|returns|coupons|customers|analytics|siteslots|themes|settings|seo|dashboard|a_*
node lcs-trade/dump.js <pages.json>   # 结构 dump（控件/表单/确认/空态/模板）
node lcs-trade/extra.js               # 滚动需求 / option 计数 / 写失败出口实测
```
产出：`/tmp/verify/lcs-trade/{out1,dump1,dump2,dump_ts,c-*,pf-*}.txt`

---

# 批次执行台账（后续轮次补齐，2026-09）

记录 §586 批次表里**已逐条回读核实过**的批次。判据只采信「打开文件 / 查库 / 实测」的现场结论，
不采信迭代过程中的口述与计数 —— 本轮就抓到三次「按 grep 计数下的判断」与实际语义相反（见 C1）。

| 批 | 状态 | 现场证据 |
|---|---|---|
| **B3** | **已完成** | ① G4（库值覆盖模板兜底）由 `417_fix_trade_pages_i18n.sql` 落地：实测库值 `admin.customers.list.empty_desc` = 「客户是访客在站点上自己注册出来的，后台不能直接新建 —— 完成注册后会出现在这里。」（即模板兜底那句）、`empty_heading` = 「还没有客户」；② 孤儿词条 `admin.customers.empty`：418 删库行，并已从 `register_admin_i18n.go` 的 190 批 key 列表移除（门槛 765 → 762）；190 seed SQL 里对应的两行 INSERT 亦已删除。**对账实测**：190 SQL 的 key 集合与条件列表双向 diff 均为空（762 = 762），符合 419 立下的「条件列表里的 key 全部仍由 seed SQL 写入」 |
| **C1** | **已完成** | `dashboard.html` 的列表卡标题行已是 `.card-header` + `.card-title`；文件内仅存的 `filter-bar` 字样出现在**注释**里（解释为什么不再借用它）。此前按 `grep -c filter-bar` 判为「未做」是判据选错 |
| **D1** | 本轮落地 | 基座控件 `internal/templates/static/js/ui/daterange.js`（渐进增强，原生两个 `input[type=date]` 保留在 DOM）+ `customers.html` / `analytics.html` 接入。**coupons 不在接入范围**：它的时间窗是 `type="datetime-local"`（需要时分），按 `02-K` §3「需要时分时用原生 `datetime-local`，无需新组件」保持不动。§501 风险①的正式决策：**时间范围留在自己的卡里**（`.card-header` 承载「时间范围」标题 + 独立 `.filter-bar`），不并入下方列表卡的筛选栏 —— 理由见该条原文：并入会回到「filter-bar 承载标题」的坑 |
| **E1** | 已完成 | ① `data-confirm-danger`：`site_slots.html` 已补（实测该解绑表单同时有 `data-confirm` 与 `data-confirm-danger`）；② `.filter-bar` 语义：本轮修 4 处 + 记 1 处遗留（见下节）；③ 统计进标题：coupons 的列表标题与其「共 N 张」统计已移入 `.card-header`，且 `{{if .Total > 0}}` 保证空时不显示统计；④ coupons 两处「＋ 新建优惠码」：经复核属**刻意并存**（页头那处由 `admin-ui-logic` §2.2 要求、空态那处由 §7 要求），已在模板写明取舍与判据出处 |

**其余批次（A1 / A2 / B1 / B2 / B4 / B5 / B6 / B7 / C2）已逐条复核完毕**（只读走账，主会话抽验 7 条证据全部属实）：
完成 28 条 / 未完成 0 条 / 判据过时 2 条，未发现「提交声称做了但实际没做」。
- A1：`theme_settings_admin_pages.go` 全部出口收口（`projectErrRedirect` / `themeSettingsFailPage` 就地重渲 / 成功 303+`?ok=`），无 `c.String`/JSON 残留；
- A2：错误槽位（`role="alert"`/`role="status"`）、页头保存按钮 `form="theme-settings-form"`、分组折叠（`details.section-fold`）均已落地。**判据过时 2 条**：「49 label i18n」改由 `theme_field_groups.go` 服务端字段表数据驱动（模板只对固定文案走 `t(key,兜底)`）；折叠的 `Collapsed` 字段由模板 `open` 属性等价实现 —— 语义结果均达成；
- B1：orders/returns 空态三段齐、详情 id 不存在经 `orderFacingError` 进 `pageErr`、returns Note 已进 `.help-pop`；
- B2：死主行动改为仅 `FilterActive` 时渲染「重置」（H13 判据写进注释）；
- B4：时间窗 `datetime-local step=1`、`.card-header` 承载标题与 `Total > 0` 统计、非法 status 原样回显；
- B5：行内 13 项 select 改 `#slot-bind-panel` 绑定面板、占位符/aria 去重、装载失败与无工程清单两档空态分流、`data-confirm-danger` 已加；
- B6：tabs ×5、空态宏收敛、daterange 接入；
- B7：删除确认说清后果（设置删除 + 页面改挂激活主题 + 激活中不可删）、空态三段齐（换 key 避开旧库值死指向）；
- C2：seo 占位空态补 `empty-desc`（复用 `.help-pop` 同一 key，避免同一件事写两句）。

## 补记：`.filter-bar` 承载列表标题的实际处数（`02-I` #29 同族）

§38 记了 5 处（inventory 三页 + coupons + dashboard）。逐条回读后 **inventory 实为 4 处** ——
`inventory.html` 有两处（:64 的列表标题行「库存流水（最近 N 条）」、:122 的卡内小节标题「该 SKU 的各仓库存」），
审计当时把这两处与另外两页记成了「三页各一处」。

修法：标题移出 `.filter-bar`，进 `.card-header` + `.card-title`（`.filter-bar` 只留筛选表单）。
其中**三处原先借的是 `.fold-title`** —— 按 `theme.css` §13 的注释，那个类「只在 `.section-fold > summary`
里有效，被列表卡标题借用时是**零样式**」，即「库存流水（最近 N 条）」这类标题当时是以正文样式渲染的。

**后续批次已收口（P2）**：`inventory_sources.html` 的「筛选」属于冗余的筛选栏自报标签，
且旧 `.filter-row > .fold-title` 在此为零样式；该标签已删除，状态筛选的 `.help` 则移到对应
状态字段旁。词条 `admin.inventory_sources.filter.title` 已经 436 seed 删除存量，191 seed 的
SQL 与 ConditionSQL 列表、门槛同步去除（582 → 581），避免启动时反复插回。
