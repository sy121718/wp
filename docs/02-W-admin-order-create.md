# 02-W · 后台代客建单 UI（立项设计）

> 内部过程文档（非对外使用）

> **性质**：只调研与设计，**未改任何代码 / 模板 / 迁移**。
> **定位方式**：全文按**语义**定位（函数名 / 常量名 / 类名 / i18n key / 文案），不给行号 ——
> `docs/02-{L,M,O}` 的行号已全部失效（AGENTS.md 明示），本文件不复制这个坑。
> **基线**：工作区 HEAD + 库值实测；迁移号已到 **431**。
> **前置文档**：`docs/02-V-decision-brief.md` §1（要 A 删文案还是 B 补入口）、§2（空态主行动）、
> `docs/02-O-trade-site-audit.md` §1 orders T1/T2、`docs/02-T-write-fail-echo-batch1.md`（分档契约）。

---

## 0. 一句话结论 + 推荐路线

**端点齐、语义齐、只缺 UI**：`POST /api/order/create` 与 `order:create` 都已存在且已按「后台代客」语义落库
（`CreatedVia=admin` / `CreateBy` / `operator_type=admin` / `Attribution` 允许 nil），本项是**纯 UI 接线**，
service 只需为「开号策略」加一个显式开关。

推荐路线（四条一起定，缺一条会返工）：

| # | 决策 | 推荐 |
|---|---|---|
| 1 | 入口形态 | **独立整页 `/admin/orders/new`**，页头主行动 + 空态主行动**双入口**（同 URL） |
| 2 | 商品明细 | **服务端渲染的变体候选行**（复用 `ListBundleSKUs` 的能力）起步；「按 SKU 搜 + 可用量」是后继增量 |
| 3 | 开号语义 | **admin 入口默认不开号、不发初始密码邮件**；开号是操作人**显式勾选**的 opt-in |
| 4 | 权限 | **复用 `order:create`**，页面写动作走 `CasbinMiddlewareForPath("/api/order/create")`；**不新增权限点、不写 seed 迁移** |

一句话风险：**不先定第 3 条就做 UI，等于把「运营误填邮箱 → 系统给陌生人发初始密码」直接放上台面。**

---

## 1. 入口与信息架构：入口放哪

### 现状（回代码）

| 事实 | 语义位置 |
|---|---|
| 订单页只有 `GET /admin/orders` + 写动作 `status / cancel / refund / note / bulk-status / bulk-cancel` | `order_router.go` 的 `if pages != nil` 段 |
| 页头 `.page-actions` 内只有工程下拉，且**仅多工程时**渲染 | `orders.html` 的 `<header class="page-head">` |
| 「有工程无订单」空态**只在带筛选时**给 `.empty-actions`（「重置」），无筛选时**不给** | `orders.html` 的空态分支 + handler 的 `FilterActive` |
| 空态文案里「（或后台代客建单）」**已删**，库值也已更新 | `admin.orders.list.empty_tail`（迁移 417 改过库值） |
| 同模块**已有**「页头 + 空态双入口」的正确样本 | `coupons.html`：页头 `.page-actions` 与空态 `.empty-actions` 各放一次同一个「＋ 新建优惠码」（`data-drawer-open="#tpl-coupon-create"`，`canCreate = isset(.PermSet["order:coupon_create"])`） |
| 后台已有「字段多 → 整页」的先例与结论 | `/admin/products/new`（`product_page_router.go`）+ `docs/02-T`：商品新建**从抽屉改成整页**，理由是 13 个字段 + 失败回填 |
| 列表页「新建」入口的抽屉片段 | `admin/partials/toolbar_create.html`（`allowed` / `tpl` / `title` / `label`） |

### 方案

| 方案 | 做法 | 代价 | 判断 |
|---|---|---|---|
| A 抽屉（照 coupons） | `data-drawer-open` + `<template>` + 表单在抽屉里 | S | ✗ 字段 20+、明细可多行，抽屉放不下；抽屉关闭态下 `302+?err=` 会**整屏丢输入**（coupons 现状即如此） |
| **B 独立整页 `/admin/orders/new`** | 页头 `.page-actions` 放主行动（`{{if canCreate}}`），空态 `.empty-actions` 放同一个入口；表单同页 POST | M | ✓ **推荐** |
| C 只挂在空态 | 有单之后页头没有入口 | S | ✗ 代客建单是**常规操作**，不是「首次才有」的操作；入口随数据消失 |

**推荐 B，四条理由**：

1. `admin-ui-logic` §2.2「主行动与筛选同行」是硬规则：页头 `.page-head .page-actions` 是唯一放页级动作的地方
   —— 单独一张「新建订单」卡是纯浪费（本项目已把它列为违规形态）。
