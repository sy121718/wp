# 后台页面骨架与信息密度规范（admin page shell）

> 本文是一次**全站设计评审**的结论与落地规范：范围是 `/admin` 下全部 39 个后台页面，
> 方法是「运行时 DOM 度量 + 逐页截图 + 源码结构分析」，不是抽样。
> 规范部分可直接作为新增页面的模板；改造模式部分给出逐页配方。

## 1. 评审结论

后台页面普遍存在四类病灶，彼此叠加：**说明文字当正文渲染**、**一页多职能纵向堆叠**、
**筛选与列表争夺空间**、**间距节奏缺失**。它们共同造成同一个结果 ——
用户打开页面后，首屏看到的几乎全是「解释」和「控件」，真正要处理的数据被推到屏幕之外。

### 1.1 实测数据（改造前）

| 页面 | 区块数 | 说明文字 | 说明占高 | 页高 | 首屏可见数据 |
|---|---|---|---|---|---|
| `/admin/settings` | 1 | 1662 字 | 596px | 1.85 屏 | 0 |
| `/admin/menus` | — | 0 | 0 | **6.83 屏** | 114 行（含 114 个内联表单） |
| `/admin/i18n` | 3 | 232 字 | 78px | 4.88 屏 | 0（51 个表单） |
| `/admin/masterdata/changes` | 4 | 356 字 | 98px | 3.99 屏 | 表格高 2438px |
| `/admin/inventory` | **7** | 690 字 | 255px | 1.74 屏 | 表格在第 0.78 屏 |
| `/admin/inventory/purchases` | **5** | 610 字 | 195px | 1 屏 | 0 |
| `/admin/inventory/sources` | **5** | 481 字 | 137px | 1 屏 | 0 |
| `/admin/analytics` | **7** | 693 字 | 254px | 1.25 屏 | 0 |
| `/admin/product-pricing` | 4 | 438 字 | 215px | 1.25 屏 | 0 |
| `/admin/returns` | 3 | 405 字 | 117px | 1 屏 | 0 |
| `/admin/customers` | 3 | 376 字 | 176px | 1 屏 | 0 |
| `/admin/orders` | 3 | 353 字 | 98px | 1 屏 | **0**（866px 首屏零条数据） |
| `/admin/coupons` | 3 | 328 字 | 98px | 1 屏 | 0 |
| `/admin/blocks` | 5 | 0 | 0 | 1 屏 | 0 |

密度最高的说明文字出现在 `i18n`（21 处）、`returns`（19 处）、`orders`（18 处）、
`product-pricing`/`customers`（各 17 处）、`settings`（15 处）。

### 1.2 根因

四个**未定义或定义不当**的基础类，是版面失控的直接原因：

1. `.form-field` —— `settings.html` / `orders.html` 等页面一直在用它，但 theme.css 与 ui.css
   里**从来没有这条规则**。表现是表单字段零间距堆叠（标签贴着上一个输入框）。
2. `.hint { margin: 0 }`（ui.css）—— 说明文字与它上面的输入框、与相邻的另一段说明之间
   都没有间距，只能靠换行分隔。一页十几个 hint 就糊成一片。
3. 没有 `.page-head` —— 标题、工程切换、主行动按钮全被塞进第一张卡片的正文里，
   于是「卡片一」既是页头又是表单，视觉上无法区分层级。
4. 没有 `.table-scroll` —— 列表容器没有任何高度约束，页面高度由行数决定，
   「列表看不见」与「页面被撑到 6.8 屏」是同一个缺失的两面。

## 2. 页面骨架规范

### 2.1 标准骨架

```html
<div class="stack">
  <!-- ① 页头：标题（+ 领域说明）、主行动、上下文选择 -->
  <header class="page-head">
    <div class="page-head-main">
      <div class="page-title-row">
        <h1>页面名</h1>
        <span class="help"><button type="button" class="help-btn" aria-expanded="false">?</button>
          <span class="help-pop" role="tooltip">领域说明……</span></span>
      </div>
    </div>
    <div class="page-actions">
      <button class="btn btn-primary">＋ 主行动</button>
      <form class="page-context">…工程/站点切换…</form>
    </div>
  </header>

  <!-- ② 主区：一张卡里装「工具行 + 撑满视口的列表」 -->
  <section class="card list-card">
    <div class="filter-bar">
      <div class="filter-row">…快捷筛选（状态徽章等）…</div>
      <form class="filter-row">…条件筛选…</form>
    </div>
    <div class="table-wrap table-scroll" tabindex="0">
      <table class="data-table">…</table>
    </div>
  </section>

  <!-- ③ 低频职能：折叠 -->
  <details class="section-fold card">
    <summary><span class="fold-title">次要职能</span><span class="fold-note">一句话说明</span></summary>
    <div class="fold-body">…</div>
  </details>
</div>
```

