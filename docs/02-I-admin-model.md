# 后台整体模型（信息架构 / UI 基座 / 操作逻辑）

> 本文是**逐页改造的基准**。方法：运行时 DOM 度量（61 个模板 + 64 条页面路由，全量非抽样）
> + 源码结构分析 + 已有规范文档（`02-H-admin-page-shell.md`）对账。
>
> 与 `02-H` 的分工：`02-H` 记录**上一轮评审的结论与已完成改造**；本文建立**当前全站模型**
> —— 有哪些实体、页面怎么分层、控件与令牌有哪些、当前实测状态如何、还剩哪些缺口。
> 改造配方仍以 `02-H` §2.3 为准，不在此重复。

## 1. 页面全景（64 条路由 / 61 个模板）

### 1.1 按职能分层

| 层 | 页面 | 数量 |
|---|---|---|
| **仪表盘** | `/admin` | 1 |
| **列表页** | products、orders、customers、coupons、returns、pages、blocks、articles、themes、media、i18n、menus、plugins、administrators、roles、permissions、departments、datarules、navigations、product-attributes/-brands/-categories/-tags、inventory、inventory/warehouses、inventory/sources、inventory/purchases、inventory/reasons、masterdata/changes、page_redirects、mail 系列、site-slots、seo、analytics | ~40 |
| **编辑页（整页写）** | articles/new、articles/edit、products/new、products/edit、products/detail、content-templates/edit、datarules/edit、themes/settings、mail/automation/edit、customers/detail、roles/permissions、article_translations、page/translations、navigation_translations、product_translations | 15 |
| **纯配置页** | settings、theme、product-pricing、mail 账号/模板 | 4 |
| **画布类** | workbench、workbench/preview、mail/automation/canvas | 4 |

### 1.2 菜单映射（8 个一级目录 / 53 个二级项）

真源是 `sys_menus` 表（`admin` 按权限过滤成树），壳层只补当前页标记与展开态。

| 一级 | 二级项 | 观察 |
|---|---|---|
| 仪表盘 | （直接链接） | — |
| 管理 | 管理员 / 角色管理 / 菜单管理 / 权限资源 / 部门管理 / 数据权限 | 6 项，自洽 |
| 内容 | 文章 / 页面管理 / 区块管理 / 内容模板 / 媒体管理 / 导航菜单 / 文案词条 / 商品详情模板 | 8 项；**「商品详情模板」属商品域却挂在内容下**（见 §5 P1-1） |
| 商品与库存 | 商品管理 / 商品属性 / 商品分类 / 商品品牌 / 商品标签 / 变更记录 | 6 项；**「变更记录」是全局主数据审计**，不专属商品（见 §5 P1-2） |
| 库存 | 库存管理 / 仓库管理 / 货源管理 / 采购入库 / 变动原因字典 | 5 项，自洽（「生产入库」已按 `02-H` §7.1 归到库存页） |
| 交易 | 订单管理 / 退货入库 / 优惠码 / 客户管理 | 4 项，**「退货入库」名不副实**（见 §5 P1-3） |
| 站点 | 主题管理 / 站点设置 / SEO 控制台 / 访问统计 / 重定向管理 | 5 项，自洽 |
| 系统 | 邮箱管理 / 邮件营销 / 邮件活动 / 邮件自动化 / 插件管理 | 5 项；邮件占 4 项，可接受 |

## 2. 设计令牌（字号 / 间距 / 颜色）

### 2.1 唯一真源

`theme.css` `:root` 定义**数值尺度**；`[data-theme]` 把后台色板 `--c-*` 以 `--sky-c-*`
同名暴露（名字统一、取值各供 —— 产物侧由 `builder.ThemeVarsCSS` 按站点主题生成，后台不随主题变）。

| 类别 | 令牌 | 取值 |
|---|---|---|
| 字体 | `--font-sans` | 系统栈 + Noto Sans SC / PingFang SC / Microsoft YaHei |
| | `--font-mono` | SF Mono / Fira Code / JetBrains Mono / Consolas |
| 圆角 | `--r-xs…--r-xl`, `--r-pill` | 4 / 6 / 8 / 12 / 16 / 9999px |
| 间距 | `--sp-xxs…--sp-xxl` | 2 / 4 / 8 / 12 / 16 / 24 / 32px |
| 布局 | `--sidebar-w` / `--topbar-h` | 240px / 56px |
| 动效 | `--tr-fast` / `--tr-norm` | 120ms ease / 200ms ease |
| 基座间距 | `--ui-sp-xs/sm/md/lg` | ui.css 消费，值对齐 `--sp-*` |
| 基座动效 | `--ui-motion-fast/base` | ui.css 消费 |

**根字号 `html { font-size: 15px }`** —— 全站 rem 基准，改它会等比缩放所有页面。

### 2.2 颜色语义（后台色板）

| 令牌 | 亮色取值 | 用途 |
|---|---|---|
| `--c-primary` / `-deep` / `-soft` / `-bg` | `#3d444f` / `#2f353d` / `#5b6572` / `#eceef1` | 主色系（中性深灰，非蓝） |
| `--c-bg` / `-soft` / `-active` / `-hover` | `#fff` / `#f4f5f6` / `#e4e4e4` / `#f4f5f7` | 页面底 / 卡片底 / 激活 / 悬浮 |
| `--c-surface` | `#fff` | 卡片与浮层底 |
| `--c-text` / `-secondary` / `-mute` / `-faint` | `#1a1d21` / `#4b5563` / `#6b7280` / `#5d656e` | 四级文字层次 |
| `--c-border` / `-strong` / `-input` | `#e5e7eb` / `#d1d5db` / `#c6ccd4` | 分隔 / 强调 / 输入框 |
| `--c-success` / `-bg` | `#059669` / `#1b2820` | 成功与徽章底 |
| `--c-warning` / `-bg` | `#c2a06a` / `#2a2318` | 警告与徽章底 |
| `--c-danger` / `-bg` | `#dc2626` / `#2b1c1c` | 危险与徽章底 |