2. 字段量与 `docs/02-T` 批 1 的判定同档（商品 13 字段已判整页，本项是「13 + 多行明细」）。
3. **与「空态不给死按钮」判据的关系**：该判据（`docs/02-O` orders T2 + `docs/02-V` §2 已复核）针对的是
   **href == 当前 URL 的链接**——点了 URL 与页面逐字不变。`/admin/orders/new` 是另一个 URL，
   是**活按钮**，不违反判据。真正要处理的是**测试**（见下）。
4. 双入口同 URL 是 `coupons.html` 的既有形态，用户不会怀疑两个按钮是否等价。

### 影响面

- `internal/module/order/inbound/http/`：新增页面 handler（GET 表单页 + POST 提交），或并入 `order_page_handle.go`。
- `internal/templates/admin/order/order_new.html`（新增）+ `orders.html`（两处入口）。
- `internal/routers/testdata/routes.snapshot`（重生成）。
- `public/test/order/feature/trade_empty_state_honesty_test.go`（两条用例的判据要动，**不可省**）。

### 风险

- **`TestOrdersEmptyStateActionsFollowFilter` 会变红**（它钉「无筛选时空态 0 个 `.empty-actions`」）。
  这不是「实现回退」，是判据过时（§6 给出改法）。
- **`TestOrdersEmptyCopyMatchesRoutes` 判据 ① 会变红**（它扫 `routes.snapshot` 里 `/admin/orders` + `/new` 等后缀）。
  必须**反转**成「存在建单入口时，页面必须真的给出入口」——比原来那条更强（原来的判据只防「文案吹牛」）。
- 侧栏高亮：`nav.go` 的 `navPathAlias` 是「子页面 → 菜单归属」表，`/admin/orders/new` 不在表里 →
  打开新页时侧栏整组**失去高亮**。需加一条映射（**存量同病**：`/admin/products/new` 也没有映射，
  见 §8 清单外发现）。

---

## 2. 表单字段：`CreateOrderReq` 全字段与必填性

### 现状（回代码，dto/order_req.go + service/order.go 的 createOrder / buildOrderDraft）

必填只有三处形状校验（`validateCreateOrderReq`，不碰数据库）：

| 字段 | 必填 | 后台代客场景下必须由操作人选 | 说明 |
|---|---|---|---|
| `projectId` | ✅ | ✅ | 空 → `order.err.projectRequired` |
| `customerEmail` | ✅ | ✅ | 空 / 不含 `@` / 长度 <3 → `customerEmailRequired` / `customerEmailInvalid` |
| `items[]` | ✅（非空、≤100 项） | ✅ | 空 → `itemsRequired`；超限 → `itemLimitExceeded` |
| `items[].variantId` | ✅ | ✅ | 空 / 不属于该工程 / 未启用 → `order.err.variantNotFound`（跨工程是越权，不是查不到） |
| `items[].quantity` | ✅ | ✅ | `1..100000`（`maxItemQuantity`）→ 否则 `quantityInvalid` |
| `customerName` / `customerPhone` | ✗ | ✅（业务需要） | 空则落库为空串；列表「客户」列会只剩邮箱 |
| `shipping`（name/phone/province/city/district/address/zip，7 项） | ✗ | ✅（业务需要） | 服务端不校验；缺收货人电话的物理单**发不出去** |
| `billing`（同 7 项） | ✗ | 建议「同收货地址」开关 | 与 shipping 共用同一形状 |
| `paymentMethod` / `paymentMethodTitle` | ✗ | ✅（否则列表该列为空） | 前台由通道自报（`s.pay.Method()`，当前只有 `paypal` / 「PayPal（模拟）」）；后台只能手填或选 |
| `shippingTotal` | ✗ | ✅ | 由调用方给（运费策略不在商品域）；负数被归 0 |
| `discountTotal` | ✗ | **禁止**输入 | 无券时**一律忽略**（SEC-001）；有券以服务端试算为准 —— 后台也不开放人工折扣 |
| `couponCode` | ✗ | 可选 | 给了就以服务端试算为准；核销与建单同事务 |
| `remark` | ✗ | 可选 | 客户备注 |
| `adminNote` | ✗ | ✅（后台场景的填写位） | 注释明写「自建订单（createdVia=admin）与代发订单的填写位置」 |
| `requestId` | ✗ | ✅（建议自动生成） | 幂等键：同键重复提交只落一单。**不填则手抖双击会出两单** |
| `createdVia` | ✗ | 由 handler 覆盖 | handler 见操作人即置 `"admin"`；**注意它是客户端可传字段** |
| `locale` | ✗ | 可选 | 只影响「初始密码邮件用哪套模板」 |
| `attribution` | ✗ | —— | 允许 nil：注释明写「后台代客下单没有访客上下文」，落 `{}`（列 NOT NULL） |
| `userId` / `ipAddress` / `userAgent` / `createBy` | —— | 服务端覆盖 | 全部 `json:"-"`，客户端传了不生效（`createBy` 由 `operatorFromContext` 落） |

