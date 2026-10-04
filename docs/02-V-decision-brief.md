# 02-V · 需要产品决策的 6 条（决策简报）

> 内部过程文档（非对外使用）

> **性质**：只调研、未改一行代码。产出是「现状事实 + 可选方案 + 代价/影响面/风险」，
> 供决策者挑一条（或直接关掉）。
>
> **来源**：交易域与站点域的操作逻辑审核（`docs/02-O-trade-site-audit.md`）与任务状态核对。
> 所依据文档的**行号已全部失效**，本简报一律按语义定位（函数名 / 类名 / 关键文案 / i18n key），
> 不引用行号。
>
> **方法**：① 读模板、handler、service、契约与迁移；② 用库值实测（`sys_i18n` 直查）核对
> 「模板兜底」与「库值」哪个在生效；③ 用 `routes.snapshot`、渲染级测试、`git log` 反向核对
> 「文档说没做」与「代码里到底做没做」。库值读数取自本机 `wp` 库（`sys_i18n`），
> 连接与口令来自 `config.yaml`。
>
> **时间基线**：`HEAD = 7521d27a`。**调研期间工作区出现了大量未提交改动**（有并行批次正在按
> `docs/02-O` 的批次表施工，见 §0.5）——本文档对每条现状的裁定均以「`HEAD` 已提交内容 +
> 库值实测」为基线，并在 §0.5 标出哪些条目正被并行批次触碰。

---

## 0. 先看这三条纠偏（否则本简报的 6 条会读错）

### 纠偏 1：第 1 / 2 / 3 / 4 条**已经落地**，不是待决策状态

`commit 679f4dac`（`feat(admin): 后台模板按模块分目录 + …`）这批里已经包含交易域空态改造，
配套迁移与测试同一批提交：

| 载体 | 内容 |
|---|---|
| `public/migrations/417_fix_trade_pages_i18n.sql`（注册见 `register_trade_pages_i18n.go`） | 修正 `admin.orders.list.empty_tail`、`admin.customers.list.empty_heading`、`admin.customers.list.empty_desc`、`admin.customers.field.registered_at/_to` 的**库值**；新增 `admin.customers.action.disable/_enable` 与 6 条 `admin.returns.note.*` |
| `public/migrations/418_retire_customers_empty_i18n.sql`（注册见 `register_customers_empty_retire.go`） | 退役孤儿词条 `admin.customers.empty` |
| `internal/templates/admin/order/orders.html` | 空态删掉「（或后台代客建单）」括注；主行动加 `.FilterActive` 守卫 |
| `internal/templates/admin/user/customers.html` | 空态主行动加 `.FilterActive` 守卫 |
| `internal/templates/admin/user/customer_detail.html` | 状态按钮改为 `{{ .["t"](.StatusActionKey, .StatusActionLabel) }}` + `account_suffix` |
| `public/test/order/feature/trade_empty_state_honesty_test.go` | 7 个用例钉住 orders / returns 的空态与文案一致性 |
| `public/test/user/feature/customer_empty_state_honesty_test.go` | 2 个用例钉住 customers 空态主行动与状态动词可翻译 |

**库值实测**（`sys_i18n` 直查，非读迁移文件）：

```text
admin.orders.list.empty_tail        zh-CN  台前下单后就会出现在这里。若已经下过单，检查上面的筛选条件是不是过窄了。
admin.customers.list.empty_heading  zh-CN  还没有客户
admin.customers.list.empty_desc     zh-CN  客户是访客在站点上自己注册出来的，后台不能直接新建 —— 完成注册后会出现在这里。
admin.customers.action.disable      zh-CN  停用        en-US  Disable
admin.customers.action.enable       zh-CN  启用        en-US  Enable
admin.customer_detail.action.account_suffix zh-CN 这个账号  en-US  " this account"
```

→ 第 1 / 2 / 3 / 4 条的**文案与行为**都已成型并生效。本简报对这 4 条的任务因此变成
「确认现状是否需要再动」，而不是「从零决策」。

### 纠偏 2：「order 模块没有任何建单端点」这句话**范围不准**

这句话出现在三处（`docs/02-O` orders 任务 1、417 迁移注释、`orders.html` 的模板注释），
但**只有对「后台页面路由」成立**：

```text
internal/module/order/inbound/http/order_router.go
  g := rg.Group("/order")
  g.POST("/create", permission.OrderCreate, h.CreateOrder)      ← POST /api/order/create 存在
```

而且后端**已经把「后台代客建单」当成一等场景**：

- `internal/permission/codes.go`：`OrderCreate Perm = "order:create"`，中文名 **「新建订单」**（权限点已存在、已声明、无需新迁移）；
- `Handle.CreateOrder`（`order_handle.go`）：`operatorFromContext(c)` 取到操作人时**自动把 `CreatedVia` 置为 `"admin"`**，并落 `CreateBy`；
- `CreateOrderReq`（`dto/order_req.go`）：`CreatedVia` 注释写「checkout / admin / api」；`AdminNote` 注释写「自建订单（createdVia=admin）与代发订单的填写位置」；`Attribution` 注释写「允许为 nil（**后台代客下单**没有访客上下文）」；
- `service/order_create.go` 的 `operatorTypeOf(createdVia)`：`CreatedViaAdmin → OperatorTypeAdmin`，进状态流转记录的 `operator_type`；
- `service/order_create_persist.go` 用 `operatorTypeOf(d.head.CreatedVia)` 落库。