**两条纪律**（有测试守）：别名值**必须带兜底**（`var(--c-x, fallback)`，否则解析成
guaranteed-invalid 会让整条声明失效）；别名选择器用 `[data-theme]` 而非 `:root`
（`--c-*` 定义在 `[data-theme]` 上，避免「别名先解析、主题值后到达」的空窗）。

**新增颜色一律走 `--sky-c-*`**，禁止写死十六进制 —— 写死 = 深色主题下退化成亮色值。

## 3. 控件库（`static/js/ui/`，前后台共用同一份基座）

### 3.1 控件清单

| 控件 | 文件 | 契约（模板侧） |
|---|---|---|
| 自绘下拉 | `select.js` (300) | `<select>` 渐进增强；原生仍在 DOM 里（视觉隐藏），表单提交照旧 |
| 确认框 | `confirm.js` (163) | `data-confirm` / `data-confirm-danger`，`<dialog>` 承载 |
| 模态弹窗 | `modal.js` (146) | `.wb-modal`，`<dialog>` + `showModal()` |
| 抽屉 | `drawer.js` (134) | `data-drawer-open="#tpl-x"` + `data-drawer-title` |
| 轻提示 | `toast.js` (95) | `WBUI.toast`；非模态反馈 |
| 按钮忙碌态 | `busy.js` (64) | 防重复提交 |
| 媒体字段 | `mediafield.js` (205) | `partials/media_field.html` |
| 颜色字段 | `colorfield.js` (116) | 文本框 + 色块取色 |
| 图标字段 | `iconfield.js` (87) | `data-icon-field` + `data-icon-name`；图标库 766KB **懒加载** |
| 主题切换 | `themetoggle.js` (44) | `data-theme-toggle` |
| 基座入口 | `index.js` (21) | `WBUI.scan`：DOM 就绪扫描 + **htmx 局部替换后重扫** |

### 3.2 关键契约

- **htmx 局部替换后必须重扫**：`index.js` 的 `WBUI.scan` 覆盖到哪儿就初始化到哪儿。
  增强脚本**不得因为「初始化时没找到元素」提前 return** —— 字段常随抽屉在打开时才进 DOM。
- **所有 POST 必须带 CSRF**：HTMX 经 `<body hx-headers>` 继承；原生表单显式加隐藏域；
  fetch 请求带 `X-CSRF-Token`。
- **htmx 是本地 vendor**（`ui/htmx.min.js`），产物按需内联（页面有 `hx-*` 属性才注入）。

## 4. 结构组件

### 4.1 页面骨架（`02-H` §2.1 已定义，此处只列现状）

```
.stack
├─ header.page-head          → .page-head-main（.page-title-row：h1 + .help）
│                              / .page-actions（主行动 + .page-context 工程选择）
├─ section.card.list-card    → .filter-bar（.filter-row 快捷筛选 + .filter-fields 条件筛选）
│                              / .table-wrap.table-scroll（表头 sticky）
└─ details.section-fold.card → 低频职能折叠
```

### 4.2 列表页标准形态（全站统一）

首列勾选框（`data-check-all` + `data-check-item`）→ 数据列 → 末列 `.col-actions` 右对齐
（编辑在前、删除在后 `btn-danger`）；选中出现 `.bulk-bar` 显示计数与批量动作。

### 4.3 分页

`partials/pagination.html` —— **服务端渲染、零 JS**。单页或空数据时 handler 不注入
`PaginationInfo`/`PaginationLinks`，模板自然不渲染。

### 4.4 树状

`partials/nav-nodes.html` —— 递归 block，`depth > 3` 后缩进封顶 36px（防深层级无限缩进）。
**导航节点的树由 `navigation` 模块管理**；`menus.html` 的权限菜单树是另一套（117 行内联表单，见 §5）。

### 4.5 图标

`sys_menus.icon` 存 Lucide 名 → `icons.js` 查表出 SVG；查不到**退回默认齿轮**，
模板不维护「名字 → 图标」映射。列表图标由 `data-icon-name` + `WBUI.scan` 驱动。

## 5. 当前实测状态与缺口

**度量口径**：`screens` = 页高/视口高；`hintRatio` = 正文说明文字总高/视口高
（`02-H` §5 判据：首屏 `.hint` 总高 ≤ 60px，即 ratio ≤ 0.066）。

### 5.1 全站度量表（2026-09 实测，视口 1646×908）