服务端算出来的（页面**只读展示**，绝不接受输入）：`subtotal` / `total`、订单号（`GWP+日期+随机`）、
状态（恒落 `pending`）、商品名/规格/SKU/单价/成本快照。

### 方案：表单分组（按「谁来决定这个值」分组，不按数据库列顺序）

1. **归属**：工程（页级上下文，`len(Projects)>1` 才渲染下拉）+ 幂等键（隐藏域，由服务端生成一次性值）
2. **客户**：邮箱（必填）· 姓名 · 电话
3. **商品明细**（多行，唯一需要选择器的部分）：变体 + 数量；行内可「再加一行 / 删除」
4. **收货地址**：7 字段 + 「同账单地址」开关（勾上则 billing 不渲染、由服务端复制 shipping）
5. **金额**：运费（**分**，与订单页金额口径一致）；优惠码（可填，服务端试算）；折扣**不给输入框**
6. **支付与备注**：支付方式（码 + 展示名，建议给 `paypal` 等候选 + 允许手填）· 客户备注 · **后台备注**（代客建单的语境说明位）
7. **开号策略**（见 §4）：勾选框，默认**不勾**

### 影响面 / 风险

- 影响面：1 个新模板 + 1 个页面 handler（含 **form → dto 组装**：`CreateOrderReq` 是 JSON 绑定，
  页面表单是 form-urlencoded，需要自己拼多行 `items`；先例是 `couponSaveReqFromForm(c)` 与
  `variantSelectionFromForm(c)` 的并行数组解析）。
- 风险：**金额单位**。订单域一律**整数分**（`order_page_handle.go` 注释：分 → 元的换算只在
  `orderAmountText` 里发生一次），商品域是元。表单里「运费」必须标明单位是**分**，否则运营会填 15（想表达 15 元）。
- 风险：**建单即扣库存 + 落 pending**。后台代客建单没有支付动作，会停在「待付款」并**立即占用库存**；
  「免支付直接成单」是产品决策（当前必须先建单、再走 `/api/order/status` 流转）。
- 风险：无金额预览。前台有结算链路，后台目前**没有**建单前的试算端点 → 提交前看不到小计/合计/券后价。
  可选增量：复用试算能力做一个只读预览端点（代价 M），**不建议**放首期。

---

## 3. 商品 / 变体选择器：仓库里已有的可复用件

### 现状（回代码）

**没有**「搜商品 / 搜变体」的选择器控件。`internal/templates/static/js/ui/` 现有控件：
`busy / colorfield / confirm / drawer / htmx / iconfield / mediafield / modal / select / themetoggle / toast`。

已有的三块**可复用件**：

| 类别 | 现成件 | 语义位置 | 能做什么 / 不能做什么 |
|---|---|---|---|
| 控件模式 | `ui/mediafield.js` | 文件头注释即用法契约 | 「**input 是唯一真值来源**」+ 按需创建弹层 + 直接打只读 GET API（`/api/media/list`）+ 挂 `WBUI.controls` —— 这正是「选择器」该长的样子 |
| 控件模式 | `ui/drawer.js` | `openDrawer` | `<template>` 内容克隆后**自己补一次 `htmx.process` + `WBUI.scan`**（否则抽屉里所有 `hx-*` 是死的）；广播 `wbui:drawer-open` |
| 页面先例 | `admin/product/product_bundle.html` | 成员表 + 页内 `<script>` | 服务端渲染 `<select name="variantId">` 每行一份候选（跨商品、只列启用变体），配「可选 SKU 共 N 个」徽章；行追加走 `fetch POST /admin/products/bundle/members/resolve` 拿 JSON 行、带 `existingVariantId` 去重、逐条 `skips` 原因 |
| 后台片段先例 | `admin/product/product_tag_hits.html` + `pages.GET("/product-tags/hits")` | `#tag-hits-panel` | `hx-get` 换来换去的**后台页面片段**（含自身翻页）—— 是「片段端点」的正规先例 |
| 访问面片段先例 | `/_fragments/bundleConfigurator`（`runtimefragment`） | 被 `product_bundle.html` 以 `hx-get` 复用 | 那是**公开面**（无 Casbin / 无 Session），语义是「前台配置器预览」，**不该**被后台选择器当成数据源依赖 |

**关键结论：有现成端点，但它不是「变体检索」。**

`GET /api/product/bundle/skus?projectId=&keyword=`
（`permission.ProductBundleSkus` = `product:bundle_skus`「捆绑可选 SKU」，`ProductService.ListBundleSKUs`）：

- 返回 `{variantId, skuCode, productId, productName, price, costPrice, enabled}` 的**全量数组**（无分页）；
- `keyword` 走 `Model.List(productID, keyword, …)` 的 `name ILIKE` → **匹配的是商品名，不是变体 SKU
  也不是规格**；命中商品后取它们**全部启用变体**；商品侧上限 `maxPageSize = 200`；