`public/test/order/feature/trade_empty_state_honesty_test.go` 的 `TestOrdersEmptyCopyMatchesRoutes`
判据 ① 只扫 `routes.snapshot` 里 **`/admin/orders` 前缀**（`/create`、`/new`、`/place`、`/draft`），
**不扫 `/api/order/*`**。所以「没有建单端点」的**准确表述**是：

> `/admin/orders` 页面路由下没有建单入口；`POST /api/order/create` 端点在，且带 admin 语义。
> 缺的是**后台页面 UI**，不是建单能力。

这一条直接决定第 1 条的成本量级（见 §1）。

### 纠偏 3：`page.Service.ListStalePages` 已实现，**当前零调用方**，且它的注释点名了归位目标

`internal/module/page/service/page_stale_overview.go` 的文件头写着：

> 两个消费者：
>   1. 后台只读展示（**/admin/pages 的待重建区块**）：`ListStalePages` —— 全站清单，按标记时间倒序……
>   2. 写侧的回执与结构化日志：`StaleImpactOfIDs` ……

实测：`ListStalePages` 全仓只有 **contract 声明 + service 实现**两处，**没有任何 handler 调用**。
它已经做完的事：跨工程逐工程设作用域、全局排序、`staleOverviewLimit = 8` 截断、总数、截断标记、
失败不降级（`ErrProjectRequired` / 原样返回 err）。

→ 第 6 条的「搬 /admin/pages」不是从零造查询，而是**给一个为它写好的能力接线**。
代价被此前的逐条核对严重高估了。

---

## 0.5 并行批次占用（决策前必读：避免派重复的活 / 改到别人手上）

调研过程中工作区从「干净」变为**16 个文件未提交改动 + 4 个新增文件**，内容是
`docs/02-O` 的批次表在施工（B1 / B2 / B4 / B5 / B6 / B7 / A1 / A2 各批）。
下表是**本文档 6 条**与这些在途改动的交集 —— 「未被触碰」的条目才适合现在派活：

| 本文条目 | 并行批次是否正在碰 | 证据（未提交 diff） | 结论 |
|---|---|---|---|
| 1. orders「后台代客建单」 | **部分**：`orders.html` 正在被改，但改的是 T4（非法 status 提示），**括注仍是已删状态** | `orders.html` 的 `.empty-desc` 追加了 `returns.list.status_filter_lead/tail` 复用；`order_page_handle.go` 新增 `FilterLabel` 注入 | 条文案的现状与我方结论一致；**别在同一文件里重复改空态 desc** |
| 2. orders 空态主行动 | **未触碰**（`{{if .FilterActive}}` 守卫原样保留） | 同上 diff 中该段只有上下文行 | 可安全决策 |
| 3①. customers 空态主行动 | **未触碰** | `customers.html` 的 diff 全在表头 `.help` 与删 colspan 提示行 | 可安全决策 |
| 3②. customers 库值 | **未触碰**（417 的 UPDATE 仍是最后一条改它的迁移） | 新迁移 `431_i18n_customers_status_help.sql` 只补 `status.help.*` 3 个 key | 可安全决策 |
| 3 的清单外发现（`empty_filtered_*` 缺 seed） | **未被修**：431 没有补这两个 key | 431 全文只含 `admin.customers.status.help.{pending,locked,failures}` | **仍然开放**，可直接派活 |
| 4. `StatusActionLabel` | **未触碰**（`customer_view.go` 的 diff 只加注释，`StatusActionKey` / `StatusActionLabel` 逻辑未变） | `customerRow` 的 switch 原样 | 结论不变，且本条本就无需动作 |
| 5. site_slots `pick_project` 空态 | **未触碰该分支**；并行批次改的是**同页另外那个空态**（`no_rows` 装载失败兜底：补 `.empty-title` / `.empty-desc` / `.empty-actions`「重新加载」） | `site_slots.html` 的 diff 里 `{{if … NoProjectEmpty}}` → `admin.site_slots.pick_project` 那一段**只有上下文行，无改动** | **仍然开放**；派活时务必说明是 `pick_project` 那一段，不是 `no_rows` 那一段 |
| 6. 影响面归位 | **未触碰**（`pages.html` / `pages_handle.go` / `block_page_impact.go` / `article_stale_impact.go` 都不在 diff 里） | `git diff --stat` 不含这些文件 | 可安全决策 |

**另外两个在途信号**（不属本文 6 条，但会影响派活）：

- `public/migrations/431_i18n_customers_status_help.sql` + `register_customers_status_help_i18n.go`
  —— 「新建迁移 + 新建 register 文件」的模式正在被使用，派活时注意**迁移版本号要避让 431**。
- 残留临时文件 `internal/module/order/inbound/http/zz_tmp_coupon_status_test.go`、
  `internal/templates/zz_tmp_coupons_batch_verify_test.go`（自述「临时文件，跑完即删」）
  —— 派活前先确认并行批次已收尾，别把临时文件当既有资产。

---

## 1. orders 空态：删文案，还是补「后台代客建单」端点？

### 现状（回代码 + 库值实测）

- 模板：`internal/templates/admin/order/orders.html` 的「有工程但无订单」空态分支
  （`{{else}}` 里那一段 `.empty-state`）。desc 只为一句 i18n：
  `admin.orders.list.empty_tail` =「台前下单后就会出现在这里。若已经下过单，检查上面的筛选条件是不是过窄了。」
  **括注「（或后台代客建单）」已删，且模板里留了注释说明删因**。
- 库值：实测已是删掉括注后的句子（见 §0 纠偏 1）。
- 页面路由实测（`order_router.go` 的 `if pages != nil` 段）：
  `GET /admin/orders` + `POST /orders/{status,cancel,refund,note,bulk-status,bulk-cancel}` —— 无建单入口。