> **⚠️ 度量口径已于本轮修正 —— 旧版「说明占比」整列作废。**
>
> 旧口径把 `.hint/.text-mute/.form-hint/.page-sub` **全部**计入「正文说明文字」，
> 但列表页普遍用 `.text-mute` 渲染**单元格内的空值占位与次要信息**
> （`<td class="text-mute">—</td>`、`<td class="text-mute text-sm">{{r.UpdatedAt}}</td>`），
> 它们属合规用法（`admin-ui-logic` §4「空值写 `—` 配 `.text-mute`」）。
>
> 实测证据（旧口径 vs 修正口径）：
> | 页面 | 旧口径 | 其中在 `<table>` 内 | **修正后（真实正文说明）** |
> |---|---|---|---|
> | `/admin/articles` | 2.847 | **50 / 50（100%）** | **0** |
> | `/admin/menus` | 6.663 | **117 / 117（100%）** | **0** |
> | `/admin/products` | 0.901 | **53 / 53（100%）** | **0** |
> | `/admin/dashboard` | 0.455 | **8 / 8（100%）** | **0** |
> | `/admin/pages` | 0.740 | **13 / 13（100%）** | **0** |
> | `/admin/settings` | 0.330 | **5 / 5（100%）** | **0** |
> | `/admin/blocks` | 0.340 | **6 / 6（100%）** | **0** |
> | `/admin/inventory` | 0.021 | 0 | 0.021（**唯一真实超标项**，20px） |
>
> **结论：全站 22 个抽样页面中，正文说明文字占比几乎全部为 0** —— 说明文字进 `.help` 这条
> 硬规则（`02-H` §2.1）**执行得比原判断好得多**。旧文档「说明超标 100 倍 / 43 倍」等表述
> 是**度量错误**，不是实际缺陷。
>
> **三份独立报告交叉核实**（商品域 / 内容交易域 / 管理站点域，各自用独立 Chrome 实例）：
> `menus` 的 6.663、`dashboard` 的 0.455、`articles` 的 2.847 等**全部为表格内单元格假阳性**，
> 按正确口径排除后均为 **0**。这一修正由两个独立代理分别复现，可信度高。
>
> **仍有一处口径缺口**（第三方反馈）：清单 §F1 的四个选择器漏了 `.perm-intro` / `.perm-hint`
> （`role_permissions.html:24/54` 有约 120 字正文说明），导致该页漏检。已补入 `02-J` §F1。
>
> **修正后的度量口径**（后续所有度量照此执行）：
> ```js
> const all = [...document.querySelectorAll('.hint,.text-mute,.form-hint,.page-sub')];
> const real = all.filter(e => !e.closest('table')      // 排除单元格占位
>                           && !e.closest('.help-pop')  // 排除悬浮说明（本就不占正文）
>                           && !e.closest('template')   // 排除未渲染的抽屉模板
>                           && e.getBoundingClientRect().height > 0);  // 排除隐藏元素
> const hintReal = real.reduce((s,e)=>s+e.getBoundingClientRect().height,0) / innerHeight;
> ```
> **判据不变**：`hintReal > 0.066`（60px/908px）即违规。修正后全站仅 `inventory` 一处 0.021，
> 在阈值内 —— **本轮无说明文字类缺陷**。

**度量可信性**：浏览器是**单例**，多任务共用同一空间时页面会互相抢占；且**任一任务执行
`/admin/dev-login` 会轮换会话、把其它任务的登录态踢掉**（本轮实测：一个批量脚本 38 页全部
落在 `/admin/login`）。本表每一项都在读数前校验 `location.href` 与目标一致（`verified` 标记），
被抢占的读数已丢弃重测。
>
> **并行扫描的正确做法**：给每个任务**独立 Chrome 实例**（`puppeteer-core` + 独立
> `user-data-dir`），而不是共用 ego 浏览器的不同 task space —— 实测三个并行任务即便各用
> 独立 space 仍互相抢占。

| 页面 | 屏数 | 说明占比(修正) | 行数/首屏可见 | 表格顶边 | 勾选 | 体积KB | 表单/模板(节点) | 问题 |
|---|---|---|---|---|---|---|---|---|
| `/admin` 仪表盘 | 1.00 | 0 | 8/8 | 0.35 | ✗ | 28 | 1 / 0 | ✅ |
| `/admin/products` | 1.05 | 0 | 20/11 | 0.29 | ✓ | 152 | 43 / 2(288) | 含**死模板** |
| `/admin/orders` | 1.00 | 0 | 0/0 | — | ✓ | 31 | 2 / 0 | ✅（空态含指引） |
| `/admin/inventory` | 1.00 | 0.021 | 0/0 | — | ✗ | 69 | 2 / 1(208) | **P0 文案**（已修，见 §5.2 #1） |
| `/admin/media` | 1.07 | 0 | 24/24 | 0.00 | ✗ | 47 | 1 / 0 | **无骨架 / 无批量** |
| `/admin/i18n` | 1.07 | 0 | 50/10 | 0.23 | ✓ | **258** | **53 / 51(1326)** | 51 模板内嵌表单 |
| `/admin/returns` | 1.00 | 0 | 0/0 | — | ✓ | 31 | 2 / 0 | ✅ |
| `/admin/settings` | 1.25 | 0 | 5/0 | 1.00 | ✗ | 43 | 3 / 0 | **首屏 0 行数据** |
| `/admin/customers` | 1.00 | 0 | 0/0 | — | ✓ | 31 | 2 / 0 | ✅ |
| `/admin/coupons` | 1.00 | 0 | 0/0 | — | ✓ | 36 | 2 / 1(43) | ✅ |
| `/admin/pages` | 1.07 | 0 | 13/12 | 0.26 | ✓ | 56 | 15 / 2(21) | 缺筛选 |
| `/admin/themes` | 1.00 | 0 | 1/1 | 0.17 | ✗ | 30 | 1 / 1(10) | ✅ |
| `/admin/blocks` | 1.07 | 0 | 3/3 | 0.62 | ✓ | 38 | 5 / 1(34) | 一页三职能；**全选框漏选** |
| `/admin/articles` | 1.35 | 0 | 50/5 | 0.62 | ✓ | 121 | **52** / 0 | 首屏仅 5 行（行高 52px，非失控） |
| `/admin/analytics` | **2.15** | 0 | 25/8 | 0.42 | ✗ | 35 | 2 / 0 | 超 2 屏（5 表并列，应改 tabs） |
| `/admin/inventory/warehouses` | 1.00 | 0 | 1/1 | 0.22 | ✓ | 39 | 3 / 2(92) | ✅ 合规 |
| `/admin/inventory/sources` | 1.00 | 0 | 0/0 | — | ✓ | 38 | 2 / 1(32) | ✅ 合规（已改造） |
| `/admin/inventory/purchases` | 1.00 | 0 | 0/0 | — | ✓ | **212** | 2 / 2(**747**) | ✅ 结构合规；模板节点多 |
| `/admin/inventory/reasons` | 1.00 | 0 | 9/9 | 0.22 | ✗ | 39 | **10** / 1(21) | 9 行 10 表单，应批量 |
| `/admin/menus` | 1.00 | 0 | 117/12 | 0.23 | ✓ | **490** | **119 / 118(4603)** | **117 行各内嵌编辑表单** |