### 2.2 组件语义

| 类 | 用途 | 关键约束 |
|---|---|---|
| `.page-head` | 页头容器 | `flex-wrap`；**≤900px 自动转纵向堆叠**，动作行独占一行 |
| `.page-title-row` | h1 与它的 `?` 同行 | `.help` 不能作为 `.page-head` 的直接子项（会被 `space-between` 推到页面中间） |
| `.help` / `.help-btn` / `.help-pop` | 领域说明的悬浮载体 | 纯 CSS 可 `hover` / `focus-within` 展开；admin.js 补 Esc、外点关闭、贴右边缘左翻 |
| `.form-field` | 单个表单字段 | 补齐了缺失的定义：label 与控件间距 `--ui-sp-xs`，字段之间 `--ui-sp-lg` |
| `.form-grid` | 短字段两列排布 | `minmax(min(100%,400px),1fr)`；`--wide` 修饰符跨列；≥1320px 固定两列 |
| `.list-card` | 列表主卡 | `display:flex; flex-direction:column` |
| `.filter-bar` / `.filter-row` | 工具行 | 一行放快捷筛选、一行放条件筛选；筛选不再独占卡片 |
| `.table-wrap.table-scroll` | 撑满视口的滚动列表 | `max-height: max(240px, calc(100dvh - 300px))` + 表头 sticky；**≤720px 放开高度**（窄屏走整页滚动/堆叠） |
| `.empty-state` | 空数据 | 标题 + 一句话 + 主行动；**不再是一段说明** |
| `.section-fold` | 次要职能折叠区 | `<details>` 原生折叠，零 JS；`.fold-body` 承担内边距 |
| `.kv` / `.kv-item` | 详情键值对 | `<dl>` 网格；替代「客户：… / 金额：…」那一串松散段落 |

### 2.3 改造配方（逐页机械执行）

1. **第一张卡**里的标题 → `.page-head` 的 h1；工程/站点切换 → `.page-context`；
   该卡里的大段说明 → 合并成一个 `.help`。
2. **「新建 X」卡片** → 删除该卡，按钮提到 `.page-actions`（抽屉仍由 `data-drawer-open` 驱动）。
3. **筛选卡片** → 并入列表卡的 `.filter-bar`（快捷筛选一行、条件筛选一行）。
4. **列表** → 外层加 `table-scroll`；空数据改 `.empty-state`。
5. **其余职能** → 保留在主卡下方，改成 `.section-fold card` 折叠；
   与主职能强相关的（如「库存变动」对「库存流水」）放主卡内、列表上方。
6. **字段旁的长说明** → `.help`，放在 label 右侧（`.field-label-row`）。
7. **详情类页面**的「标签 + 值」段落 → `.kv` 网格。

## 3. 已完成的改造（第一批）

| 页面 | 改造前 | 改造后 |
|---|---|---|
| `/admin/settings` | 1.85 屏；1662 字说明铺满正文 | **1 屏**；说明 1662 → 68 字（全部进 `.help`）；字段两列；高级项折叠 |
| `/admin/orders` | 3 张卡；首屏 866px 零条数据 | 页头 + 工具行 + 撑满视口的列表；空状态含主行动 |
| `/admin/inventory` | **7 个区块**纵向平铺；列表被挤到最下 | 页头 + 主卡（变动表单可折叠 + 流水占主区）+ 3 个折叠区；1.58 屏 |
| `/admin/inventory/purchases` | 5 个区块；每块带大段说明 | 页头 + 主列表 + 2 个折叠区 |

## 4. 顺带修掉的缺陷

**4 个页面的顶栏在显示原始 i18n key**（`MsgMasterDataChangesTitle — go_wp 管理后台`）。

`internal/web/shell/shell.go` 会对 title 做 `t(title, title)`：词条命中显示译文，
未命中**回退字面量**。因此 handler 传 `Msg*Title` 而词条缺失时，key 会原样出现在顶栏与
`<title>` 里。全站实测命中 4 个页面：

- `/admin/content-templates`（`MsgContentTemplatesTitle`）
- `/admin/inventory/sources`（`MsgInventorySourcesTitle`）
- `/admin/inventory/purchases`（`MsgInventoryPurchasesTitle`）
- `/admin/masterdata/changes`（`MsgMasterDataChangesTitle`）