- 端点侧：`POST /api/order/create` + `order:create` 权限点 + admin 语义**齐备**（§0 纠偏 2）。
- 测试：`TestOrdersEmptyCopyMatchesRoutes` 三条判据（路由表无建单端点 / 页面无「代客建单」/ 库值无旧承诺）。

### 方案

**A. 保持现状（删文案路线，已完成）**

- 改什么：无。已付的代价 = 1 条 UPDATE 迁移 + 1 处模板删词 + 1 个测试用例。
- 代价：**0**（已完成）。
- 影响面：关闭 `02-O` orders 任务 1；此前的逐条核对里「空态括注承诺了不存在的入口、应删」那条也可结案。
- 风险：**注释与文档里的因果表述不准**（「没有任何建单端点」）。要么改措辞为「/admin/orders 页面路由无建单入口」，
  要么保持 A 的同时接受「读注释的人会以为后端也没有能力」。这是**文档债**，不是功能债。

**B. 补后台建单页（真做代客建单）**

- 改什么（按层列）：
  - **controller/handler**：新增 `internal/module/order/inbound/http/order_new_page.go`（GET 表单页 + POST 提交），
    或复用 `order_page_handle.go` 加两个方法。POST 侧走**现有 service 用例**，不新写建单逻辑。
  - **路由**：`order_router.go` 的 `if pages != nil` 段加 `pages.GET("/orders/new", …)` 与
    `pages.POST("/orders/create", builtin.CasbinMiddlewareForPath("/api/order/create"), …)`
    —— 权限点**复用 `order:create`**，**不需要新权限点、不需要 seed 迁移**（与 `order:note` / `bulk-*` 的既有手法一致）。
  - **模板**：新增 `internal/templates/admin/order/order_new.html`。字段面很大：
    工程 / 客户邮箱·姓名·电话 / 收货与账单地址 / **订单项（变体 + 数量，可多行）** / 支付方式 /
    运费 / 优惠码 / 备注。订单项录入是**当前没有现成控件**的一项 ——
    `internal/templates/static/js/ui/` 只有 `select` `mediafield` `colorfield` `iconfield`，
    没有「按关键词搜商品 → 选变体 → 加行」的选择器；可借鉴的先例是
    `product_bundle.html` 内嵌的片段选择器（`hx-get /_fragments/bundleConfigurator`）与
    `product_create_form.html` 的变体行表单。**这是本方案的主要工作量所在。**
  - **service / model**：**零改动**（`CreateOrder` 已支持 `CreatedVia=admin`、`CreateBy`、`AdminNote`、
    `Attribution=nil`）。
  - **测试**：新增渲染级用例（页面能渲染 + 提交走通）；**必须改** `trade_empty_state_honesty_test.go` 的
    `TestOrdersEmptyCopyMatchesRoutes`（判据 ① 要**反转**成「存在建单端点则文案必须提到它」）；
    `internal/routers/testdata/routes.snapshot` 需重新生成（路由表快照）；
    `orders.html` 空态文案要**还原**（并再写一条 UPDATE 迁移把库值改回来 —— 模板兜底不是真源）。
- 代价：**L**（1 个新页面 + 1 个新控件 + 3~4 个测试文件 + 1 条回调迁移）。量级与「商品新建整页」同档。
- 影响面：订单模块 handler/路由/模板/测试；`routes.snapshot`；`02-O` orders 任务 1 的结论反转。
- 风险（**这一条是必须先定的产品语义，不是代码细节**）：
  - `service/order_create_draft.go` 的 `buildOrderDraft` **无条件**调用 `ensureGuestAccount`；
    `ensureGuestAccount` 只看 `req.UserID != nil`，**不看 `CreatedVia`** → 后台代客建单时若客户邮箱
    没有账号，会**自动开号并发初始密码邮件**给客户。代客建单的真实场景（电话单、老客户、线下单）
    里这可能是误发。**补 UI 之前必须先定：admin 入口是否跳过开号 / 是否跳过发信。**
  - 建单链路还有：库存扣减（失败补偿）、订单号生成、优惠码核销（同事务）、
    支付落账后的 `order.paid` webhook 派发。后台代客建单要不要「建单即成单（免支付）」，也是产品决策。

**C. 保留暗示，改成「后台暂不支持代客建单」的前置说明**

- 改法：把 desc 改成否定式前置声明。
- 代价：**S**（1 模板 + 1 UPDATE 迁移 + 1 测试断言）。
- 影响面：同上测试。
- 风险：`02-O` 已论证「写清楚比留暗示好」，但**一个不存在的功能写进空态就是噪声** ——
  用户不会问「能不能代客建单」，除非你先提。**不推荐。**

### 结论

**保持 A。** 若产品确实要代客建单，**单开一条「后台建单页」需求**，并且该需求的第一个决策点不是 UI，
而是「admin 入口下的开号 / 发信 / 免支付语义」。顺带把三处注释里「没有任何建单端点」的措辞
修正为「`/admin/orders` 页面路由无建单入口」。

---

## 2. orders「有工程但无数据」空态该给哪个主行动？

### 现状（回代码）

- 模板 `orders.html`：空态整行进 `<tbody>`（`colspan` = 8，表头常驻）。
- 三个分档全在：`LoadFailed`（装载失败）→ 只说「这一页的数据没能读出来」；
  `len(.Projects) == 0`（无工程）→ `.empty-actions` 指向 `/admin/pages`；
  `else`（有工程但无订单）→ desc 提示「检查上面的筛选条件是不是过窄了」。