- **没有可用量字段**（可用量在 `BundleOptionDetail.Available`，那是另一条读模型）。

其他相关端点：

- `GET /api/product/list`（`product:list`）：商品级搜索，`keyword` 同样只匹配 `name ILIKE`，有分页；
- `GET /api/inventory/stock/sku`（`inventory:stock_sku`）：**按 SKU 查库存**——可用作「手填 SKU 反查」的地基；
- 服务端渲染路径可直接用收窄只读端口 `VariantAvailabilities(ctx, projectID, variantIDs)`
  （`product/contract/variant_stock.go`，由 product 实现）批量取可用量。

### 方案（三选一，含代价）

| 方案 | 做法 | 代价 | 优点 | 代价/缺陷 |
|---|---|---|---|---|
| **1 手填变体 id / SKU** | `<input name="variantId">` + 数量；服务端校验（`ErrVariantNotFound` 可读） | **S** | 一天能出货 | 运营拿不到变体 id（后台商品编辑页不暴露 id，只在 `/api/product/get` 里）→ 实际可用性差，**只配当内部切片，不作为对外交付** |
| **2 服务端渲染候选行**（推荐起步） | 照 `product_bundle.html`：每行 `<select name="variantId">`，候选由 handler 用 `ListBundleSKUs` 铺；「关键词 → GET 重渲染本页」做过滤 | **M** | 零自定义 JS；先例逐字可抄；候选人眼可搜（浏览器原生 select 打字跳转） | 候选上限 = 200 商品的全部启用变体；**按 SKU / 规格搜不到**；无可用量列；每次改关键词整页重渲染 |
| **3 JS 选择器控件** | 新增 `ui/variantfield.js`（照 `mediafield.js`：input 唯一真值 + 弹层 + 只读 GET）；候选行含 SKU / 商品 / 规格 / 价格 / 可用量；已选行由 JS 追加，形态抄 `product_bundle` 的 fetch + appendMembers | **L** | 唯一能做「按 SKU 搜 + 分页 + 可用量」的形态；与明细多行天然契合 | 需要**一个新端点**（见下）+ 1 个权限点 + WBUI 注册 + 「片段换入后重扫」；新 JS 越出「只做 HTMX 覆盖不到的控件层」的收敛边界要交代清楚 |

**若做方案 3，端点建议**：新增收窄只读 `GET /api/product/variant/search`（`projectId` + `keyword` 匹配
`product_variants.sku_code` / 商品名 / 变体规格 + `limit`），权限点复用 `product:list`
（**理由**：有建单权 ≠ 有商品检索权，但为选品再让管理员配一个 `product:bundle_skus` 是**配权负担**；
`product:list` 语义上就是「读商品目录」）。代价：1 条路由声明 + 1 权限点常量（`permission.SyncToDB`
幂等 upsert，**不写 seed 迁移**）+ service 1 个方法。

### 影响面 / 风险

- 影响面：新模板的行结构（并行数组，字段名 `variantId` / `quantity` 与 `OrderItemReq` 对齐）+
  若要方案 3 则加上 product 模块的路由/service 与新 JS。
- 风险：**不要用 `/_fragments/*` 做后台选择器的数据源**——那是公开面（访客可达、无鉴权），
  后台功能依赖它会形成「无权限也能读」的旁路，且两类接口的变更节奏不同。
- 风险：方案 2 的候选规模在 SKU 多时会变成「几千个 option 的下拉」——`product_bundle` 已有此形态，
  但要接受「表单首屏体积随 SKU 数线性增长」。
- 风险：**可用量不是必填信息**。缺货会在提交时由库存守卫整体拒绝（`ErrStockInsufficient`，同事务回滚），
  属于「可读的失败」，不是静默错误。

---

## 4. `ensureGuestAccount` 语义（本项最关键的决策点）

### 现状（回代码，逐条精确）

```text
buildOrderDraft（service/order.go）
  └── 无条件调用 s.ensureGuestAccount(ctx, req)           ← 不看 CreatedVia

ensureGuestAccount
  └── if req.UserID != nil || s.guest == nil { return req.UserID, false }   ← 唯一的两个短路
  └── 否则调 usercontract.GuestAccountProvisioner.EnsureGuestAccount

user.Service.EnsureGuestAccount（user_guest_account.go）
  ├── 邮箱已有账号 → 只返回既有账号（Created=false，PasswordMailed=false，不发信、绝不改密码）
  └── 邮箱无账号   → 建号（12 位随机密码、status=active、用户名由邮箱前缀派生去重）
                     + sendGuestAccountMail（模板 key = "guest_account"，locale 决定模板语言）
                     → Created=true，PasswordMailed = 发信是否受理成功
                     发信失败**不回滚建号**（客户可走「忘记密码」）
```

**结论（复核任务书的关键点，全部成立）**：