> **新增度量轴的意义**：`体积KB` 与 `模板节点` 揭示了一类此前未被度量的成本 ——
> `/admin/menus` 一页下发 **490KB HTML、4603 个模板内节点**，`/admin/i18n` 258KB / 1326 节点。
> 它们的「页面高度」只有 1 屏（看起来正常），但传输与解析成本是普通页面的 10 倍以上。
> **这才是 menus/i18n 的真实问题**（不是「说明超标」）。

### 5.2 已确认缺陷

| # | 级别 | 位置 | 问题 | 证据 |
|---|---|---|---|---|
| 1 | ~~P0~~ **已修** | `inventory.html:201` | 空态文案与 UI 不符：「还没有库存流水 —— **先用上面的表单做一次入库**」，但该页**根本没有入库表单**（入库在采购入库页）。模板里写的是正确文案，**被库里 i18n 旧值覆盖** | 迁移 `191_i18n_seed_product_inventory.sql:894` 写入旧值；浏览器实测确认。**根因**：该 seed 用 `ON CONFLICT DO NOTHING`（seed 是默认值来源、后台是真相来源），所以**改模板文案必须同批写新迁移**。→ **已修**：迁移 `317_fix_inventory_moves_empty_i18n.sql` + `register_inventory_moves_empty_i18n.go`（UPDATE 带旧值前置条件，不覆盖运营手工修改） |
| 2 | **P0** | `article_router.go:52` | **`/admin/articles/new` 含超管全员 403**，内容域主写入路径完全不可用 | `CasbinMiddlewareForPath` 的 act 硬取 `c.Request.Method`（`casbin.go:56`），该 GET 页面复用了 `/api/content/create`，而库中策略是 `POST` → `Enforce(sub, obj, "GET")` 恒 false。**已修**：新增 `CasbinMiddlewareForPathAs(obj, act)` + 调用点显式声明 `http.MethodPost` |
| 3 | **P0** | `theme_settings_admin_pages.go:148/153`、`theme_admin_pages.go:145/243`、`admin_pages_handle.go:127/145` | **缺参/数据不存在时 `c.String` 直出**，页面**完全脱离页壳**（实测 DOM 仅 9 节点、`document.title` 为空、无导航）——用户只能手改地址栏 | 违反 `AGENTS.md`「后台页面 handler 禁止 `c.String(500, err.Error())` 直出」硬约束的**第①种形态**。`datarule_edit` 直出裸 i18n key `MsgFieldRequired` |
| 4 | **P1** | `menus.html` / `i18n.html` | 117 / 50 行 × 每行一个 `<template>` 内嵌完整编辑表单 → menus **490KB / 118 模板 / 4603 节点 / 119 form**；i18n **258KB / 51 模板 / 1326 节点** | 运行时实测。模板定义 `menus.html:158-209`（`id="tpl-menu-edit-{{m.ID}}"`）。**注意**：真实成本是**体积**，不是「说明超标」（旧口径错误，见 §5.1） |
| 5 | **P1** | `products.html:254` | **死模板** `tpl-product-create` —— 新建商品已改为整页 `/admin/products/new`（52/108 行是链接），整个表单 DOM 仍随列表页下发 | `grep -rn tpl-product-create` 仅命中定义处、**无任何引用** |
| 6 | **P1** | `media.html` | **无 `.page-head`、无 `.filter-bar`、无 `.h1`、无批量选择** —— 全站唯一完全未套骨架的列表页 | 运行时 `pageHead:0, filterBar:0, h1:""`；源码 `grep -cE 'page-head\|filter-bar\|data-check-all\|col-actions\|<h1' media.html` = **0**。24 行数据要点 24 次才能删完 |
| 7 | ~~P1~~ **作废** | ~~`blocks.html:163/194/225` 三段表格共用一个全选框 → 段间静默漏选~~ | **父 agent 误判，已用实测推翻**：`admin.js:522` 的 `scopeOf(el)` 取 `el.closest('form')`，而三段表**同处一个 `<form>`**（`blocks.html:143` 的 form 包住整个 `.list-card`），全选联动正常（实测勾选 3 行 → 「已选 3 项」）。**但三层嵌套 `{{if}}` 仍是设计信号** —— 它是「三段拆表」逼出来的，合并成一张表后自然消失 |
| 8 | **P1** | 7 个列表页 | **缺 `.filter-bar`**：articles、pages、blocks、navigations、article_translations、navigation_translations、customer_detail | 运行时 `filterBar:0`。`02-H` §7 已把「关键词 + 状态 + 服务端分页」定为列表页基座 |
| 9 | **P1** | `analytics.html` / `masterdata_changes.html` | **同一份数据多张同构表并列**：analytics 5 张（日期/路径/来源/设备/语言，均「维度+浏览数+独立访客」三列）、masterdata 2 张（明细 + 汇总） | 运行时 `tables:5` / `tables:2`，analytics `screens:2.15` 超 2 屏。判据 `admin-ui-logic` §7.1「同一份数据的不同切法用 tabs」——项目已内置 `.tabs` 组件 |
| 10 | **P1** | `product_brands.html:151/187` | SEO 描述用单行 `<input type="text">`；**而同域 `product_categories.html:161` 已是合规的 `<textarea rows="3" data-counter="155">`** —— 同域内实现漂移 | 违反 `admin-ui-logic` §8 硬规则 2（meta 必须纯文本 + 字数提示）。`product_edit.html:110` 有 textarea 但漏 `data-counter` |
| 11 | **P1** | `product_detail_template_page.go:120-123` | **死入口**：`/admin/products/template` 不带 `?product=` 时**静默 302 回商品列表**，但菜单「内容 → 商品详情模板」有直接链接 → 点了回到列表，无任何提示 | 实测 302。与 #3 同族（缺参静默） |
| 12 | **P2** | `mail_marketing.html:95` / `mail_automation.html:136-139` | i18n 库值与模板默认值脱节：空态 `.empty-title` 与 `.empty-desc` **输出同一句话**（库值覆盖了模板里的完整句） | dbx 实查 `sys_i18n`：`admin.mail.marketing.contacts.empty` 库值 = 「没有匹配的联系人。」，而模板写的是完整句。**这是 #1 同类缺陷的第二次出现** —— #1 修了但没加回归测试 |
| 13 | **P2** | `products.html` 状态列 | 状态输出英文裸值 `<span class="badge">published</span>`（20 行），同页库存列却已本地化（`∞ 无限`） | 违反「同一概念全站同名」；判据 `admin-ui-logic` §4 |
| 14 | **P2** | `site_slots.html` | 行内说明**逐行重复**：下拉占位符塞了「（选择页面：显示的是页面草稿路径）」（10 行重复 10 次）；单元格内放完整说明句 | `hintReal` 修正口径下这是「说明进 `.help`」硬规则的**未记录变体**：不进正文，改进行内单元格/控件占位符，视觉上「没占正文空间」，按行重复后总成本更高 |
| 15 | **P2** | `plugins.html` | 空态「安装插件」按钮链接是**锚点占位** `href="#plugin-install"`，且页头 `.page-actions` 为空 —— 非空态时页头**没有任何安装入口** | 判据 `admin-ui-logic` §2.2「主行动进 `.page-actions`」 |
| 16 | **P2** | `coupons.html:54` + `:114` | 空数据时同屏渲染**两个**「＋ 新建优惠码」按钮（页头一个、空态一个） | §2.2 与 §7 各自都要求主行动，但**不应并存**；二选一 |
| 17 | ~~P2~~ **作废** | ~~多个列表页说明占比超标~~ | ~~articles 2.85 / products 0.90 / pages 0.74 / dashboard 0.455 / blocks 0.34 / settings 0.33~~ | **度量口径错误导致误判** —— 修正后全部为 **0**（见 §5.1）。此项作废 |