- **「有工程但无数据」这一档的现状是：`.FilterActive` 为真时给「重置」**
  （`{{if .FilterActive}}` 守卫，href = `/admin/orders?project={{selectedProject}}`，
  文案 key `admin.orders.filter.reset`）；无筛选时**不给**任何主行动。
- `FilterActive` 由 handler 算好注入：`order_page_handle.go` 的
  `"FilterActive": filter.Status != "" || filter.Keyword != "" || filter.PaymentMethod != ""`
  （判据与 customers 页的 `customerFilterActive` 同名同义）。
- 测试：`TestOrdersEmptyStateActionsFollowFilter` 断言「无筛选 0 个 action / 带筛选恰好 1 个 /
  href 必须带 project / 不得把 keyword 带回去」。

### 方案

**A. 保持现状（条件性「重置」）**

- 代价：**0**（已完成，且测试已钉）。
- 影响面：无。
- 风险：文案是「重置」而不是 `02-O` 建议的「清掉筛选」。**这其实更好** ——
  筛选栏那颗按钮用的就是同一个 key（`admin.orders.filter.reset`），同一动作同一词，
  用户不会怀疑两者是否等价。缺点：空态里的语义是「清掉筛选」，而「重置」需要用户自己联想到筛选栏。

**B. 空态专属文案「清掉筛选」（`02-O` 原推荐）**

- 改法：`.empty-actions` 里的 key 换成新 key（如 `admin.orders.filter.clear`）或改现有 key 的值。
- 代价：**S**（1 模板 + 1 条 seed 迁移）。
- 影响面：`TestOrdersEmptyStateActionsFollowFilter` 只断言 href 的数量与内容，**文案变了测试仍绿**
  → 测试不会拦，得靠人工核。
- 风险：**不要改 `admin.orders.filter.reset` 的值** —— 筛选栏那颗按钮会一起变，
  「筛选栏的重置」与「空态的清掉筛选」表达同一动作却分离成两处文案，反而制造不一致。

**C. 不给主行动，只在 desc 里说明原因**

- 改法：删掉 `{{if .FilterActive}}` 那一段。
- 代价：**S**（删 4 行 + 改测试）。
- 影响面：`TestOrdersEmptyStateActionsFollowFilter` 的「带筛选恰好 1 个」断言必须改。
- 风险：`02-O` 的原始判据是「desc 说检查筛选，却不给清掉筛选的按钮 → 用户多 3 步」。
  走 C 等于把这个判据判死。

### 结论

**保持 A。** 不建议为「重置 vs 清掉筛选」这一个词付一条迁移 + 一轮测试改动；
真要统一口径，应该在**全站**层面定「筛选栏按钮 = 重置 / 空态按钮 = 清掉筛选」的写法规则，而不是单页改。

---

## 3. customers 空态的两个问题

### 现状（回代码 + 库值实测）

**① 主行动「重置」的 href 等于当前 URL —— 已修。**
`customers.html` 的空态现在是：`.empty-title` / `.empty-desc` 按 `{{if .FilterActive}}` 分叉，
`.empty-actions` 同样被 `{{if .FilterActive}}` 包住；无筛选时**整个 `.empty-actions` 不渲染**。
测试 `TestCustomersEmptyStateActionsFollowFilter`（走真 handler + 真模板 + 真库）钉住：
无筛选时页面里不得出现 `class="empty-actions"`；带筛选时应出现且 href 不含 `keyword=`。

**② 「后台不能直接新建」被库值覆盖掉 —— 已修。**
417 迁移用带旧值前置条件的 `UPDATE` 把库值改成与模板兜底一致的句子，实测库值已是
「客户是访客在站点上自己注册出来的，后台不能直接新建 —— 完成注册后会出现在这里。」
`empty_heading` 库值也从「没有符合条件的客户」改成「还没有客户」。

**顺带发现（原报告未记）**：模板在**筛选态**用的是另外两个 key ——
`admin.customers.list.empty_filtered_heading`（「该筛选条件下暂时没有账号」）与
`admin.customers.list.empty_filtered_desc`（「徽章上的数字是各口径的总数……」）。
**这两个 key 在 `sys_i18n` 里不存在**，全仓 `.sql` / `.go` 也没有它们的 seed
（实测：`like 'admin.customers.list.empty%'` 只返回 `empty_desc` / `empty_heading` 各两语言）。
→ 英文界面下这两个分支**回落中文兜底**。
417 的注释里还专门写了「有筛选时走的是另一个 key（`empty_filtered_heading`），不受影响」——
说明作者知道这个分叉，但漏了 seed。

### 「保留哪句话、主行动该指向哪」这个决策点是不是真的？

- 「保留哪句话」是**假二选一**：模板已经按 `FilterActive` 分成两档，两句各自服务一个场景 ——
  无筛选档要回答「我能不能在这儿建客户」（→「后台不能直接新建」）；
  筛选档要解释「徽章上的总数为什么和空列表不一致」（→「徽章数字是各口径总数…」）。**两句都要留。**
- 「主行动该指向哪」**事实已定**：带筛选 → 指向不带筛选的本页（`/admin/customers`，无 query）；
  无筛选 → 不给主行动。若将来要给无筛选态一个真出路，可考虑「去前台下单/注册页」，
  但那需要前台注册 URL（当前模板没有这个数据）。

### 方案

**A. 保持现状 + 补两个 filtered key 的 seed**

- 改法：新增一条 i18n 迁移（仿 417 的形态：`INSERT ... ON CONFLICT (item_key, lang) DO NOTHING`
  + 自带 `init()` 注册），补 `admin.customers.list.empty_filtered_heading` /
  `admin.customers.list.empty_filtered_desc` 的中英各一行。