1. 后台建单路径**会**给访客开号并发初始密码邮件 —— 而且是**无条件**的：
   `req.UserID` 是 `json:"-"`，后台页面表单**根本传不进来** → 后台代客建单**必然**走开号分支
   （只要装配期注入了 `GuestAccountProvisioner`，而生产装配必然注入）。
2. **幂等性不会「反复发信」**：第二次起该邮箱已有账号 → `Created=false`、`PasswordMailed=false` →
   不发信、不改密码。并发同邮箱由唯一索引兜底（`Create` 撞键后重查既有账号，`Created=false`）。
   → 「重复下单反复发信」这个担心**不成立**；真正成立的是「**第一次就发了**」。
3. `CreateOrderResp.AccountMailed` 是页面**唯一**能拿来告知「已给客户开号并发密码」的信号
   （注释：仅「新建且初始密码寄出去了」为 true）。

### 影响

| 维度 | 具体后果 |
|---|---|
| 合规 / 隐私 | 运营用客户邮箱下单 = **单方面以客户身份开户**，并向**第三方邮箱**发送登录凭据。代客场景（电话单、线下单、老客户）里客户往往没在本站注册、也不一定想注册 |
| 误发 | 邮箱填错一位 → 凭据发给陌生人；该账号还绑着这一单的订单归属（`user_id`） |
| 语义错配 | 「订单联系邮箱」被当成「账号身份」——前台这么做有理由（客户在下单页接受「下单即开户」），后台没有这句同意 |
| 幂等 / 信噪 | 不重复发信（见上）；不会重复建号 |
| 反向代价（**必须一起承认**） | 若跳过开号 → `user_id` 为空 → **客户在账户中心看不到这单**：访客订单查询走 `user_id`（`GetByIDForUser` / 按 user 过滤），且全仓**没有**「注册/登录后按邮箱认领历史订单」的逻辑（user 与 order 两侧都查过，没有）。缺这条后继能力时，客服只能人工报状态 |

### 可选策略

| 策略 | 做法 | 代价 | 评价 |
|---|---|---|---|
| **A 维持现状** | 无条件开号 + 发信 | **0** | ✗ 等于接受上面整张影响表；`docs/02-V` §1 已把这条列为「补 UI 前必须先定」的产品语义 |
| **B admin 入口默认跳过开号** | 只存匿名联系方式（`user_id` 空、订单照常落库） | **S**（service 一个判定 + 模板一句提示 + 1 条测试） | ✓ 安全默认；代价是「账户中心不可见」（需后继需求） |
| **C 默认跳过 + 显式 opt-in 开号**（推荐） | B 的基础上，表单给一个**不勾选**的复选框：「同时给客户开号并把初始密码发到这个邮箱」 | **S~M**（多一个字段 + 回执提示 + 3 条测试） | ✓✓ 默认安全、需要时可用；把「开号发信」变成操作人的**显式动作**，与「误发」的责任链一致 |
| **D 必须绑已有账号** | 邮箱必须已注册，否则拒绝建单 | **L** | ✗ 新客（电话单的主要对象）根本无法代客下单 → 场景不成立；且 `GuestAccountProvisioner` **只有** `EnsureGuestAccount` 一个方法，没有「只读查账号是否存在」的能力，要新增 user contract 方法 |

### 明确推荐：**C**

实现要点（供实施阶段照做，**不在本设计内动代码**）：

1. **开关放显式字段，不要用 `CreatedVia` 隐式判定**：`CreatedVia` 是**客户端可传**的
   （`json:"createdVia"`，handler 只在为空时补 `"admin"`）→ 拿它当「是否开号」的开关，
   等于把这个安全语义交给调用方选。新增显式字段（如 `guestAccount` 三态 `none|provision`，
   或布尔 `provisionGuestAccount`），**默认 `none`**。
2. **默认值的落点**：`CreateOrderReq.CreatedVia == "admin"` 时默认 `none`（不显式传也不开号），
   显式 `provision` 才走开号 → 前台不受影响（checkout 留空 → 兜底 `checkout`，`UserID` 可能为 nil，仍开号）。
3. **表单**：复选框 + `.help` 悬浮说明（「会给该邮箱创建账号并发送初始密码；邮箱已有账号则只关联、不发密码」），
   成功后回执按 `AccountMailed` 分支提示（true → 「初始密码已发往 …」；false 且勾了 → **不谎报**，
   因为「已有账号只关联」时并没有发信 —— 这正是 `AccountMailed` 语义的设计初衷）。
4. **既有测试不受影响**：`TestOrderCreateProvisionsGuestAccount` 与
   `TestOrderCreateNeverResetsExistingAccountPassword` 用的 `createBaseReq` **不带 `CreatedVia`**（走 checkout 兜底）
   → 按 admin 分支判定不会让它们变红；但必须**新增**一条「admin 默认不发信 / opt-in 才发信」的用例。

### 影响面 / 风险