### 5.2.1 本轮并行检测新增（18–30，均已交叉验证）

| # | 级别 | 位置 | 问题 | 证据 |
|---|---|---|---|---|
| 18 | **P0** | `sys_menus` 的「商品详情模板」项 | **菜单死链**：该项 href=`/admin/products/template`（无参数），而 `product_detail_template_page.go:119-121` 在缺 `product` 时 **302 回 `/admin/products`** → 从菜单点进去看不到目标页，**UI 里没有任何路径可达**，只有手拼 `?product=` 才行 | 实测导航被重定向。区分于 §5.3 IA-1（那是菜单**归属**问题，本条是**功能不可达**） |
| 19 | **P1** | `article_edit.html` | **7 张卡挤一页，页高 13.75 屏**（全站最高）：正文 / SEO 字段 / 实时预览 / SEO 评测 / 发布 / 可视化编辑 | 实测 `.card h2` 依次为上述 6 个 + 1 个未具名；`scrollHeight/innerHeight=13.75`。「发布」按钮在第 11 屏附近 |
| 20 | **P1** | `page_translations.html` | **8 张同构表并列，5.17 屏，首屏仅 4 行可译**；`hintReal=0.1751`（超阈值 2.65 倍，全站唯一真实超标项） | 实测 8 张表 thead **完全相同**（`字段\|原文\|译文\|状态\|来源`），共 44 行。需带 `?pageId=` 才能访问 |
| 21 | **P1** | `page_redirects.html:101` | **写操作伪装成筛选栏**：`<form method="post" action=".../create" class="filter-bar">` —— 回车即创建一条重定向，**无二次确认**，样式与筛选栏无异 | 把写操作放进 `.filter-bar` 是高风险语义错配。真实 URL 是 `/api/page/redirect`（不在 `/admin/` 下） |
| 22 | **P1** | `inventory_reasons.html:66-84` | **9 行 × 每行一个 `<td><form>`** + `checkAll=0`（本域唯一有写操作却无批量的列表页） | 实测 `tdForms=9`。**同域 10 处同类页面全部合规**（用 `form="id"` + 表格外隐藏表单），可直接横抄修正 |
| 23 | **P1** | 4 个字典页 | `product_attributes`(4 行) / `product_categories`(10 行) / `product_brands`(4 行) / `product_tags`(4 行) **无筛选、无分页** | 实测 `filterBar=0`。品类/标签增长后不可用 |
| 24 | **P1** | `product_bundle.html` | **290KB / 146 个表单字段名平铺**（`bundle/save` 逐成员行重复 9 个字段） | 本域最重页面。与 §6.1 的 menus/i18n 同模式 |
| 25 | **P1** | `i18n.html` | **两个同名 `lang` 下拉**：一个切换界面语言（选项 `English/简体中文`）、一个筛选词条语言（选项 `全部语言/zh-CN/en-US`），选项集不同 | 用户分不清哪个改界面、哪个筛数据 |
| 26 | **P1** | `media.html` | 补充 §5.2 #6：搜索框是**裸 `<input class="media-search">`**（不在 `.filter-bar` 里），上传/批量下载/视图切换散在 `.media-toolbar`。**另有 24 个行级勾选但无全选框、无 `.bulk-bar`、批量动作只有「下载」没有「删除」** | `media-admin.js:145/183` 动态生成 `.media-card-check`；`admin.js` 的成熟批量机制未被接入 |
| 27 | **P1** | `product_tags.html` | 一页并存两个不同实体的列表：卡1「标签列表」+ 卡2「命中的商品」（首屏是**空壳卡**，仅标题 + 一行提示等 htmx 填充） | 判据 §7.1「多视图用 tabs 不要并列两块」 |
| 28 | **P2** | `product_pricing.html:68/77` | **`.hint` 被用作字段标签**（「范围」「筛选集」），导致 `hintReal=0.1289`（超 1.95 倍）—— 标签是控件的一部分，不该计入说明占比 | 另有 `:158/161/186/187/189` 折叠区内 5 处 |
| 29 | **P2** | `inventory.html:122` / `inventory_warehouses.html:53` / `inventory_reasons.html:44` | **`.filter-bar` 承载两种语义**：既做筛选栏，又做标题行（`filterFields=0` 但类名是 `filter-bar`） | 后续按 `.filter-bar` 批量处理样式会误伤标题行 |
| 30 | **P2** | `product_categories.html:104` | **层级数据用平铺表格**，父子关系只靠 `Label` 里的缩进字符串；无父级列、无展开折叠、无树控件 | `02-K` §3 要求层级数据用树选择/树表。项目已有递归 block 先例 `partials/nav-nodes.html` |