- 代价：**S**（1 SQL + 1 register 文件）。
- 影响面：i18n 一致性；无既有测试会被破坏（现有 `TestCustomersEmptyStateActionsFollowFilter`
  断言的是 `class="empty-actions"` 与标题/说明的中文串，不受影响）。
- 风险：低。注意 `sys_i18n` 主键是 `(item_key, lang)`，同批写入要成对；不同批次写同名 key 是
  「版本号小的先执行、先写者胜出」，所以新 key 不与存量冲突。

**B. 取消筛选态分叉，两档合回一个 key**

- 改法：模板删掉 filtered 两个 key 的分支，统一用 `empty_heading` / `empty_desc`。
- 代价：**S**（1 模板）。
- 影响面：`TestCustomersEmptyStateActionsFollowFilter` 的「带筛选时标题」相关断言（若有）。
- 风险：**丢信息** —— 筛选态那句「徽章数字是各口径总数」是用户在「徽章显示 0、列表也空」时
  唯一能解释矛盾的话（`02-O` 更正 6 专门记了「计数与空态文案脱节」）。不推荐。

**C. 什么都不做**

- 代价 0。风险：英文界面上筛选态空态永远是中文（同族问题 `02-I` 记过多次，属「信息缺失型」）。

### 结论

**A。** 决策点里唯一还需要真决策的其实是**新 key 的英文文案**（要成对、要说明「徽章计数不受筛选影响」）。
「保留哪句 / 主行动指向哪」两条都已由代码定死，不需要再议。

---

## 4. `{{.StatusActionLabel}}这个账号` 到底读作「停用」还是「启用」？

### 现状（回代码 —— 这是本简报里事实最确定的一条）

`customer_view.go` 的 `customerRow(item *userdto.CustomerResp) gin.H` 里，状态动作只有两个方向：

```go
switch item.Status {
case customerStatusActive:      // 1
    row["Actionable"]      = true
    row["NextStatus"]      = customerStatusDisabled
    row["StatusActionKey"] = customerActionDisable     // "admin.customers.action.disable"
    row["StatusActionLabel"] = "停用"
case customerStatusDisabled:    // 0
    row["Actionable"]      = true
    row["NextStatus"]      = customerStatusActive
    row["StatusActionKey"] = customerActionEnable      // "admin.customers.action.enable"
    row["StatusActionLabel"] = "启用"
case customerStatusPending:     // 2
    row["PendingHint"] = "待激活：客户还没完成邮箱验证。……"
}
```

- `customerStatusActive = 1` / `customerStatusPending = 2`（`customer_handle.go`）；
  待激活**刻意不给按钮**（`Actionable=false`，模板走 `{{else}}` 只渲染 `.hint`）。
- 取值域因此只有两种：当前**正常** → 按钮写「**停用**」；当前**已停用** → 按钮写「**启用**」。
- 模板 `customer_detail.html` 的按钮已经是**两段各自取当前语言**：
  `{{ .["t"](.StatusActionKey, .StatusActionLabel) }}{{ .["t"]("admin.customer_detail.action.account_suffix", "这个账号") }}`
  —— 中文出「停用这个账号」/「启用这个账号」，英文出 "Disable this account" / "Enable this account"。
- 库值实测：`admin.customers.action.disable` = 停用 / Disable；`enable` = 启用 / Enable；
  `account_suffix` = 这个账号 / " this account"。**中英都不混排。**

### 确定答案

> **两种都可能，但不是歧义，而是同一个按钮在两种状态下必须换词。**
> 读作「停用这个账号」当且仅当该客户当前状态是「正常」；
> 读作「启用这个账号」当且仅当当前状态是「已停用」。
> 按钮的语义始终是「切到 `NextStatus`」，label 描述的是「这一下要做什么」——
> 与提交的 `toStatus` 隐藏域一致，无逻辑错误。

「中英混排」的原始担心（`StatusActionLabel` 是中文字面量、后缀走词条）**已经不存在**：
动词本身已 key 化，模板优先取词条、字面量只作兜底。

### 方案

**A. 保持现状（key + 中文兜底两份并存）**

- 代价 **0**。风险：低 —— 两份真源的漂移面是「改了 Go 里的兜底中文但没改词条」，
  而词条命中时页面显示的是词条值，Go 字面量只在**缺词条**时才现形（英文界面缺词条会回落中文，
  这正是 417 新增 `action.disable/enable` 要解决的问题）。

**B. 收敛为一份真源（删掉 Go 里的中文字面量，兜底串移到模板）**

- 改法：`customerRow` 不再设 `StatusActionLabel`；`detailData` 相应移除；
  模板改成 `{{ .["t"](.StatusActionKey, "停用") }}` 这类写法会失去「按状态给不同兜底」的能力
  → 需要 `{{if .Actionable}}…` 或新增 `StatusActionFallback`。**成本高于收益。**
- 代价：**S~M**（1 Go 文件 + 1 模板 + 1 测试开关）。
- 影响面：`TestCustomerStatusActionVerbIsTranslatable`、`customer_view.go` 的 `detailData`。
- 风险：低，但改动落在**列表页与详情页共用的 `customerRow`** 上，两处一起受影响。

**C. 把「这个账号」并进 Go 服务端整句**

- 代价 **M**（要引入 i18n 到 Go 侧拼句路径，等于绕开模板取词机制）。
- 不推荐：`02-O` 提到的「动词也 key 化」的既有手法（orders/returns 的批量文案注释）已经把方向定成
  「两段各自走词条」，本页是同一手法的落地。