- 影响面：`dto/order_req.go`（1 字段）、`service/order.go`（`buildOrderDraft` 出入口判定）、
  新页面模板、`public/test/order/feature/order_guest_account_test.go`（新增用例，不改既有两条）。
- 风险：**`user_id` 为空的订单在账户中心不可见**（B/C 的共同代价）。建议同批在 `docs/10-todo.md`
  记一条后继需求「客户登录后按邮箱认领历史订单」，并在客服可见处（订单详情）显示「客户账号：未关联」。
- 风险：如果默认改成不开号而**没有人**在 UI 上勾选，实际上会让「访客自己下单」与「后台代客下单」的账号行为分叉
  —— 这是**有意为之**（后台没有「下单即开户」的同意），但要在 `.help` 里写清楚，否则运营会以为是 bug。

---

## 5. 权限与导航

### 现状（回代码 + AGENTS.md 挂载矩阵）

| 事实 | 语义位置 |
|---|---|
| `order:create` = 「新建订单」，**已存在并已声明** | `internal/permission/codes.go`（常量 + 中文名表）+ `order_router.go` 的 `g.POST("/create", permission.OrderCreate, h.CreateOrder)` |
| `/admin/*` 页面路由**不挂 Casbin**；写动作由页面 handler 用 `builtin.CasbinMiddlewareForPath("<API 路径>")` 复用 API 权限点 | `order_router.go` 的 `status / cancel / refund / note / bulk-*` 六处，注释明写「权限点路径一个字符都不能改」 |
| 模板做按钮级可见性 | `shell.PermSetKey`（`PermContextMiddleware` 注入）→ `{{canCreate := isset(.PermSet["order:create"])}}`（`coupons.html` 同形） |
| 菜单真源是表 | `nav.go` 注释：「增删改菜单：改表（写迁移，或在「菜单管理」页编辑），不改代码」 |
| 子页面高亮靠映射 | `navPathAlias`（`nav.go`）—— 菜单项高亮是 `MatchKey(菜单路径) == currentPath` **精确匹配** |
| 权限缺口门禁 | `scripts/check-permission-gaps.sh` 只比对 `^[A-Z]+ /api` 的路由 → **页面路由不在它的视野** |

### 方案

1. **页面路由**：`pages.GET("/orders/new", …)` 挂在 `pages` 组即可，**不走 Casbin**（与挂载矩阵一致）。
2. **写动作**：`pages.POST("/orders/create", builtin.CasbinMiddlewareForPath("/api/order/create"), …)`
   —— 复用 `permission.OrderCreate`，**不新增权限点、不写 seed 迁移**（与 `order:note` / `bulk-*` 同手法；
   AGENTS.md：新增权限点走「常量 + 路由注册处声明 + 启动期幂等 upsert」，seed 只在存量台账用）。
3. **按钮守卫**：页头与空态两个入口都必须 `{{if canCreate}}`；无权限的用户看不到入口，
   即使手敲 URL 提交也会被 Casbin 拒（页面要把它渲染成可读提示，不是 500）。
4. **菜单**：**不需要**新增菜单项（挂「订单管理」下，同 `products/new`、`themes/settings`、
   `datarules/edit` 等既有子页面）。**需要**在 `navPathAlias` 加一条
   `"/admin/orders/new": "/admin/orders"`（否则打开新页时侧栏失去高亮）。
5. **门禁**：`routes.snapshot` 要重生成（新页面路由 + 新 POST 都会进快照）；
   `check-permission-gaps.sh` 预期**无变化**（新路由不在 `/api` 下，且没有 `/api` 新端点）。

### 影响面 / 风险

- 影响面：`order_router.go`（2 行路由）、`nav.go` 的 `navPathAlias`、`routes.snapshot`。
- 风险：`CasbinMiddlewareForPath` 的参数是**字面量路径**，与 `order_router.go` 的 API 注册路径必须逐字一致
  —— 打错不会编译失败，只在运行时表现为「403 或越权放行」。
- 风险（**清单外发现**）：`navPathAlias` 里**没有** `/admin/products/new`、也没有
  `/admin/products/...` 的其他子页 → 商品新建页当前打开时侧栏不高亮。本项顺手补自己这条；
  商品那条建议单列一条清理（见 §8）。

---

## 6. 测试与验收

### 必须改（不改就红，且都属于「判据过时」而非实现回退）

| 用例 | 位置 | 改法 |
|---|---|---|
| `TestOrdersEmptyCopyMatchesRoutes` | `public/test/order/feature/trade_empty_state_honesty_test.go` | 判据 ①（扫 `routes.snapshot` 断言 `/admin/orders` 下无 `/new`）**反转**为：「路由表存在建单入口 ⇒ 页面必须真的有指向它的入口（`.page-actions` 或 `.empty-actions` 里有该 href），文案与路由表一致」。判据 ②③ 保留（页面与库值都不得再出现「代客建单」这种**承诺式**描述——现在的入口是真的，措辞改成「＋ 代客建单」这种**动作**即可，断言词需相应调整） |
| `TestOrdersEmptyStateActionsFollowFilter` | 同上 | 现判据「无筛选 0 个 action / 带筛选恰好 1 个」→ 改为「无筛选时 action 只能是**建单入口**（不得有指向本页的重置）/ 带筛选时 = 建单 + 重置」，并保留「重置必须带 project、不得带回 keyword」 |