### 5.2.4 第三轮（管理/站点/系统域）新增

| # | 级别 | 位置 | 问题 | 证据 |
|---|---|---|---|---|
| 31 | **P0** | `admin_pages_handle.go:896` | **裸出 i18n key `MsgFieldRequired`**（不是裸中文，是**未翻译的内部标识符**），页面 DOM 仅 5 节点、无 `<title>`、无页壳 | 源码确认；常量定义在同文件 `:45`。**比裸中文更差** —— 用户看到内部 key，既不知哪里错、也无返回入口 |
| 32 | **P0** | `theme_settings_admin_pages.go:148/153/278/283`、`theme_admin_pages.go:145/243` | **裸文本脱离页壳 6 处**：`c.String(400, "缺少主题 id")` / `"主题不存在"` — DOM 5 节点、`title=""`、不加载任何资源 | 逐行核实准确。**全站最严重的脱离页壳案例** |
| 33 | **P0** | `admin_pages_handle.go:285-287` | **`role_permissions` 唯一静默失败**：缺 `role_id` 时 302 回 `/admin/roles`，**无 `err=`、无任何提示** | 实测裸访问落到角色列表，页面正常、无徽章 |
| 34 | **P0** | `administrators.html:46` / `departments.html:38` / `mail_marketing.html:98,161` | **`<table>` 被包在 `{{if len(...)==0}}…{{else}}` 内** → 空数据时**整个表头不渲染**，用户看不到有哪些列 | 实测 `tables=0`。**4 个案例** |
| 35 | **P1** | 11 页 / 19 处 | **空态无 `.empty-actions`**：`administrators`、`departments`、`roles`、`permissions`、`datarules`、`menus`、`seo`、`analytics`(6 处)、`role_permissions`、`mail_automation_run`、`mail_campaign` | 覆盖面**远超原估的 3 页**。正确样本：`dashboard.html:67`、`plugins.html:29`、`mail.html` |
| 36 | **P1** | `role_permissions.html` | 页高 **4.15 屏 / 962 节点**（超「单职能 ≤1 屏」），逐节点平铺仅靠客户端折叠 | 勾一个深层按钮要滚 3 屏 |
| 37 | **P1** | `plugins.html` | **4 张只读巡检表占主导**（`:76/102/111/120`），把「安装插件」入口推到第 5 屏 | §A2 同型（`blocks` 的「待重建影响面」）。**不适用 tabs**（列结构不同），应**折叠** |
| 38 | **P1** | `site_slots.html:107` | **每行内嵌一个含 13 个 option 的 `<select>`**，10 行 → 130 个 option 节点（67.6KB / 仅 10 行数据）。且下拉占位符塞了口径说明「（选择页面：显示的是页面草稿路径）」逐行重复 | 违反 §C3「单元格是值不是句子」的精神 |
| 39 | **P2** | `masterdata_changes.html:137` | 单元格拼句子：`商品 · Alibarbar SWIRL 13000 630e11b2-…`（类型+名称+UUID 同格，UUID 撑宽列到 60+） | 应拆列或 UUID 移进 `title` |
| 40 | **P2** | `settings.html:198-200` | 下拉选项含**实现术语**：`default_plain` / `all_prefix` / `off` 直接显示在中文描述前 | 违反 §4「术语与用户语言一致」 |
| 41 | **P2** | `mail_automation_edit.html:86` | 12 个同构 `node_type_N` 下拉 × 6 选项 = **72 个 option** 重复渲染 | 可改共享 `<datalist>` 或 Segmented |
| 42 | **P2** | `mail_marketing:93` / `mail_automation:136` / `mail_automation_run:62` | 空态 `.empty-desc` **以 `.empty-title` 原句开头**（输出同一句话两遍） | 与 #12 同源 |
| 43 | **P2** | `mail_campaign` / `mail_automation_run` / `mail_automation_canvas` | **归口文案档位不一致**：canvas 给「自动化流程不存在」，run/campaign 给「参数不合法」——同一个 `mailErrPageText` 出口给出不同粒度 | 且三者都**无 `id==0` 前置判定**，靠下游查询失败兜底 |

### 5.2.5 模式 A 全量核验（缺参/异常时用户无出路，10 处）