### 结论

**A，不需要决策，也不需要改动作。** 这条从「待确认」直接结案：
值是确定的两种，「这个账号」的拼法已 key 化，中英各自成句。

---

## 5. `site_slots` 空态文案指向不存在的控件

### 现状（回代码 + 库值实测 —— **未修**）

- 模板：`internal/templates/admin/page/site_slots.html`，槽位卡里
  `{{if isset(.NoProjectEmpty) && .NoProjectEmpty}}` 分支 → `.empty-state` 里**只有** `.empty-desc`：
  key `admin.site_slots.pick_project`，文案「**先在上面选一个站点工程。**」
- 库值实测：`admin.site_slots.pick_project`（zh-CN）=「先在上面选一个站点工程。」、
  （en-US）=「Pick a site project above first.」—— **仍在库里、仍在渲染**。
- 关键事实（决定这条的性质）：`NoProjectEmpty` 由 handler 算好并注入 ——
  `internal/module/page/inbound/http/site_slot_handle.go` 的
  `"NoProjectEmpty": len(projects) == 0 && pageErr == ""`；
  而页头工程选择器的渲染条件是 `{{if len(.Projects) > 1}}`。
  **两者互斥**：`len(projects) == 0` 时页头**必然**没有选择器。
  → 这句文案指向的控件在它的**唯一触发场景**下**不可能存在**。这是永久性的 G2 违规，不是偶发。
- 冗余证据：同一模板上方紧邻的位置已有
  `<p class="badge badge-warning">还没有站点工程 —— 先去<a href="/admin/pages">页面管理</a>建一个工程，槽位是挂在工程下的。</p>`
  —— 已经给了正确指引与链接。空态那句**既错又重复**。
- 另：该空态**缺 `.empty-title`**（三段式不齐，`02-O` G2 同族第 N 处）。
- 既有测试：`internal/module/page/inbound/http/site_slot_facing_test.go`（面向文案相关）。
- **同页另一个空态不要混淆**：槽位卡的 `{{else if len(rows) == 0}}` 分支是**装载失败兜底**
  （`admin.site_slots.no_rows`），与「未选到工程」是两件事。它正在被并行批次改写
  （见 §0.5：补 `.empty-title` / 通用装载失败文案 / `.empty-actions`「重新加载」，
  并去掉「检查后端日志」这种把技术细节丢给运营的措辞）。**本条只针对 `pick_project` 那一段。**

### 方案

**A. 删除该分支 + 退役词条**

- 改法：删掉 `{{if … NoProjectEmpty}}` 那段 `.empty-state`；写一条退役迁移删
  `admin.site_slots.pick_project` 的两语言行（仿 `418_retire_customers_empty_i18n.sql` +
  `register_customers_empty_retire.go` 的形态：**删除必须排在原 seed 批次之后**，
  且要检查 192 那批的幂等条件计数是否把该 key 算进去 —— 若算，得同批收口，否则下一轮启动会被重灌）。
- 代价：**S**（1 模板 + 1 SQL + 1 register；若动 192 的门槛则是「S+」）。
- 影响面：`site_slot_facing_test.go`；i18n 一致性测试；`docs/02-O` site_slots 任务 3/5 可结案。
- 风险：若将来 handler 改成「不自动选首个工程」（当前是自动选），这个状态会回来 →
  在删除处留一行注释说明前提。

**B. 改成实话 + 补三段式与主行动**

- 改法：`.empty-title` 补「还没有站点工程」；`.empty-desc` 复用已有的
  `admin.site_slots.no_project.tail`（「建一个工程，槽位是挂在工程下的。」）；
  `.empty-actions` 指向 `/admin/pages`。同时**删掉上方那条重复的 badge**（两者二选一）。
- 代价：**S**（1 模板 + 1~2 条新 key 的 seed 迁移）。
- 影响面：同上 + 新 key 需中英成对。
- 风险：低。但要接受「空态与页头 badge 表达同一件事」的取舍 —— 若保留 badge，
  空态就不该再重复；若删 badge（`NoProjectEmpty` 时页头会显得空），则空态必须承担全部信息。

**C. 在 0 工程时也渲染工程选择器（补控件）**

- **不可行**：0 个工程没有可选项，渲染一个空 `<select>` 正是
  `site_slots.html` 里「没有页面」空态专门批判过的形态（「一个空下拉只会让你点一个必然失败的提交」）。
  这条不是「代价高」，是**控件本身不可满足**。

### 结论

**A**（删分支 + 退役词条）。若希望保留一个空态块，则 **B** 且必须同批删掉重复的页头 badge。
两者的成本同档；A 更符合「文案提到的控件必须真的存在」这条判据。

---

## 6. 「待重建影响面」的信息架构归位

### 现状（回代码 —— 与此前逐条核对的记载已不一致）

同一事实（哪些页面处于「有更新未发布」）当前在 **3 个页面以 3 种形态**出现：

| 页面 | 形态 | 取数 |
|---|---|---|
| `/admin/blocks`（`internal/templates/admin/block/blocks.html`） | 页头 `.page-sub` 一行 **警告徽章**（`{{ .StaleImpact.Total }} 个页面有更新未发布`）+ `<details class="section-fold card">` **折叠清单**（默认收起）；无数据时说「当前没有待重建的页面」；读不到时说 Hint 原文 | `internal/module/block/inbound/http/block_page_impact.go` 的 `blockStaleImpact`（遍历**全部工程**，逐工程 `pages.List` 后取 `Stale`，`blockStalePageLimit = 30`） |
| `/admin/articles`（`internal/templates/admin/content/articles.html`） | **`<section class="card">` 常驻区块（未折叠）** + 计数 + 清单 | `internal/module/content/inbound/http/article_stale_impact.go` 的 `articleStaleImpact`（同形，`staleImpactPageLimit = 30`） |
| `/admin/pages`（`internal/templates/admin/page/pages.html`） | **只有行级徽章**（`{{if p.Stale}}` → 「有更新未发布」），**没有聚合区块** | `pages_handle.go` 注入 `Stale: p.Stale` |