### 必须新增

1. **模块内渲染测试**（照 `order_bulk_page_render_test.go` 的 `bulkPageBase` + `renderAdminTemplate`）：
   - `/admin/orders/new` 渲染完整（`</html>` 在输出里 —— Jet 缺键会 500 且丢弃半截内容，`</html>` 是最稳的判据）；
   - **字段清单正反双向断言**（模板读的每个回填字段都在 handler 的清单里，反向也钉）；
   - `PermSet` 不含 `order:create` 时**不渲染**两个入口。
2. **feature 链路**（`public/test/order/feature/`，真 PG）：
   - 后台建单成功 → `created_via = admin`、`create_by = 操作人 id`、状态 `pending`、
     库存已扣、状态流转日志的 `operator_type = admin`；
   - 明细多行 + 券核销走通（复用 `fakeMail` 与既有 fixture）；
   - **开号策略**：admin 默认**不发** `guest_account` 邮件（断言 `mail.calls` 里没有该模板）；
     显式 opt-in 时**发**且 `AccountMailed=true`；邮箱已有账号时勾了也不发（复用既有安全边界用例的判据）；
   - 权限：`order:create` 缺失 → 403（不是 500）。
3. **`routes.snapshot` 重生成**：`WP_DUMP_ROUTES=write go test ./internal/routers/ -run TestRouteSnapshotStable -count=1 -v`
   （**只看 diff 是不是你想加的那几条**；把误挂/漏挂当成基线写进去会让快照失去意义）。
4. **i18n 迁移**：新页面全部文案走 `.["t"]("key","中文兜底")`；新增词条必须**同批写迁移 + register 文件**
   （`ON CONFLICT (item_key, lang) DO NOTHING`，**版本号小的先赢**）—— 模板兜底不是真源：

   > 迁移号 **从 432 起**（429 / 430 / 431 已被占用，别撞）。

### 「写失败原地留住输入」分档是否适用 → **适用**

判据（`docs/02-T` + `admin-ui-logic` §9）：一次写失败后用户填的东西还在不在。本项是**多字段 + 可多行明细**，
输入成本高于商品新建的 13 字段单页 → 必须照分档契约：

| 请求 | 失败 | 成功 |
|---|---|---|
| htmx（`HX-Request: true`） | 200 + 片段自身（错误槽 + 回填后的表单） | `HX-Redirect`（**不能 302**） |
| 原生 | 302 + `?err=` 回 **`/admin/orders/new`**（不是列表页） | 302 到列表页（带 `?ok=`） |

四条不可省的约束（每条都有实测代价）：① 成功路径也要分档；② 回填键名带 `FormEcho*` 前缀；
③ 多值按**值**命中；④ 错误槽放 `<form>` 之外、host 之内。

**明细多行的回填是新增难点**（`02-T` 的两批都没碰过「动态行数」）：
建议**整份明细行由服务端重渲染**（handler 从 `PostFormArray` 重建行视图 → 片段渲染 N 行），
而不是逐字段 echo —— 逐字段 echo 需要按索引对齐并行数组，索引一错就静默错位（而「错位」在页面上表现为
「数量跑到了别的商品上」，不会报错）。

### 验收命令（实施阶段照跑）

```bash
go build ./...
go test ./internal/module/order/... -count=1
go test ./public/test/order/... -count=1
go test ./internal/templates/... -count=1
bash scripts/check-no-internal-error-leak.sh
bash scripts/check-empty-state-table-head.sh
# 真实渲染证据（不是「看起来对」）：响应必须含 </html>
```

**反向证据要求**：失败提交后 `location.pathname` 不变、错误槽可见、刚填的每个字段都还在（含明细行数与数量）、
再补一次成功提交确认整页正常跳转（`HX-Redirect` 生效而不是把整页 HTML 塞进片段位置）。

---

## 7. 分阶段实施清单

每阶段一个**可验证交付物**，前一阶段的产物不因后一阶段返工。

### 阶段 1 · 安全默认 + 能建出一单（M）

交付物：`/admin/orders/new` 整页，能从后台建出一单，且**默认不给客户开号、不发邮件**。