| 页面 | 触发 | 现状 | 分档 | 源码 |
|---|---|---|---|---|
| `theme_settings` | `?id` 缺失 | 裸文本「缺少主题 id」，5 节点/无页壳 | **最差** | `theme_settings_admin_pages.go:148` |
| `theme_settings` | `?id` 不存在 | 裸文本「主题不存在」 | **最差** | `:153` |
| `theme_settings`（POST 侧） | 缺 id / 不存在 | 裸文本 × 2 | **最差** | `:278`、`:283` |
| `datarule_edit` | `?id` 缺失 | **裸 i18n key `MsgFieldRequired`** | **最差** | `admin_pages_handle.go:896` |
| `datarule_edit` | `?id` 不存在 | 裸文本「数据规则不存在」 | **最差** | `:901` |
| `theme`（POST 侧） | 缺 id | 裸文本「缺少主题 id」× 2 | **最差** | `theme_admin_pages.go:145`、`:243` |
| `role_permissions` | `?role_id` 缺失 | **静默 302**，无 `err=` | **最差（静默）** | `admin_pages_handle.go:285-287` |
| `mail_campaign` | `?id` 缺失 | 302 + `?err=参数不合法`（文案与原因错位） | 中（有壳无出路） | `mail_marketing_page_handle.go:174-181` |
| `mail_automation_run` | `?id` 缺失 | 302 + `?err=参数不合法` | 中 | `mail_automation_page.go:313-316` |
| `mail_automation_canvas` | `?id` 缺失/不存在 | 302 + `?err=自动化流程不存在` | 中 | `mail_automation_canvas.go:33-37` |

**统一修法**（出口已现成）：
- **最差档 7 处** → `shell.PageErrorBadRequest(c, "<scene>", err)`（`internal/web/shell/errors.go:45` 已有，`inventory_page_handle.go:81` 等在用），或 303 回列表页带可读 `?err=`；
- **静默档 1 处** → 改 302 + `?err=`（该页 `RolesPage` 已渲染 `Err` 字段，**零改动成本**）；
- **中档 3 处** → 补 `id == 0` 前置判定 + 按「缺少 / 不存在 / 无权限」三档给文案。

> **判据扩展**（应写入 `02-J` §A）：模式 A 除「`c.String` 直出」与「`c.Redirect(...?err=)` 回带内部错误」，
> 还包括「**无前置 `id==0` 判定、靠下游查询失败兜底**」这一形态（mail 三页都是）。

### 5.2.2 交叉验证纠正（父 agent 的判断被实测推翻）

| 原判断 | 实测结论 | 教训 |
|---|---|---|
| `blocks` 三段表格全选框「段间漏选」 | **不成立** —— 三段同处一个 `<form>`，`admin.js:522` 按 `closest('form')` 取作用域，全选正常 | 从源码结构推断运行时行为**必须用浏览器实测复核**（`AGENTS.md` 已记录过同类教训） |
| `products_new` 「缺分类/品牌/标签/属性入口」 | **不成立** —— `products_new.html:21` 用 `{{include "partials/product_create_form.html"}}` 引入 148 行共享表单（注释明确「两处各抄一份必然分叉」） | 统计字段必须**跟进 include 的片段** |
| `product_edit` 「三段只有标题没有表单」 | **不成立** —— 三段全在 `/admin/products/update` 表单内（60 行开、197 行闭） | 统计字段必须**解析 form 边界**，不能只看 action 列表 |

> 三条误判的共同根因：**用静态文本统计代替结构解析**。已写入 `02-J` §H 作为方法论约束。

### 5.2.3 值得推广的正面样本（建议写入规范）

| 样本 | 位置 | 为什么值得推广 |
|---|---|---|
| **disabled 主行动 + `title` 说明前置条件** | `inventory_purchases.html:33` | 实测「＋ 新建采购单」`disabled` + `title="这个工程还没有启用中的货源 —— 先去货源管理建一个…"`，空态另给 `.empty-actions` 指向修复页。**全站最好的「条件未满足」反馈** |
| **空态三段式 + 主行动** | `coupons.html:106-109` | 交易域唯一空态带主行动的页面，其余三页（orders/returns/customers）应向它对齐 |
| **带计数的可点击徽章筛选** | `customers.html:67` | `✓ 全部 0 / 正常 0 / 已停用 0 …` 是可点击链接带 `aria-current`，符合 §2「信息与操作合一」 |
| **域内最正确的列表模式** | `products.html` | `data-check-item` + 表格外隐藏表单：43 forms / **0 tdForms** / **0 每行模板** —— 同域 `product_attributes`(9 模板 572 节点) / `product_categories`(11 模板 383 节点) 可直接横抄 |
| **内容域骨架最合规** | `content_templates.html` | 有 `.filter-bar`、`checkAll`、`.col-actions`、首屏 5/5 行、`hintReal=0` |

### 5.3 信息架构缺口（待你确认，不擅自动）

| # | 现象 | 判据（`admin-ui-logic` §1） |
|---|---|---|
| IA-1 | 「商品详情模板」挂在**内容**目录，实体属商品域 | 菜单层级应反映模块边界 |
| IA-2 | 「变更记录」（`masterdata_changes`）挂在**商品与库存**下，但主数据审计是全局能力 | 目录归属与实体边界不符 |
| IA-3 | 「退货入库」在**交易**目录，但页面是「审核 → 入库 → 退款」的流程页 | 名称与职能不符（页面含退款，不止入库） |
| IA-4 | **邮件 4 项**（邮箱管理/邮件营销/邮件活动/邮件自动化）占**系统**目录近半 | 邮件可收拢为独立一级，或合并为「邮件」+ 子页 |
| IA-5 | 「库存」与「商品与库存」两个一级目录并列 | 二者是同一领域的上下层，拆两个一级目录值得商榷（`02-H` §7 已定性为独立一级） |

> IA 类问题涉及 `sys_menus` 数据改动，影响全站导航 —— **逐条确认后再动**。

## 6. 改造优先级建议

**前两项已完成**（本轮）：