修复：迁移 [228_i18n_seed_page_titles.sql](../public/migrations/228_i18n_seed_page_titles.sql)
补 4 key × 2 语言词条，注册在 `register_i18n_layer.go`。
**新增传 `Msg*Title` 的页面必须同批 seed 词条**，否则它的顶栏会直接露出 key。

## 5. 验收方法

改造后的页面按下表逐项核对（均可在浏览器控制台取值）：

| 项 | 判据 |
|---|---|
| 页面高度 | 单职能页面 ≤ 1 屏；多职能页面 ≤ 2 屏 |
| 说明文字 | 首屏内 `.hint` 总高 ≤ 60px（说明应进 `.help`） |
| 列表可达 | 首个数据表格顶边 ≤ 0.8 屏 |
| 列表可用 | `.table-scroll` 的 `max-height` 生效且表头 `position: sticky` |
| 多端 | 1440 / 768 / 375 三视口下 `document.documentElement.scrollWidth === clientWidth` |
| 触屏 | `.help` 可点按展开（`:focus-within`），不依赖 hover |
| 键盘 | Tab 可达 `.help-btn`，Esc 收起 |

## 6. 进度

- 已完成：`settings`、`orders`、`inventory`、`inventory/purchases`、`products`
- 待改造（按「说明文字量 × 区块数」排序）：
  `i18n`(21 hints/4.9 屏)、`returns`(19)、`product-pricing`(17)、`customers`(17)、
  `analytics`(7 区块)、`inventory/sources`(5 区块)、`blocks`(5)、`masterdata/changes`(4)、
  `coupons`、`pages`、`navigations`、`site-slots`、`seo`、`mail*` 各页
- 结构本身健康、只需微调的：`menus`（工具行已紧凑，但 114 行内联表单建议改批量操作）、
  `permissions`、`media`、`plugins`

> 商品列表（`products`）本批收口的四件事，对其它列表页同样适用：
> ① **筛选与分页是列表页的基座**（关键词 + 状态 + 服务端分页），不靠「一页取 100 条」硬撑；
> ② **操作列只放这一行才做的事**：编辑（进编辑页）、预览、删除 ——
> 「详情」由名称列承担，不在操作列重复一个同义入口；URL 段这类「详情里看得更全」的列不进列表；
> ③ **写与读分页**：`/admin/products/edit` 是商品域**唯一**的编辑界面（基本字段 + 属性引用 +
> 分类与品牌 + 手工标签 + 变体清单 + 评分 + SEO 检查 + 详情页模板动作 + 多语言入口），
> `/admin/products/detail`（点名称进入）**只读**展示「这个商品由什么组成」。
> 读与写混在一页时，用户分不清「我在看还是在改」，而每个写表单都要带 CSRF、错误回显与回跳地址；
> ④ **写操作的 PRG 回跳到编辑页**（`productEditLocation`）：详情页只读之后，回详情页等于把用户
> 丢到一个没有表单的页面上；参数用 `url.Values.Encode()` 生成，测试断言一律解析后比较（不拼字符串前缀）。

> 第二轮（功能归属与筛选形态）见 §7 —— 那一轮的判据对**其余所有页面**同样适用。
## 7. 第二轮评审：功能归属与筛选形态

第一轮修的是**版式**（说明占版面、列表被压）。第二轮指出的是**信息架构**问题
——「功能都有问题吧」「完全没法用」。这一轮的结论同样适用于其余页面。

### 7.1 一个职能只应出现在它归属的页面

| 原位置 | 职能 | 判定依据 | 现位置 |
|---|---|---|---|
| 采购入库页 | 生产入库（自家工厂） | 与采购单无关（自家工厂根本没有采购单）；变动原因字典里本来就已有 `production_in` | 库存管理页 |
| 采购入库页 | 进货历史 | 它就是「按 SKU 看过往入库流水」，不值得单独维护一张表 | 库存管理页的流水筛选（SKU + 原因） |
| 全部后台页面 | 工程 / 站点切换器 | 只有一个工程时它不提供任何选择，纯占位 | 单工程时隐藏（`len(.Projects) > 1` 才渲染） |

**判据**（可直接套用到其余页面）：

1. 如果一个功能能用**现有页面的筛选条件**表达，它就不该是一个独立区块
   （进货历史 → 流水筛选）；
2. 如果一个操作与所在页面的**主语**无关，它就换了页面
   （生产入库与「采购单」无关 → 归到「库存」）；