- 页面：新模板 + GET/POST handler（form → `CreateOrderReq` 组装，明细支持 1..N 行）；
- 明细录入：服务端渲染候选行（§3 方案 2，复用 `ListBundleSKUs` 能力 + 关键词重渲染）；
- 入口：页头主行动 + 空态主行动，均 `{{if canCreate}}`；空态文案从「承诺式」改为「动作式」；
- 开号：显式开关落地，**admin 默认 `none`**（§4 推荐 C 的第 1~2 点）；
- 路由 + `navPathAlias` + `routes.snapshot` + 分档失败回填 + i18n 迁移（432+）；
- 验收：§6 的必改用例已改、新增渲染用例与 feature 用例绿、两条门禁绿、
  `curl /admin/orders/new` 响应含 `</html>`。

**为何把开号默认塞进阶段 1**：不加它，阶段 1 一出货就是「无条件给客户发初始密码」——
这正是 §4 要避免的风险；而它本身只值一个字段 + 一处判定（代价 S）。

### 阶段 2 · 开号 opt-in UI（S）

交付物：表单上的「同时给客户开号并把初始密码发到该邮箱」复选框 + 帮助文案 + 按 `AccountMailed` 分支的回执。

- 新增「勾了才发信 / 已有账号不发信」的用例（§6 第 2 条第 3 项）；
- 订单详情展示「客户账号：已关联 / 未关联」，让「账户中心看不到」这件事**在数据上可见**。

### 阶段 3 · 选择器升级（L，按需）

交付物：`ui/variantfield.js`（照 `mediafield.js`）+ 变体检索端点（若需按 SKU 搜）+ 候选行显示
SKU / 商品 / 规格 / 价格 / **可用量**（服务端走 `VariantAvailabilities`，JS 档走端点）。

- 触发条件：阶段 1 上线后出现「按 SKU 找不到变体」「候选太多」的真实反馈；
- 注意越界申报：新增业务 JS 必须在 `internal/templates/static/js/ui/` 内，并说明为什么
  「只做 HTMX 覆盖不到的控件层」这一条容许它。

### 阶段 4 · 产品决策项（不阻塞前三个阶段）

- 「后台建单免支付直接成单」（当前只能建 pending 再流转）；
- 「客户登录后按邮箱认领历史订单」（阶段 1 起就存在的账户中心盲区）；
- 「建单前金额预览端点」（只读试算）。

---

## 8. 清单外发现（只报告，未改）

1. **`navPathAlias` 缺 `/admin/products/new`**：打开商品新建页时侧栏不高亮（本项顺手补自己的
   `/admin/orders/new`，商品那条建议单独清理；同类还有哪些子页面漏了映射值得一次扫全）。
2. **`POST /admin/coupons/create` 失败会整屏丢输入**（`couponRedirect` → 302 回列表页 + `?err=`，
   而表单在抽屉里、抽屉默认关闭）：与 `docs/02-T` 分档契约同族，属**批 2~7 尚未覆盖**的模块内债。
3. **`CreateOrderReq.CreatedVia` 是客户端可传字段**（`json:"createdVia"`，handler 仅在空值时补 `admin`）：
   影响 §4 的开关选型（不要拿它当安全语义），也意味着 `POST /api/order/create` 的调用方可以自称 `admin`
   从而拿到 `operator_type=admin` 的流转记录 —— 当前只有后台用这条 API，属**可接受**，但值得记一条审计点。
4. **无「按邮箱认领历史订单」能力**（user / order 两侧均无）：`user_id` 为空的订单在客户账户中心永久不可见。
5. **`GuestAccountProvisioner` 无可读查询**（只有 `EnsureGuestAccount`）：任何「先判断再决定」的
   策略（例如 §4 的 D）都要新增 user contract 方法，这是 D 被判 L 的直接原因。

---

## 附：本次任务的复核结论（对任务书背景事实的逐条核对）

| 任务书陈述 | 复核结果 |
|---|---|
| `POST /api/order/create` 已存在 | ✅ `order_router.go` 的 `g.POST("/create", permission.OrderCreate, h.CreateOrder)` |
| 权限点 `order:create`「新建订单」已声明 | ✅ `codes.go` 常量 + 中文名表；库侧由启动期 `SyncToDB` 幂等 upsert |
| `operatorFromContext` 会置 `CreatedVia="admin"` 并落 `CreateBy` | ✅ `order_handle.go` 的 `CreateOrder`（仅在 `CreatedVia == ""` 时补，且要求取到操作人） |
| `AdminNote` 注释写明「自建订单（createdVia=admin）」 | ✅ `dto/order_req.go` |
| `Attribution` 允许 nil | ✅ `marshalAttribution` nil → `{}`（列 NOT NULL） |
| `operatorTypeOf` 有 `CreatedViaAdmin → OperatorTypeAdmin` | ✅ `service/order.go` 的 `operatorTypeOf`，并在 `persistOrderTx` 落进状态流转日志 |
| 缺的只是后台 UI：`/admin/orders` 无建单入口 | ✅ 页面路由仅 GET + 5 个 POST 写动作；`routes.snapshot` 侧由 `TestOrdersEmptyCopyMatchesRoutes` 的判据 ① 钉着 |