| # | 项 | 状态 |
|---|---|---|
| 1 | **P0 #1** 库存空态文案 | ✅ 已修（迁移 317 + 注册） |
| 2 | **P0 #2** `/admin/articles/new` 403 | ✅ 已修（`CasbinMiddlewareForPathAs` + 调用点声明 POST） |

**待办**（按「用户受伤程度 ÷ 改造成本」排序）：

| 序 | 项 | 依据 | 成本 |
|---|---|---|---|
| 3 | **P0 #3** 5 处 `c.String` 直出改 `shell.PageError` | 脱离页壳的死胡同 + 违反项目硬约束 | 低（纯 handler 分支） |
| 4 | **P1 #11** `product_detail_template` 死入口（缺参静默 302） | 与 #3 同族，一并收口 | 低 |
| 5 | **P1 #6** `media` 套骨架 + 补批量选择 | 全站唯一未套骨架；无批量是实打实的效率损失 | 低 |
| 6 | **P1 #5** 删死模板 `tpl-product-create` | 删一行 + 验证，纯收益 | 极低 |
| 7 | **P1 #8** 7 个列表页补 `.filter-bar` | 判据明确、模板同构（抄 `content_templates.html:48`） | 低~中 |
| 8 | **P2 #12** i18n 库值脱节（2 条词条 + **加回归测试**） | 第二次出现，不加测试会有第三次 | 低（词条）+ 中（测试） |
| 9 | **P1 #4** menus/i18n 抽屉单份化（需先定架构，见 §6.1） | 收益最大（490KB → 预计 ~50KB） | 高 |
| 10 | **P1 #7** blocks 三段表格合并（消除漏选） | 需同步改 `admin.js` 的 scope 查询 | 中 |
| 11 | **P1 #9** analytics / masterdata tabs 收口 | 复用已有 `.tabs` 组件 | 中 |
| 12 | **P1 #10** `product_brands` SEO textarea 对齐 | 同域已有正确实现，照抄即可 | 低 |
| 13 | **P2 #13~#16** 状态本地化 / 行内说明 / 主行动重复 | 打磨 | 低 |

### 6.1 待决策：每行一份抽屉模板（menus / i18n）怎么瘦身

**现状机制**（`ui/drawer.js:44`）：抽屉打开时 `body.appendChild(tpl.content.cloneNode(true))` ——
**克隆页面里已存在的 `<template>`**。所以「每行一份模板」是当前架构的必然结果，
不是谁写错了。代价实测：menus 490KB / 4603 节点，i18n 258KB / 1326 节点。

| 方案 | 做法 | 代价 | 收益 |
|---|---|---|---|
| **A. 按需取片段**（推荐） | 行内按钮改 `hx-get="/admin/menus/edit-form?id=X"`，抽屉打开时拉取 | 需新增 handler + 路由 + 权限点；一次网络往返（本地 <10ms） | 页面体积降 90%+；抽屉内容永远是最新的 |
| B. 保留模板但精简 | 只保留必需字段，其余进抽屉内二级 | 不解决根本（仍随行数线性增长） | 小 |
| C. 不改 | 接受大页面 | 0 | 0 |

**项目内已有先例**：`product_tags.html:123` 的「命中数按需展开」（注释标注审计 PERF-02），
以及 `_fragments/bundleConfigurator`（`product_bundle.html:268`）。

**方案 A 的注意点**（改造时必查）：
- 抽屉的 `openDrawer` 只在**打开瞬间**克隆模板 —— 改 htmx 后需确认
  `htmx.process` + `WBUI.scan` 仍在内容插入后执行（`drawer.js` 已有这段，别绕过它）；
- 新增 `GET /admin/menus/edit-form` 是为页面服务的**内部片段端点**，
  是否需挂 `authorizedAPI` + Casbin 需按 `AGENTS.md`「新增挂在 authorizedAPI 下的接口」核实；
- i18n 页同理，且它与「文案词条是真相来源」的语义相关，改动要更谨慎。

> **此项需你确认后再动** —— 它改的是全站抽屉的加载模式，影响面超出单页。

## 7. 逐页改造的固定流程

每个页面按此顺序（`admin-ui-logic` §9 的落地版）：

1. **度量现状**：`screens` / `hintRatio` / 行数 / 首屏可见行 / 表格顶边 / 横向溢出；
2. **读源码结构**：`grep -nE 'page-head|filter-bar|data-check-all|col-actions|section-fold|class="hint'`;
3. **对账判据**：`admin-ui-logic` §2.1 / §2.2 / §7 + `02-H` §5 验收表；
4. **一次一页改造**，改完立即截图核对（视觉检查是技能的一部分，不是可选项）；
5. **跑测试**（模板完整性测试、i18n 测试、路由快照）；
6. **改断言**：期望的标记随功能移走 → 改断言并注明；标记无故消失 → 实现有 bug；
7. **更新本文 §5 度量表**。

## 8. 硬性约束（改造时必须遵守）

- **页面正文零说明文字**：解释进 `.help` 悬浮（`02-H` §2.1）；
- **主行动与筛选同行**：`.page-actions`，不单独占卡（§2.2）；
- **描述类字段不用单行 input**；**SEO/meta 字段不用富文本**（`admin-ui-logic` §8）；
- **颜色走 `--sky-c-*`**，禁止写死十六进制；
- **所有 POST 带 CSRF**；HTMX 经 body 继承，原生表单显式加隐藏域；
- **新增挂 `authorizedAPI` 的接口**必须在 `internal/permission/codes.go` 加常量 +
  在路由注册处声明（漏写是编译错误），跑 `scripts/check-permission-gaps.sh`；
- **新增传 `Msg*Title` 的页面必须同批 seed 词条**，否则顶栏露出 key（`02-H` §4）；
- **改完更新本文 §5 度量表与 `02-H` §6 进度**。