**关键事实**：`docs/02-L` P1-10 的问题是「`blocks.html` 的只读影响面卡占列表主位」，
可选修法是「搬 `/admin/pages` **或**降级为 `.help` + 徽章」。
**降级这一路已经落地了** —— `blocks_stale_impact_render_test.go` 里就有一个用例叫
「有 stale：页头徽章 + 折叠清单，**且不再有常驻只读卡**」。
所以此前的逐条核对把它列在「信息架构 / 归位（6 条）」里，记的其实是**未采用的那个修法**。

**同时**：`page/service/page_stale_overview.go` 的 `ListStalePages`（全站清单、`staleOverviewLimit = 8`、
跨工程合并、全局排序、截断、失败不降级）**零调用方**，而它的文件头注释点名消费者是
「**/admin/pages 的待重建区块**」。也就是说：**归位目标的能力已经写好了，只是没接线**。

**作用域张力**（搬之前必须定的语义）：`/admin/pages` 是**单工程聚焦**（`pages_handle.go` 的
`focusProjectID(projects, c.Query("project"))`），而 `ListStalePages` 支持
「`ProjectID` 为空 = 全部工程」。块与文章是**跨工程共享**资产（`blockStaleImpact` 遍历全部工程），
页面列表却是单工程视图 —— 「全站待重建」与「本工程待重建」是两个不同的数。

### 方案

**A. 搬到 `/admin/pages`（此前核对记录的方向）**

- 改法：`pages_handle.go` 注入 `ListStalePages` 的结果；`pages.html` 加区块；
  `blocks.html` / `articles.html` 的清单改为指向 `/admin/pages` 的链接或直接删除；
  `block_page_impact.go` / `article_stale_impact.go` 与其 render 测试随之下线。
- 代价：**M**（page handler + pages.html + 两个模块的删改 + 3 个测试文件改动 + 新增渲染用例）。
- 影响面：`internal/module/block/inbound/http/blocks_stale_impact_render_test.go`（4 个用例）、
  `internal/module/content/inbound/http/article_stale_impact_render_test.go`、
  `internal/templates/admin_template_resolve_test.go`（模板引用落点守卫，删区块时若动 include 会波及）、
  `public/test/page/**`。
- 风险：① **丢掉就地反馈** —— `/admin/blocks` 与 `/admin/articles` 上的徽章是「我刚改完这个块/文章，
  影响了哪些页」的**动作后反馈**，删掉后用户要跳到另一个页面才知道；② 作用域语义要定（见上）；
  ③ `StaleImpact` 是可选键、模板一律 `isset` 包裹，删改时别破坏「缺键仍整页渲染」这条既有不变量。

**B. 保持现状**

- 代价 **0**。
- 风险：① 三处形态、两处重复实现（两份约 40 行的只读统计，`block_page_impact.go` 的注释解释了
  「为什么不抽公共包」）；② `ListStalePages` 长期零调用 → 结构上像死代码，
  将来做死代码清理时会被误删；③ 运营不知道「全站在哪看」。

**C. 中间态：`ListStalePages` 归位 `/admin/pages` 作为**全站**归口，就地徽章保留**

- 改法：只在 `/admin/pages` 增加一个区块（接 `ListStalePages`，`ProjectID` 传空拿全站，
  标题写明「全站」以区别于页面的单工程视图）；`blocks.html` 的折叠清单尾部加一行
  「看全部 → /admin/pages」链接；`articles.html` 的常驻卡**折叠为 `<details>`**（或同样加链接）；
  **不动**两个模块的取数实现。
- 代价：**M 的一半**（page handler 接线 + pages.html 区块 + 1~2 处链接 + 新增渲染测试）。
- 影响面：`pages_handle.go` / `pages.html` / 新增 page 渲染测试；`blocks.html` / `articles.html` 各一处小改
  （不动 `*_stale_impact_render_test.go` 的既有断言，除非把 articles 的卡片折叠）。
- 风险：低。唯一要定的是「区块标题要不要写『全站』」—— 不写会与页面列表的工程作用域打起来。

### 结论

**C。** 理由：`ListStalePages` 已经为 `/admin/pages` 写好却无人调用，接线成本低；
而 `/admin/blocks`、`/admin/articles` 的徽章是**就地动作反馈**，与「全站影响面总览」是两件事，
不该互相取代。走 A（彻底搬）需要额外付「丢掉就地反馈」的代价，收益不明显。

---

## 7. 哪些其实不需要决策（事实已定 / 动作已完成）