3. 如果控件在**当前数据下没有可选值**，它就不该出现（单工程下的工程切换器）。

注意「生产入库」与「库存变动」**没有**合并成一个表单 —— 库存变动契约
（`StockChangeLineReq`）不带成本价字段，而生产入库必须手工写成本价。
两者并存是正确的：同属「手动改库存」，区别只在成本价那一个字段。

### 7.2 筛选区：项目名在左、控件在右，**横向紧凑排列**

`.filter-fields` / `.filter-field`（theme.css §11）：每个条件是「label + 控件」的紧凑单元，
横向排列、自动换行；label 不换行、控件限宽 200px，**宽屏一行放 3~4 个**。

> 这一节我改了两轮才做对，记下两次错法以免重犯：
> ① 最初把筛选控件铺满一行、靠 placeholder 解释语义 —— 控件互相挤压且语义不清；
> ② 第二轮改成「一行一个条件」的固定网格 —— 宽屏上 4 个条件被拉成 4 行、右侧整片留白，
>    **比原来更浪费**。
> 另一个坑：`.filter-fields` 在 `.filter-bar`（flex 容器）里必须 `flex: 1 1 100%`，
> 否则它会收缩到 min-content、退化成单列 —— 这正是错法 ② 在浏览器里的实际成因。

**不要**在筛选区写解释性文字 —— 那类说明一律进 `.help` 悬浮。
按钮文案要**自解释**：写「＋ 入库 / 出库 / 调整」而不是「库存变动」（后者是抽象名词，
用户看不出点它会发生什么）。

```html
<div class="filter-fields">
  <div class="filter-field"><label for="f-type">类型</label><select id="f-type" name="type">…</select></div>
  <div class="filter-field"><label for="f-kw">关键词</label><input id="f-kw" name="keyword"></div>
  <div class="filter-actions"><button class="btn btn-sm">筛选</button><a class="btn btn-sm btn-ghost">重置</a></div>
</div>
```

### 7.3 说明文字一律悬浮，包括折叠区的注解

折叠区的 `<summary>` 里不再跟一行说明（如「不走采购单，成本价手工填写」）——
它同样占据横向空间且不可折叠，一律收进该行的 `.help`。

### 7.4 第二轮的实测结果

| 页面 | 改造前 | 改造后 |
|---|---|---|
| `/admin/inventory` | 7 区块；无流水筛选 | 页头 + 主卡（库存变动 / 生产入库 / 流水筛选 + 表）+ 3 折叠区；`filter-field` 逐行排列 |
| `/admin/inventory/purchases` | 5 区块（含生产入库、进货历史） | **只有采购单 + 收货**，首屏 608px（1 屏内） |
| `/admin/inventory/sources` | 5 张卡、1.88 屏 | 页头 + 筛选 + 列表 + 折叠统计，**1 屏** |

### 7.5 菜单层级：模块边界要反映到目录上

库存模块原先的「库存管理 / 货源管理 / 采购入库」挂在**「商品与库存」**目录下，与商品八项
（商品/属性/分类/品牌/标签/定价/捆绑/详情模板）并列。这是库存管理页装不下的**根本原因** ——
页面在替菜单还债。模块在代码里本来就是独立的（`internal/module/product/inventory/`）。

迁移 `229_inventory_menu_split.sql` 把它独立成一级目录：

```text
库存
├── 库存管理      /admin/inventory            ← 看流水 + 改库存（写操作走抽屉）
├── 仓库管理      /admin/inventory/warehouses
├── 货源管理      /admin/inventory/sources
├── 采购入库      /admin/inventory/purchases
└── 变动原因字典  /admin/inventory/reasons
```

**判据**：菜单是信息架构的第一层表达。当一个页面必须靠折叠才装得下，先看它的父目录是不是
把不相关的实体混在了一起。

### 7.6 折叠 vs 拆页

折叠**只适用于「同一职能的次要细节」**，不适用于「另一个职能」。
库存管理页改造后折叠数为 **0**：仓库 → 独立页、原因字典 → 独立页、
SKU 各仓库存 → 并入流水筛选（同一查询对象的另一个视角）、
登记变动/生产入库 → 右侧抽屉（项目既有的 `data-drawer-open` 机制）。

### 7.7 顺带发现的配置缺口

服务端口只从 `config.yaml` 读，**没有环境变量覆盖**：`GOWP_SERVER_PORT=8090 go run cmd/main.go`
仍会绑在 8080（`config.Init("config.yaml")` 硬编码文件名，无 `AutomaticEnv`）。
需要换端口时得改配置文件。