| 条目 | 已定的事实 | 是否还需动作 |
|---|---|---|
| 2. orders 主行动 | `.FilterActive` 守卫 + 「重置」已实现并被测试钉住 | **不需要**（除非要统一全站「重置 / 清掉筛选」的用词） |
| 3①. customers 主行动 | 同上，`href` 已不再等于当前 URL | **不需要** |
| 3②. 「后台不能直接新建」 | 417 已把库值改回该句（库值实测确认） | **不需要** |
| 4. `StatusActionLabel` | 取值只有「停用」（当前正常）/「启用」（当前已停用）两种；待激活不给按钮；动词已 key 化，中英各自成句 | **不需要** —— 「二选一」的答案就是「按状态各用一种」 |
| 1. 订单建单 | `/api/order/create` 端点在、权限点在、admin 语义在；缺的只是后台页面 UI | **文案侧已定**（保持删除）；「要不要补 UI」是真决策，但优先级取决于产品是否需要代客建单 |
| 5. site_slots 空态 | 该分支的文案指向的控件在唯一触发场景下不可能存在 | **不需要**（性质已定）；选 A 还是 B 是风格选择 |
| 6. 影响面归位 | `ListStalePages` 已为 `/admin/pages` 写好、零调用；blocks 已按「降级」修法落地 | **需要**（选 A / B / C 之一） |

**真正需要决策的只有两条**：**第 5 条的 A/B** 与 **第 6 条的 A/B/C**。
其余是「已完成」或「事实已定，只是要不要补一句话的债」。

---

## 8. 一句话结论表

| # | 条目 | 我的推荐 | 为什么 |
|---|---|---|---|
| 1 | orders 空态「后台代客建单」 | **保持「删文案」（已完成）**；要代客建单另立需求 | 后端有 `POST /api/order/create` 与 admin 语义、权限点也已存在，但补 UI 是 L 级（缺商品/变体选择控件），且必须先定 admin 入口下的「自动开号 + 发密码邮件 + 免支付」语义 —— 那才是真决策点，不是一个词条 |
| 2 | orders「有工程无数据」主行动 | **保持现状（条件性「重置」）** | 与筛选栏共用一个 key = 同一动作同一词，用户不会怀疑两者等价；换文案要付迁移 + 改测试，收益只是一个词 |
| 3 | customers 空态两问题 | **已完成；补 `empty_filtered_heading/_desc` 两个 key 的 seed** | 「保留哪句」是假二选一（两档各管一个场景，都要留）；真缺口是筛选态那两个 key 在库里不存在 → 英文界面回落中文 |
| 4 | `{{StatusActionLabel}}这个账号` | **保持现状；本条结案** | 只有「停用 / 启用」两种取值，由当前状态决定，与提交的 `toStatus` 一致；动词已 key 化，「Disable这个账号」的中英混排已消除 |
| 5 | site_slots 空态指向不存在的控件 | **删分支 + 退役词条（A）** | 页头选择器在 `Projects > 1` 才渲染，而该文案只在 `Projects == 0` 触发 —— 互斥，控件永不存在；上方还有一条重复的 badge 已经给了正确指引 |
| 6 | 「待重建影响面」归位 | **C：接 `ListStalePages` 到 `/admin/pages` 作全站归口，就地徽章保留，articles 的常驻卡折叠** | 能力已备却零调用，接线成本低；就地徽章是「刚改完这块/文章影响哪些页」的动作反馈，与「全站总览」不是同一件事，不该互相取代 |

---

## 9. 清单外发现（不在 6 条范围内，但值得单独立项）

1. **`admin.customers.list.empty_filtered_heading` / `.empty_filtered_desc` 无 seed**
   —— 模板在用（`customers.html` 的筛选态分支），`sys_i18n` 与全仓 `.sql` / `.go` 都没有它们。
   英文界面回落中文。**没有任何门禁覆盖该页**：`internal/templates/admin_group_f_i18n_test.go` 的
   双向对账只覆盖它自己那批（seed 文件是 `193_i18n_seed_admin_system.sql`、模板范围是 group F 的子集），
   customers 页不在其中 → 缺口可以长期存在。
2. **`page.Service.ListStalePages` 零调用方**（见 §6）。能力完备、注释点名了归位目标却没人接线 ——
   既造成三处重复实现，也让它在将来做死代码清理时容易被误删。
3. **三处注释的因果表述不准**：`docs/02-O` orders 任务 1、`417` 迁移头注释、`orders.html` 模板注释
   都写「没有任何建单端点」，准确说法是「`/admin/orders` **页面路由**无建单入口；
   `POST /api/order/create` 端点在」。同一批测试的判据 ① 也**只**扫 `/admin/orders` 前缀 ——
   判据本身正确，是文字表述把范围说大了。
4. **`config.yaml` 已切到业务角色**：`database.user: go_wp_app`、`require_rls_role: true`、
   `run_migrations: false`。这与 `AGENTS.md` 里「应用连接用的是超级用户 `root`，所以 RLS 策略目前
   一行都挡不住」的记载不符（未启动服务验证运行时探针结论，仅凭配置判断）。

---

## 10. 未做的事与原因

- **未改任何代码**（含模板、迁移、测试），符合任务约束；
- **未 `git add` / `git commit`**；
- **未启动服务实测页面**：本轮的「现状确认」走的是「读代码 + 直查 `sys_i18n` 库值 + 路由快照 +
  既有渲染级测试的断言」四条路径；凡是需要浏览器实测的读数（屏数、`href == 当前 URL` 的实测、
  空态渲染 DOM）都引用了 `02-O` 原始报告与既有测试，并已按「语义定位」重新核对过代码，
  **未把它们当成未验证的事实使用**；
- **未跑测试套件**（`go test`），因此「测试会动」是按断言内容推断的影响面，不是实测结果；
- **未把并行批次在途改动的文件当作最终现状**：对第 1 / 3 / 5 条的现状裁定以 `HEAD` 已提交内容
  为准，并已逐文件 `git diff` 核对并行批次改的是哪一段（§0.5）。决策若落在这些文件上，
  **先确认并行批次已收尾**。
