# 组件基座改造方案（对齐 Ant Design 交互契约，零前端框架）

> 输入基准：`02-K-antd-component-reference.md`（antd 可迁移契约 + §7 优先级表）、
> `02-I-admin-model.md`（设计令牌 §2 / 控件库 §3）、`02-H-admin-page-shell.md`（页壳骨架）、
> `AGENTS.md`（「交互方式（HTMX）」「所有组件必须适配多端」两节为硬约束）。
>
> **本文只做调研与设计，不改任何文件。** 所有「现状」均为实际读码结果，含文件:行号。
> 影响面计数为 `grep` 实测值（口径写在每项里，可复现）。

---

## 0. 执行摘要

三个结论决定整份方案的形态：

**① 错误态的 CSS 已经在了，缺的是生产它的链路。** `ui.css:622-635` 已定义
`.form-input[aria-invalid="true"]` 的边框态与 `.form-error` 的提示态，`ui_script.go:44`
也把 `form-error` 列进了产物基座类清单 —— 但 `grep -rn 'aria-invalid\|form-error' internal/`
在 `.html` / `.go` 里**零命中**。这意味着「字段级错误提示」不是「要新造一套样式」，
而是**打通服务端 → 模板 → 控件的最后一公里**，成本比 `02-K` §7 估的「中」更低。

**② `WBUI.select` 的菜单其实是「伪分组」—— 忽略了 `optgroup`。** `select.js:112-126` 的
`rebuild()` 遍历 `sel.options` 时**完全丢弃 group 语义**：`available()`（第 107-111 行）
会读 `opt.parentElement` 判断 optgroup 的 disabled/hidden，但渲染出的 `<li>` 是平铺的。
全站只有 2 处 `optgroup`，而 `blocks.html:104-119` 的 16 项类型下拉是硬编码平铺 ——
`02-K` §3 说的「需要分组」目前既没有数据侧的分组，也没有渲染侧的分组。这是**同一个缺口的两面**。

**③ HTMX 的按钮 loading 已经全局接好了，`02-K` §4 的「未被普遍使用」表述需要修正。**
`admin.js:224-280` 的 `initHtmxFeedback()` 已经对 `htmx:beforeRequest` 派发的 `elt`
自动调 `WBUI.busy(btn)` 并回退到 `classList.add('is-busy')`，`htmx:afterRequest` 统一收尾。
25 处 `hx-*` 属性**全部**被覆盖，页面上**不需要**再逐个写 `hx-indicator`。
真正的缺口是：本地 `<form method="post">`（非 htmx，31 处 `?err=` 回带的 POST 走的就是它）
请求期间毫无反馈，用户在整页跳转前看不到任何提示。

**④ 调研中发现一处存量缺陷：`--ui-sp-xxl` 被引用但全仓无定义。**
`theme.css:982`（`.empty-state` 的 `padding`）与 `theme.css:994`（h2 的 `margin-top`）
都在用 `var(--ui-sp-xxl)` **且未写兜底**，而 `ui.css:290-297` 的 `:root` 兜底段只到
`--ui-sp-xl`（`grep -rn -- '--ui-sp-xxl:' internal/templates/static/css/` 零命中）。
按 CSS 规范，无兜底的未定义 `var()` 会让**整条声明**在 computed-value 阶段失效 ——
即 `.empty-state` 当前很可能没有任何内边距。这条与 C.4 直接相关，
修法与门禁建议见 C.4 / §D.3。

据此，`02-K` §7 的优先级需要**调整顺序**（理由见 §D.1）。

---

## A. 现状盘点

### A.1 控件清单（`static/js/ui/`）

| 控件 | 文件 | 行数 | 模板侧契约 | 已实现的能力 | 缺失的能力（对照 `02-K` §3/§4） |
|---|---|---|---|---|---|
| 自绘下拉 | `select.js` | 300 | `<select>` 渐进增强；`data-wb-native` 跳过 | combobox/listbox ARIA、键盘（Enter/Space/↑↓/Home/End/Esc/字母前缀跳转）、MutationObserver 重建（动态改 option 同步）、`data-ui-key` 展开态跨 morph 恢复、label[for] 焦点转交 | **optgroup 分组渲染**（第 112-126 行丢弃 group）、**搜索过滤**、**虚拟滚动**（长列表无上限） |
| 抽屉 | `drawer.js` | 134 | `data-drawer-open="#tpl-x"` + `data-drawer-title`；`<template>` 克隆 | `<template>` 克隆 + `htmx.process` 认领 hx-* + `WBUI.scan` 增强、焦点陷阱（Tab 循环）、`inert` 背景、焦点回归触发者、`wbui:drawer-open/close` 事件 | **头部 `extra` 提交按钮槽**（提交按钮在 `.form-actions` 表单底部，`drawer.js:49` 只写 title）、**抽屉宽度按内容自适应**、**未保存改动提示** |
| 模态弹窗 | `modal.js` | 146 | `.wb-modal` + `<dialog>`；`data-modal-open/-close/-static/-nokeyboard/-autofocus` | `showModal()` 原生模态、遮罩点击关闭、显式接管 Esc（不信浏览器 default action，`modal.js:73-83` 注释记了 CDP 下原生 Esc 不关）、焦点回归、`wbui:modal-open/close` 事件 | **内容懒加载**、**栈式多层**（同开两个只按最近处理） |
| 确认框 | `confirm.js` | 163 | `data-confirm` / `-title` / `-ok` / `-cancel` / `-danger`；`<form>` 与 `<a>` 都支持 | `<dialog role="alertdialog">`、`data-confirm-bypass` 防二次确认、`WBUI.confirm` / `WBUI.alert` API、`is-alert` 单按钮形态 | **不需要改**（已满足契约） |
| 轻提示 | `toast.js` | 95 | `WBUI.toast(msg, {type,duration,dismissible})` | `role="status"`/`role="alert"`、同屏 MAX=4 挤掉最早、transitionTime 读实际时长而非硬编码 | **操作成功后的 `?ok=` 回带**（现为页顶 badge，未走 toast） |
| 按钮忙碌态 | `busy.js` | 64 | `WBUI.busy(btn[, promise][, {label}])` | 记住原 `disabled` 状态（不一律启用）、成功失败都恢复、同按钮重入复用收尾函数、`is-busy` + `aria-busy` | **声明式用法**（`data-busy`）、**非 htmx 表单提交的自动接入**（见 §B.6） |
| 媒体字段 | `mediafield.js` | 205 | `data-media-field` + `data-media-input/-img/-tip/-pick/-clear` | input 唯一真值、预览只认可解析地址、弹窗单例挂 body、`/api/media/list` 直连（不依赖 media-lib.js） | 弹窗未走 `WBUI.modal` 的 `<dialog>`（自建 `.media-pick-mask` div，`mediafield.js:78-99`），**缺焦点陷阱** |
| 颜色字段 | `colorfield.js` | 116 | `input[data-color-field]` | 空值即「未设置」、色块 + 输入框左侧色带双呼应、`colorizable` 只认能显示的值、`setTimeout(sync,0)` 补回填竞态 | **不需要改** |
| 图标字段 | `iconfield.js` | 87 | `data-icon-field` + `input[name="icon"]`；列表 `data-icon-name` | 图标库 766KB 懒加载（三态状态机）、随 `WBUI.scan` 渲染（htmx 替换后不再永远为空）、未知图标退回标记 | **不需要改** |
| 主题切换 | `themetoggle.js` | 44 | `data-theme-toggle` | `aria-pressed`、防闪烁脚本刻意留在 `<head>`（`themetoggle.js:8-10` 记了理由） | **不需要改** |
| 基座入口 | `index.js` | 21 | 无（脚本入口） | `WBUI.ready` 扫描、`htmx:afterSwap` 重扫 | **不需要改** |

> 另有 `_util.js`（76 行）提供 `ready/each/$$/markOnce/register/transitionTime/scan`，
> 与 `htmx.min.js`（带 LICENSE / VERSION 的本地 vendor）。
> 加载顺序由 `partials/ui_scripts.html` 唯一持有（`ui_assets_test.go:44-70` 钉住「htmx → 助手 → 控件 → index.js」）。

### A.2 现状的关键机制

**`WBUI.scan` 是唯一扫描入口**（`_util.js:62-75`）：遍历 `WBUI.controls` 注册的初始化函数，
逐个 try/catch，失败打 `console.error('[WBUI] 控件初始化失败', e)` 并派发 `wbui:error` 事件。
`index.js:13-20` 在 DOM 就绪与 `htmx:afterSwap` 两个时机调用它。

**`markOnce` 防重复增强**（`_util.js:35-39`）：写 `el.dataset['wbui'+flag] = '1'`。
但 `select.js:56-59` 刻意**不用**它 —— 状态存在 `WeakMap` 里，因为 data 标记会随
morph/clone 复制、而监听器不会，复制出来的标记会让新节点跳过增强。这是新控件必须遵守的先例。

**HTMX 全局反馈已在 `admin.js`**（非 `static/js/ui/`）：
`initHtmxFeedback()`（`admin.js:224-321`）做三件事 ——
① `[data-hx-progress]` 进度条由 inflight 计数驱动；
② 触发按钮自动 `WBUI.busy`；
③ `responseError`/`sendError`/`timeout` 一律 toast。
`settled` WeakSet 按 xhr 去重，因为 htmx 会对已移出文档的元素补派发一次 `afterRequest`。

---

## B. 逐组件差距分析

### B.1 字段级错误提示（`02-K` §7 第 1 位）

**antd 契约**（`02-K` §2.2）：错误紧贴控件下方、红字、**预留高度不改变布局**、
控件边框转红；校验时机是「失焦 + 提交时全量」。

**本项目现状**：页顶统一 `?err=` 回带。实测：
`grep -rn 'badge badge-warning" role="alert"' internal/templates/admin/*.html` →
**60 处 / 50 个文件**。文案前缀各页自拟（`"上一次操作未完成："` / `"提示："` / `"概览数据暂时读不到："` …），
模板形态一致（`administrators.html:28`、`departments.html:26` 等）。

服务端侧：`grep -rln '&err=' internal/**/*.go` → **10 个文件 / 31 处**
（`product_tag_page.go` 单文件 8 处、`product_taxonomy_page.go` 6 处、
`product_attribute_page.go` 5 处、`inventory_source_page_handle.go` 2 处、
`contenttemplate` 2 文件、`mail_automation_page.go`、`product_pricing_page.go`、
`product_bundle_page.go`、`product_detail_template_page.go`）。

**CSS 已就绪但无人使用**（这是本方案最重要的发现）：

```css
/* ui.css:622-635 —— 已存在 */
.form-input[aria-invalid="true"],
.form-select[aria-invalid="true"],
.form-textarea[aria-invalid="true"] { border-color: var(--sky-c-danger, #cf8080); }
.form-input[aria-invalid="true"]:focus { box-shadow: 0 0 0 3px rgb(179 74 74 / 15%); }
.form-error { margin-top: var(--ui-sp-xs); font-size: 12px; color: var(--sky-c-danger, #cf8080); }
```

`grep -rn 'aria-invalid' internal/ --include=*.html --include=*.go` → **0 命中**（只命中 CSS 自身）。
`form-error` 同理 —— 仅在 `ui_script.go:44` 的产物类清单里出现。

**结论**：差距不在 CSS，而在**「哪条错误属于哪个字段」这条信息在链路上丢失了**。
现在 `?err=` 只带一句话，没有任何字段定位。

### B.2 日期范围控件（§7 第 2 位）

**antd 契约**（`02-K` §3）：范围应是一个控件（`RangePicker`），共享一个弹层、
共用一套「起止」语义与一个「最近 N 天」快捷区。

**现状**：8 处 `type="date"`、4 个文件，全部是「相邻两个独立 input」：

| 文件:行 | 字段对 |
|---|---|
| `inventory.html:107,111` | `timeFrom` / `timeTo` |
| `analytics.html:61,65` | `from` / `to` |
| `customers.html:88,90` | `registeredFrom` / `registeredTo` |
| `masterdata_changes.html:95,97` | `since` / `until` |

四个页面的 HTML 形态完全一致（`<input type="date" class="form-input" id="x" name="y" value="{{z}}">`），
且都在 `.filter-field` 里（`02-H` §7.2 的筛选形态）。

**关键约束**：这四个页面**没有任何 htmx**（全站 `hx-*` 仅 25 处，集中在这 4 个页面之外），
筛选是**原生 GET 表单提交**。所以范围控件的两个值必须仍然是两个成功的表单字段，
控件只能做「联动 + 快捷 + 校验」，不能改变提交形状。

### B.3 可搜索 / 可分组下拉

**antd 契约**（`02-K` §3）：>10 项要支持搜索；层级/类别数据要分组。
超过 ~7 项用 `Select`，≤7 项互斥用 `Segmented`。

**实测规模**：`<select>` **169 处 / 52 个文件**；
`optgroup` **仅 2 处**；`<option>` 行数最多的页面：
`blocks.html`(19)、`inventory_purchases.html`(17)、`settings.html`(16)、
`menus.html`(16)、`inventory_sources.html`(16)、`navigations.html`(15)、
`mail_marketing.html`(15)、`inventory.html`(14)。

**`blocks.html:104-119` 的 16 项类型下拉**（用户显式点名的目标）：

```html
<option value="block">{{ .["t"]("admin.blocks.kind.block", "区块") }}</option>
<option value="header">{{ .["t"]("admin.blocks.kind.header", "页眉") }}</option>
…
<option value="snippet">{{ .["t"]("admin.blocks.kind.snippet", "片段模板") }}</option>
```

16 项**没有语义分组**，且 **`select.js` 的菜单渲染完全忽略 `optgroup`**：

```js
// select.js:112-126 —— rebuild() 遍历 sel.options，optgroup 语义在此丢失
WBUI.each(sel.options, function (opt, i) {
    var li = document.createElement('li');
    li.className = 'wbs-option';
    li.setAttribute('role', 'option');
    li.dataset.index = String(i);
    li.textContent = opt.label;          // ← 只有标签，没有分组容器
    …
});
```

`available()`（第 107-111 行）**读了** `opt.parentElement` 判断 optgroup 的 disabled/hidden，
说明作者意识到 optgroup 存在，但输出侧没有体现。这是**渲染侧的分组缺口**。

数据侧同样缺：16 项类型的语义分组（结构类 / 导航类 / 内容类 / 布局类）需要先定，
`option` 上的 `data-group` 属性是落点（照抄 `menus.html:106` 的
`<option value="{{p.ID}}" data-type="{{p.Type}}">` 先例 —— 那里已经用 data 属性带业务元信息）。

**搜索**：`select.js:209-217` 已有**字母前缀跳转**（`indexOf(ch) === 0`），
但那不是搜索 —— 输入「页」找不到「页脚」以外的项，输入两个字符直接失效。

### B.4 表格空态保留表头（§7 第 4 位）

**antd 契约**（`02-K` §1）：`Empty` **内嵌在表格里**，表头始终保留 ——
「表头是认知锚点，删了用户不知道这列表有什么列」。

**现状**：`.empty-state` 共 **84 处 / 51 个文件**；`class="data-table"` **77 处**。
空态**取代表格整体**的模式（`{{if len(.Rows) == 0}}` → `.empty-state` → `{{else}}` → `<form><table>`）
命中 **10 处**：

```
administrators.html:45 / datarules.html:43 / departments.html:38 / menus.html:38
masterdata_changes.html:116 / products.html:94（len(.Products)）/ blocks.html:154,188
```

以 `administrators.html:45-60` 为例：

```html
{{if len(.Rows) == 0}}
<div class="empty-state">
    <p class="empty-title">{{ .["t"]("admin.admins.empty", "还没有管理员，点右上「新建管理员」创建一个。") }}</p>
</div>
{{else}}
<form method="post" action="/admin/administrators/bulk-delete">
…
<div class="table-wrap table-scroll" tabindex="0" role="region">
    <table class="data-table">
        <thead><tr><th class="col-check">…</th>…</tr></thead>
```

**`<thead>` 在 `{{else}}` 分支里** —— 空数据时整张表连同表头一起消失。

### B.5 抽屉头部固定提交按钮（§7 第 5 位）

**antd 契约**（`02-K` §2.3）：`Drawer` 的 `extra={<Space>取消 / 提交</Space>}` ——
按钮在**头部右侧**，表单滚动时始终可见。

**现状**：抽屉外壳在 `layout.html:111-119`（全局唯一，页面只提供 `<template>`）：

```html
<aside class="drawer" data-drawer hidden aria-hidden="true">
    <header class="drawer-head">
        <span class="drawer-title" data-drawer-title></span>
        <button type="button" class="drawer-close" data-drawer-close>✕</button>
    </header>
    <div class="drawer-body" data-drawer-body></div>
</aside>
```

`drawer.js:49` 只写 title：`if (titleEl) { titleEl.textContent = titleText || ''; }` —— **没有 extra 槽**。

提交按钮全在表单底部 `.form-actions`：**12 处**（`datarules` / `departments` /
`administrators` / `roles` / `menus` / `permissions` 各 2 处）。

规模：
- `data-drawer-open` **75 处 / 27 个文件**；
- `<template id="tpl-…">` **52 个**；
- 字段最多的抽屉表单：`mail.html`(5 模板/44 标签)、`inventory_warehouses.html`(24 标签)、
  `product_tags.html`(22)、`product_categories.html`(20)、`inventory_sources.html`(20)、
  `mail_marketing.html`(20)。

`.drawer-body { flex:1; overflow-y:auto; padding: var(--ui-sp-xl,24px) }`（`ui.css:197`）——
抽屉是可滚动的，字段多的表单滚到底部才能看到提交按钮，这正是 antd 用头部 `extra` 要解决的问题。

### B.6 HTMX 请求期间按钮 loading（§7 第 6 位）

**`02-K` §4 的表述需要修正**：原文说 `WBUI.busy`「**未被普遍使用**」。实测不然 ——

`admin.js:224-280` 的 `initHtmxFeedback()` 在 `htmx:beforeRequest` 时调 `markBusy(e.detail.elt)`：

```js
// admin.js:239-253
function markBusy(elt) {
    if (!elt || elt.nodeType !== 1 || pending.has(elt)) return;
    var btn = elt;
    if (btn.tagName !== 'BUTTON' && !(btn.tagName === 'INPUT' && btn.type === 'submit')) {
        btn = btn.querySelector('button[type="submit"], input[type="submit"], button');
    }
    if (!btn) return;
    var finish;
    if (window.WBUI && WBUI.busy) { finish = WBUI.busy(btn); }
    else { btn.classList.add('is-busy'); … }
    pending.set(elt, finish);
}
```

**25 处 `hx-*` 全部自动覆盖**，模板侧**不需要**写任何 `hx-indicator`（全站仅 1 处
`hx-indicator` 命中，且是注释）。

`WBUI.busy` 的直接调用点只有 3 处（`workbench/core.js:50`、`media-admin.js:43`、
`admin.js:246`），但这**不代表没被使用** —— 主路径是 htmx 事件自动接入。

**真正缺失的**：`grep -c 'hx-'` 显示全站只有 25 处 htmx 属性，而业务写操作
（删除/保存/批量）绝大多数走**原生 `<form method="post">`**（31 处 `?err=` 回带来自这条路）。
原生表单提交期间：没有进度条、没有按钮 loading、**整页白屏直到服务端返回**。
这是 §4 说的「请求期间无反馈」在**主路径**上的真实缺口。

### B.7 树选择（§7 第 7 位）

**antd 契约**（`02-K` §3）：`TreeSelect` —— 层级数据（分类、部门、菜单父级）用树选择，
展开层级 + 缩进 + 排除自身及其后代。

**现状**：5 处平铺 `<select>`，全部用前缀缩进模拟层级：

| 文件:行 | 字段 | 层级表达方式 |
|---|---|---|
| `product_categories.html:142` | `parentId` | `{{o.Label}}`（服务端已拼缩进） |
| `product_categories.html:189` | `parentId` | 同上（第二个表单） |
| `departments.html:105` | `parent_id` | `{{p.Indent}}{{p.Title}}` 类 |
| `menus.html:104` | `parent_id` | **`{{p.Indent}}{{p.Title}}`**（明确用 Indent 前缀） |
| `navigations.html:85` | `parentId` | 同 departments |

`menus.html:100-110` 是最完整的样本：

```html
<div class="form-group">
    <label class="form-label">{{ .["t"]("admin.menus.field.parent", "上级菜单") }}</label>
    <select name="parent_id" class="form-select">
        <option value="0">{{ .["t"]("admin.menus.parent_root", "（根菜单）") }}</option>
        {{range _, p := .Parents}}
        <option value="{{p.ID}}" data-type="{{p.Type}}">{{p.Indent}}{{p.Title}}</option>
        {{end}}
    </select>
</div>
```

注意 `data-type="{{p.Type}}"` —— 页面另外挂了逻辑按类型过滤候选（`select.js:102-104`
的注释明确提到「后台『上级菜单候选过滤』就按类型逐项 `setAttribute('hidden'/'disabled')`」）。
**树选择改造必须保留这条链路**，否则会打断已有的过滤功能。

`product_categories.html:142` 的树还有个额外约束：`{{if o.ID != row.ID}}` ——
**行内表单必须排除自身**（否则能把自己设成自己的父级）。改造后这条逻辑要沉降到控件或
数据侧，不能让每个调用点各写一遍。

---

## C. 改造方案

> **共同约束（每项都适用，不再重复）**：
> - **不引入任何前端框架运行时**；新能力一律进 `static/js/ui/`，且必须登进
>   `WBUI.controls`（HTMX 局部替换后由 `WBUI.scan` 重扫，见 `_util.js:62-75`）。
> - **渐进增强**：原生控件必须留在 DOM 里（视觉隐藏），`name` / `value` / `label[for]` /
>   表单提交全部照旧 —— 这是 `WBUI.select` 的既有契约（`select.js:7-13`），新控件照此。
> - **颜色一律 `--sky-c-*`，每处带兜底** `var(--sky-c-x, #fallback)`
>   （`ui_css_test.go` 的三条硬约束：只写一个命名空间 / 每个引用带兜底 / 别名段与引用双向对表）。
>   间距用 `--ui-sp-*`，动效用 `--ui-motion-*`。
> - **多端**：`AddHover` 规则包在 `@media (hover: hover)`；触屏等价形态同段给出；
>   按压反馈 `:active` 不包媒体查询。**注意**：`AddHover` / `AddHoverNone` / `AddActive`
>   是**构建期插件样式引擎**的 API（`internal/builder/core/css.go:229/266/292`），
>   服务后台的 `ui.css` 是**静态文件**，需手写等价形态：
>   ```css
>   @media (hover: hover) { .x:hover { … } }      /* 等价于 AddHover */
>   @media (hover: none)  { .x:active { … } }     /* 等价于 AddHoverNone，触屏替代形态 */
>   .x:active { … }                                /* 等价于 AddActive */
>   ```
>   `ui.css` 现有代码已按此写（如第 29-31、50-52、103-105 行）。
> - **宽度不写死**：`min(100%, <设计宽度>)`；绝对位移用 `min()` / `clamp()` 按视口封顶，
>   上限要让元素自身尺寸参与计算。

---

### C.1 字段级错误提示链路（`?err=` → `aria-invalid` + `.form-error`）

**现状**：`administrators.html:28` 页顶 `<p class="badge badge-warning" role="alert">{{.Err}}</p>`；
`ui.css:622-635` 的 `.form-error` / `[aria-invalid]` 规则**零消费**；
`product_tag_page.go:172` 等 31 处 `&err=` 回带只带一句话，无字段定位。

**antd 契约**（`02-K` §2.2）：错误紧贴控件下方、红字、预留高度、控件边框转红。

**改法**

**① 数据侧：把「一句话」升级为「字段 + 文案」的有序结构，但**不破坏**现有 `?err=` 回带。**

新增一个可由 handler 填充的 DTO（放在 `pkg/response` 或模块 dto 包，不新建散落结构）：

```go
// 字段级校验错误的对外形状。字段名与前端 name 属性一一对应。
type FieldError struct {
    Field   string `json:"field"`
    Message string `json:"message"`
}
```

handler 侧**不改现有 `?err=`**（31 处不动），**只增**一条并行通道：把字段错误编码进
**一个** `?ferr=` 查询参数（JSON，Base64URL 免转义），例如：

```go
// 只在「能定位到字段」时带上；定位不到就只带 ?err=（现有行为不变）。
func withFieldErrors(target string, ferrs []response.FieldError) string {
    if len(ferrs) == 0 { return target }
    b, _ := json.Marshal(ferrs)
    sep := "&"; if !strings.Contains(target, "?") { sep = "?" }
    return target + sep + "ferr=" + base64.RawURLEncoding.EncodeToString(b)
}
```

**为什么用查询参数而不是 flash/session**：`AGENTS.md` 的鉴权链 + 现有 31 处回带全是
query 回带，**同一条路径**；session 会引入「刷新后错误还在/换页错误串页」的新状态。
代价是 URL 会变长 —— 所以**只在字段错误 ≤8 条时启用**，超了就退回只有 `?err=`。

**② 模板侧：页面不再逐字段手写，由 `shell` 统一注入。**

`shell.Prepare`（`internal/web/shell/shell.go`）解析 `?ferr=` 后放进渲染数据键 `FieldErrors`
（`[]response.FieldError`）。每个长表单页面**不需要改** —— 新增一个
**`admin/partials/field_error.html`** 片段，在需要字段级提示的页面里用一次：

```html
{{import "partials/field_error.html"}}
…
{{yield fieldError(field="name")}}
```

`{{yield fieldError(field="dept_name")}}` 展开为：

```html
{{if isset(.FieldErrors)}}{{range _, fe := .FieldErrors}}{{if fe.Field == params.field}}<p class="form-error" id="err-{{params.field}}">{{fe.Message}}</p>{{end}}{{end}}{{end}}
```

**③ 控件侧：新增 `static/js/ui/fielderror.js`，一个纯 DOM 增强，负责三件 CSS 做不到的事。**

契约（HTML 属性，**不改模板既有结构**）：

```html
<!-- 服务端渲染时直接带上：aria-invalid 让边框变红，紧跟其后的 .form-error 是提示 -->
<input class="form-input" name="name" aria-invalid="true" aria-describedby="err-name" value="{{.Form.Name}}">
<!-- 上面那句由 Go 侧（handler 或模板）按 FieldErrors 决定是否输出 -->
```

JS 只做**已存在错误**的收尾（不无中生有地创建错误，那是服务端的事）：

```js
// ui/fielderror.js —— 把「服务端已知的错误」挂到控件的可访问性链上，并支持就地清除。
// ① 给 [aria-invalid="true"] 的控件找它后面的 .form-error，写 aria-describedby；
// ② 用户在出错控件上首次输入时，移除 aria-invalid 与 .form-error（错误已被编辑动作
//    消解，不该继续红着），并派发 'wbui:fielderror-clear' 供页面挂自定义逻辑；
// ③ 键盘可达：错误文本用 sr-only 之外的常规文本，Tab 到控件时读屏经 aria-describedby 播报。
WBUI.register(function (scope) {
    WBUI.$$('[aria-invalid="true"]', scope).forEach(function (ctl) {
        if (!WBUI.markOnce(ctl, 'FieldError')) return;
        var box = ctl.parentElement && ctl.parentElement.querySelector('.form-error');
        if (box && !ctl.getAttribute('aria-describedby')) {
            if (!box.id) box.id = 'fe-' + (++seq);
            ctl.setAttribute('aria-describedby', box.id);
        }
        var clear = function () { /* 移除 aria-invalid + .form-error，解绑自身 */ };
        ctl.addEventListener('input', clear, { once: true });
    });
});
```

**注意 `select.js` 的联动**：`WBUI.select` 会把原生 `<select>` 视觉隐藏
（`sel.classList.add('wbs-native')`，`select.js:73`）。所以 `[aria-invalid]` 的
边框态**必须同时作用于 `.wbs-trigger`** —— 在 `ui.css` 增加：

```css
.form-select[aria-invalid="true"] + .wbs-trigger,  /* 结构上 select 在 .wbs 内、trigger 在其后 */
.wbs:has(> select[aria-invalid="true"]) .wbs-trigger { border-color: var(--sky-c-danger, #cf8080); }
```

（`.wbs` 的 DOM 结构见 `select.js:62-72`：`div.wbs > select.wbs-native + button.wbs-trigger + ul.wbs-menu`。）

**CSS 侧**：**零新增颜色** —— `ui.css:622-635` 已够。只需补两条：
- `.wbs-trigger` 的错误态（上面那条）；
- `.form-error` 的**预留高度**：antd 的错误「不改变布局」。本项目 `ui.css:635` 的
  `.form-error { margin-top: var(--ui-sp-xs) }` 是**插入式**（会把下方内容顶走）。
  改为「始终保留一行高度」在长表单里更稳，但那会给**所有**字段加一行空白。
  **建议保持插入式**（不预留），理由：本项目是服务端渲染，错误只在回带时出现一瞬间，
  布局跳动只发生一次；而全局预留高度会让 `menus.html` 那种 117 行的页面白长 117 行。

**影响面**：Go 侧 **10 文件 / 31 处 `&err=`**（**不删不改**，只在需要字段定位的页面**新增** `?ferr=`）；
模板侧 **60 处页顶错误条 / 50 文件**（**不动**，字段级提示是**新增的补充通道**，
页顶那条继续承担「说不清是哪个字段」的错误）；
`shell.Prepare` 1 处（新增键 `FieldErrors`）；
`ui.css` 2 条新增规则；`ui/fielderror.js` 1 个新文件 + `partials/ui_scripts.html` 1 行。

**验证**（浏览器控制台）：

```js
// ① CSS 链路真的通（先手动造一个错误态，验证规则生效）
document.querySelector('input[name=name]').setAttribute('aria-invalid','true');
getComputedStyle(document.querySelector('input[name=name]')).borderTopColor; // 期望 rgb(207,128,128) 系
// ② 可访问性链：控件真的关联到错误文本
document.querySelector('input[aria-invalid="true"]').getAttribute('aria-describedby'); // 非空
document.getElementById(document.querySelector('input[aria-invalid="true"]').getAttribute('aria-describedby')).textContent; // 错误文案
// ③ 输入后错误就地消解
var c = document.querySelector('input[aria-invalid="true"]');
c.value = 'x'; c.dispatchEvent(new Event('input', { bubbles: true }));
c.hasAttribute('aria-invalid');  // 期望 false
// ④ 端到端：提交一个必然失败的字段，看 URL
location.search.includes('ferr=');  // 期望 true
// ⑤ 多端：错误提示不造成横向溢出
document.documentElement.scrollWidth === document.documentElement.clientWidth; // 375 视口下期望 true
```

**风险**：
- **URL 增长**：`ferr` 是 Base64(JSON)，8 条错误的 URL 约 400 字符。**必须设上限**（见改法①）。
- **与 `?err=` 重复展示**：页面会同时有页顶 badge 和字段红字。**两者语义不同**
  （前者「操作失败」、后者「这个字段错了」），但**同一句话不能两处出现** —— 要求 handler
  在带 `ferr` 时，`err` 只放**总结性**文案（如「保存失败，请检查标红的字段」）。
- **i18n**：`fe.Message` 必须走已有的**白名单文案**（`AGENTS.md`「错误文案三件套」：
  `XxxFacingMessages` + 归口文案 + 结构化日志）。**不能**把 `err.Error()` 塞进去 ——
  `scripts/check-no-internal-error-leak.sh` 会拦。
- **`?ferr=` 是新查询参数的第三种形态**：`scripts/check-no-internal-error-leak.sh`
  的「重定向 query」检查项需要**同批确认**它对 `ferr` 的判定（`ferr` 带的是白名单文案则安全）。

---

### C.2 日期范围控件（`data-range-picker`）

**现状**：`inventory.html:107,111` / `analytics.html:61,65` / `customers.html:88,90` /
`masterdata_changes.html:95,97` —— 4 页 8 个独立 `<input type="date">`，形态完全一致。

**antd 契约**（`02-K` §3）：范围是一个控件（`RangePicker`），共享弹层与快捷区。

**改法**

**模板侧契约**（渐进增强，两个原生 input **留在 DOM 里**）：

```html
<!-- 容器加 data-range-picker，两个原生 input 原样保留（name/value/表单提交全不变） -->
<div class="filter-field" data-range-picker
     data-range-quick="{{ .["t"]("admin.common.range.quick", "近 7 天|近 30 天|本月|今年") }}">
  <label for="mv-time-from">{{ .["t"]("admin.inventory.filter.time", "时间") }}</label>
  <input type="date" class="form-input" id="mv-time-from" name="timeFrom" value="{{filterTimeFrom}}">
  <span class="range-sep" aria-hidden="true">–</span>
  <input type="date" class="form-input" id="mv-time-to" name="timeTo" value="{{filterTimeTo}}">
</div>
```

**升级前的形态必须能用**：不加载 JS 时它是两个普通日期框（现状一模一样）；
JS 增强后**只做锦上添花**。

**JS 侧：新增 `static/js/ui/range.js`**（约 90 行），`WBUI.register` 扫描
`[data-range-picker]`，做**三件事**（都不改变提交形状）：

1. **约束联动**：`from` 改变时给 `to` 设 `min`，`to` 改变时给 `from` 设 `max` ——
   防「起 > 止」这种必然空结果的筛选。这是纯 DOM 属性操作，`change` 事件不拦截。
2. **快捷按钮**（`data-range-quick` 提供文案，逗号/竖线分隔）：在**两个 input 之间或之后**
   渲染一组 `.range-quick > button[type=button]`；点击写两个 input 的 `value` 并派发
   `input` + `change`（**照抄 `mediafield.js:63-70` 的 `applyValue` 先例**：
   写 input → 派发事件 → 页面既有逻辑一行不改）。
   **`type="button"` 是关键** —— 在 `<form>` 里默认 `type` 是 `submit`，
   点了会直接提交筛选表单。
3. **非法组合的就地提示**：起 > 止时给 `to` 加 `aria-invalid="true"` 并用 C.1 的
   `.form-error` 通道提示 —— **与 C.1 复用同一条链路**（这是把两项改造合并成一个基座的收益）。

**CSS 侧（`ui.css` 新增一段 `.range-*`）**：

```css
/* 用 --ui-sp-* 与 --sky-c-*，全部带兜底；宽度不写死 */
.range-sep { padding: 0 var(--ui-sp-xxs, 4px); color: var(--sky-c-text-mute, #8a9199); }
[data-range-picker] { display: flex; align-items: center; gap: var(--ui-sp-xs, 6px);
                      flex-wrap: wrap; min-width: 0; }
[data-range-picker] .form-input { width: min(100%, 148px); }  /* 原生 date 的固有宽度量级 */
.range-quick { display: flex; gap: var(--ui-sp-xxs, 4px); flex-wrap: wrap; }
.range-quick button {
  padding: var(--ui-sp-xxs, 4px) var(--ui-sp-sm, 8px); font-size: 12px;
  border: 1px solid var(--sky-c-border, #e5e7eb); border-radius: var(--ui-r-sm, 6px);
  background: var(--sky-c-bg, #fff); color: var(--sky-c-text-secondary, #c2c8cf); cursor: pointer;
}
@media (hover: hover) {                       /* 等价 AddHover */
  .range-quick button:hover { border-color: var(--sky-c-border-strong, #d1d5db);
                              background: var(--sky-c-bg-hover, #f4f5f7); }
}
@media (hover: none) {                         /* 等价 AddHoverNone：触屏的等价形态 */
  .range-quick button { padding: var(--ui-sp-sm, 8px) var(--ui-sp-md, 12px); }  /* 触屏加大命中区 */
}
.range-quick button:active { background: var(--sky-c-bg-active, #e4e4e4); }    /* 等价 AddActive，不包媒体查询 */
.range-quick button:focus-visible { outline: 2px solid var(--sky-c-primary, #3d444f); outline-offset: 1px; }
```

**触屏要点**：原生 `<input type="date">` 在手机上会拉起系统日期选择器（这是**优点**，
不要自绘日期面板去替代它 —— 自绘面板在手机上比系统选择器差）。所以范围控件在触屏上的
形态是「两个系统日期框 + 一排快捷按钮」，快捷按钮的命中区在 `hover: none` 下加大。

**多端**：`flex-wrap: wrap` + `width: min(100%, 148px)` —— 375px 视口下两个日期框换行、
快捷按钮再换一行，不横向溢出。

**影响面**：`type="date"` **8 处 / 4 文件**；
`partials/ui_scripts.html` +1 行；`ui.css` +1 段；`ui/range.js` 新文件。

**验证**：

```js
// ① 控件确实增强（快捷按钮被渲染，且是 type=button）
[...document.querySelectorAll('[data-range-picker] .range-quick button')].every(b => b.type === 'button'); // true
// ② 点击快捷按钮写回原生 input 并派发事件
var box = document.querySelector('[data-range-picker]');
box.querySelector('.range-quick button').click();
[box.querySelector('input[name=timeFrom]').value, box.querySelector('input[name=timeTo]').value];
// 期望：两个非空 ISO 日期，且 from <= to
// ③ 约束联动真的生效
var f = box.querySelector('input[type=date]');
f.value = '2026-01-10'; f.dispatchEvent(new Event('change', { bubbles: true }));
box.querySelectorAll('input[type=date]')[1].min;  // 期望 '2026-01-10'
// ④ 提交形状未变（关键回归）
[...new FormData(box.closest('form')).keys()].filter(k => /time|date|from|to/i.test(k));
// 期望：两个独立字段名仍在，没有被合并成一个
// ⑤ 多端 375 视口
document.documentElement.scrollWidth === document.documentElement.clientWidth; // true
[...document.querySelectorAll('[data-range-picker] *')].filter(el => el.getBoundingClientRect().right > document.documentElement.clientWidth); // []
```

**风险**：
- **`min`/`max` 的时区**：`<input type="date">` 的 `value` 是 `YYYY-MM-DD`（无时区），
  直接用字符串比较赋值是安全的；**不要**用 `new Date().toISOString().slice(0,10)`
  —— 那是 UTC，东八区凌晨会差一天。用 `getFullYear/getMonth/getDate` 手工拼。
- **快捷按钮的中文文案**：`data-range-quick` 的文案由模板传入（走 `t()`），
  JS 不内置中文 —— 否则 i18n 漏网（`admin_group_f_i18n_test.go` 的 key 判据）。
- **`.filter-field` 的 flex 上下文**：`02-H` §7.2 记过 `.filter-fields` 必须
  `flex: 1 1 100%`，否则退化成单列。`[data-range-picker]` 放进 `.filter-field` 后，
  自身也是 flex 容器，**要在真实页面上确认没有把 label 挤走**。

---

### C.3 可搜索 / 可分组下拉（扩展现有 `WBUI.select`，**不新建文件**）

**现状**：`select.js:112-126` 的 `rebuild()` 丢弃 `optgroup` 语义；
`select.js:209-217` 只有字母前缀跳转（`indexOf(ch) === 0`），不是搜索；
`blocks.html:104-119` 的 16 项类型无分组。

**antd 契约**（`02-K` §3）：>10 项支持搜索；分组用 `optgroup` 呈现为不可选的分组标题。

**硬约束**：**不得破坏 `WBUI.select` 的既有契约** —— 原生 select 留 DOM、表单提交照旧、
`data-wb-native` 跳过、WeakMap 状态、`data-ui-key` 展开态恢复、`WBUI.select.create` /
`closeAll` / `openKeys` / `restoreOpen` 四个 API **签名不变**。
`select_contract_test.go` 用 node 求值 `select.js` 跑完整交互场景（展开/选择/关闭/
展开态跨重建恢复/点外部关闭），**这些断言必须全绿**。

**模板侧契约**（两个新属性，都放在**原生 `<select>`** 上，因为它是唯一真值）：

```html
<!-- 可搜索：>=10 项的枚举加 data-searchable -->
<select class="form-select" name="type" data-searchable>…</select>

<!-- 分组：用标准 optgroup（数据侧零迁移），label 就是分组标题 -->
<select class="form-select" name="kind" data-searchable>
  <optgroup label="{{ .["t"]("admin.blocks.group.structure", "结构") }}">
    <option value="block">{{ .["t"]("admin.blocks.kind.block", "区块") }}</option>
    <option value="header">{{ .["t"]("admin.blocks.kind.header", "页眉") }}</option>
    …
  </optgroup>
  <optgroup label="{{ .["t"]("admin.blocks.group.nav", "导航") }}">
    <option value="breadcrumb">…</option>
    <option value="drawer">…</option>
    <option value="search">…</option>
  </optgroup>
  …
</select>
```

**为什么用 `optgroup` 而不是自造 `data-group`**：
① 它是**标准 HTML**，`select.js:107-111` 的 `available()` **已经在读它**；
② 原生 select 在无 JS 时（服务端渲染的首屏、或 JS 失败）**分组仍然可见** ——
`<option>` 上的 `data-group` 在原生下拉里完全不可见，那才是真正的退化；
③ 与 `menus.html:106` 的 `data-type` 不冲突（那个是**过滤**用的，可以共存）。

**JS 侧改动（全部在 `select.js` 内，不新建文件）：**

**改动 1 —— `rebuild()` 输出分组（约 112-129 行）。** 保持 `items` 数组的**扁平索引**
不变（`choose(i)` / `highlight(i)` 都按 `sel.options` 的索引工作，是这套实现的基石）：

```js
function rebuild() {
    menu.innerHTML = '';
    items = [];
    var lastGroup = null, group = null;
    WBUI.each(sel.options, function (opt, i) {
        var parent = opt.parentElement;
        var inGroup = parent && parent.tagName === 'OPTGROUP';
        var label = inGroup ? (parent.label || '') : '';
        // 分组标题只在组内第一个可见项之前插入一次
        if (inGroup && label !== lastGroup) {
            group = document.createElement('li');
            group.className = 'wbs-group';
            group.setAttribute('role', 'presentation');   // 分组标题不可选
            group.setAttribute('aria-hidden', 'true');    // 不参与读屏选项
            group.textContent = label;
            menu.appendChild(group);
            lastGroup = label;
        } else if (!inGroup) { lastGroup = null; }

        var li = document.createElement('li');
        li.className = 'wbs-option';
        li.setAttribute('role', 'option');
        li.dataset.index = String(i);
        li.id = menu.id + '-' + i;
        li.textContent = opt.label;
        li.hidden = opt.hidden || (inGroup && parent.hidden);
        if (inGroup) { li.dataset.group = label; }
        if (!available(i)) { li.setAttribute('aria-disabled', 'true'); }
        menu.appendChild(li);
        items.push(li);
    });
    sync();
    if (root.classList.contains(OPEN_CLASS)) { highlight(sel.selectedIndex, 1); }
}
```

**关键正确性点**：`items` 仍与 `sel.options` **同序同长**（分组标题不进 `items`），
所以 `highlight()` 的取模遍历（`select.js:147-158`）与 `choose(i)` 的
`sel.selectedIndex = i`（第 183 行）**一行都不用改**。

**改动 2 —— 搜索（可选，`data-searchable` 才启用）。** 在 `menu` **之前**插入一个
搜索框（不进 `items`），输入时按 `opt.label` 过滤：不命中的 `<li>` 设 `hidden = true`。
**不要移除 DOM** —— 移除会让 `items` 下标与 `sel.options` 脱钩，把上面那条基石打碎。
过滤后按 `highlight(第一个可见项, 1)` 落点。

键盘：搜索框里 `ArrowDown` 把焦点交给 `trigger` 并 `highlight` 第一项；
`Escape` 清空搜索后收起（第二次 Escape 才关）。

**改动 3 —— `sync()` 加一行**（约 135-145 行）：菜单收起时**保留**搜索关键词会让下次展开
看到过滤后的残缺列表，所以 `close()` 里清空搜索框并解除所有 `hidden` 过滤。

**CSS 侧（`ui.css` 的 `.wbs-*` 段落，第 41-54 行附近新增）**：

```css
.wbs-group {
  padding: var(--ui-sp-xs, 6px) var(--ui-sp-md, 12px);
  font-size: 12px; font-weight: 600;
  color: var(--sky-c-text-mute, #8a9199);
  text-transform: none;
}
.wbs-search { padding: var(--ui-sp-xs, 6px); border-bottom: 1px solid var(--sky-c-border, #e5e7eb); }
.wbs-search input {
  width: 100%; box-sizing: border-box;
  padding: var(--ui-sp-xs, 6px) var(--ui-sp-sm, 8px);
  font-size: 13px; color: var(--sky-c-text, #1f2937);
  background: var(--sky-c-bg, #fff);
  border: 1px solid var(--sky-c-border-input, #c6ccd4); border-radius: var(--ui-r-sm, 6px);
}
```

**多端**：菜单本身已是 `position: absolute` + `z-index`（`select.js` 配套样式）；
菜单高度当前**无上限** —— 169 处 select 里有 `menus.html` 那种候选很多的场景。
**必须补**：

```css
.wbs-menu { max-height: min(60dvh, 320px); overflow-y: auto; overscroll-behavior: contain; }
```

`60dvh` 用 `dvh` 而非 `vh`（移动端地址栏收起/展开时 `vh` 不更新，菜单会超出视口）；
`overscroll-behavior: contain` 防止触屏滑到菜单底部时把页面一起带走（这是触屏的典型坑）。

**触屏**：`.wbs-option` 当前 padding 是 `7px 10px`（`ui.css:48`），触屏命中区偏小。
在 `@media (hover: none)` 下加到 `10px 12px`。
**注意**：`ui.css:50-52` 已有的 `@media (hover: hover) { .wbs-option:hover }` 是正确写法
（等价 `AddHover`），**不要**改成裸 `:hover`。

**影响面（这项最大，必须列全）**：
- `<select>` **169 处 / 52 个文件** —— 全部经 `WBUI.select` 增强（除 `data-wb-native` 的少数）；
- `optgroup` **2 处**（改造后模板侧会新增，属**纯增量**）；
- `select_contract_test.go` 的 node 探针**必须全绿**（这是回归门）；
- `WBUI.select.create` 的调用点（动态字段）：`grep -rn 'WBUI.select.create' static/js/`
  → 主要在 `workbench/`（检查器 schema→表单）；`create` **签名与行为不变**，
  但**新增的菜单结构（分组标题 / 搜索框）不能进 `items`**，否则 `openKeys`/`restoreOpen`
  的 `data-ui-key` 契约会受影响；
- `admin.js` 的「上级菜单候选过滤」按 `setAttribute('hidden'/'disabled')` 改 option
  —— `MutationObserver`（`select.js:230-235`）已监听 `hidden`/`disabled` 属性，
  `rebuild()` 重跑时会**重新计算分组标题**（因为 `lastGroup` 在每次 rebuild 重置），
  过滤后整组不可见时**分组标题也不该出现** —— 这需要一条额外规则：
  **组内全部项都不可见时跳过该组标题**。实现上用「先收集再输出」两遍扫描，
  或接受「空组标题短暂出现」的降级（**建议做对**，因为分组标题是本次新增的语义）。

**验证**：

```js
// ① 分组标题已渲染且不可选
var s = document.querySelector('select[name=kind]');
s.closest('.wbs').querySelectorAll('.wbs-group').length;      // 期望 == optgroup 数
[...s.closest('.wbs').querySelectorAll('.wbs-group')].every(g => g.getAttribute('role') === 'presentation'); // true
// ② items 与 sel.options 同序同长（分组标题没混进去）—— 这是本改动的核心不变量
var menu = s.closest('.wbs').querySelector('.wbs-menu');
var opts = [...menu.querySelectorAll('.wbs-option')];
opts.length === s.options.length;                             // 期望 true
opts.every((li, i) => li.textContent === s.options[i].label); // 期望 true
opts.every((li, i) => li.dataset.index === String(i));        // 期望 true
// ③ 键盘路径未被破坏：展开后方向键移动、Enter 选中
var t = s.closest('.wbs').querySelector('.wbs-trigger');
t.focus(); t.click();                                          // 展开
s.closest('.wbs').querySelector('.wbs-menu').hidden;          // false
t.dispatchEvent(new KeyboardEvent('keydown', {key:'ArrowDown', bubbles:true}));
t.getAttribute('aria-activedescendant');                       // 指向某个 wbs-option 的 id
// ④ 分组标题不被键盘选中（方向键跳不过去）
//    连续 ArrowDown 遍历全部项，active 元素始终是 .wbs-option 而非 .wbs-group
// ⑤ 搜索（data-searchable）
var inp = s.closest('.wbs').querySelector('.wbs-search input');
inp.value = '页'; inp.dispatchEvent(new Event('input', {bubbles:true}));
[...s.closest('.wbs').querySelectorAll('.wbs-option')].filter(li => !li.hidden).map(li => li.textContent);
// 期望：只含标签里有「页」的项（含「页眉」「页脚」）
// ⑥ 关闭后搜索被清空（不留残留过滤）
s.closest('.wbs').querySelector('.wbs-trigger').click();  // 关
s.closest('.wbs').querySelector('.wbs-trigger').click();  // 再开
[...s.closest('.wbs').querySelectorAll('.wbs-option')].some(li => li.hidden);  // 期望 false
// ⑦ 原生契约未破坏：
s.value;                                                    // 真实值仍在原生 select 上
[...new FormData(s.form).entries()].find(([k]) => k === 'kind');  // 提交仍带 kind
// ⑧ 长菜单不溢出视口（触屏尤其）
var m = s.closest('.wbs').querySelector('.wbs-menu');
m.getBoundingClientRect().bottom <= innerHeight;            // 期望 true
// ⑨ 回归：跑契约测试
//    go test ./internal/templates/ -run 'TestDropdown|TestSelect' -v
```

**风险**：
- **`items` 下标必须与 `sel.options` 严格同序** —— 这是整套交互（`choose(i)` /
  `highlight(i)` / `available(i)` / `data.index`）的基石。任何「把分组标题塞进
  `items`」或「过滤时移除 DOM」的写法都会**静默破坏选择功能**（选 A 得到 B）。
  验证 ② 就是为这条不变量写的。
- **`optgroup` 的 `disabled`/`hidden`**：`available()` 已处理，但 `rebuild()` 里
  `li.hidden` 的表达式（`select.js:122`）只看了 `opt.parentElement.hidden`，
  **没看 `parent.disabled`** —— 分组被整体禁用时 `available()` 会返回 false（正确），
  但 `li.hidden` 仍是 false（项可见但不可选）。**这次一并修**（改成同时判断）。
- **`data-wb-native` 的 select 不受影响**（`enhance` 第 55 行提前 return），
  工作台的 `wb-unit-select` 安全。
- **`select_contract_test.go` 的 node 探针脚本**在 `select_contract_test.go` 里以
  字符串常量保存，**读的是 `select.js` 源码**。改动后必须确认探针仍能求值
  （它用 `--input-type=module` + `--eval`，对 `optgroup` 无断言，预期不受影响，
  但**必须实跑**）。

---

### C.4 表格空态保留表头（`partials/table_empty.html`）

**现状**：`administrators.html:45-60` 等 **10 处**用 `{{if len(.Rows) == 0}}` →
`.empty-state` → `{{else}}` → `<table>` 的结构，**表头随表格一起消失**。

**antd 契约**（`02-K` §1）：「表头是认知锚点，删了用户不知道这列表有什么列」。

**改法**

**模板侧：新增 `admin/partials/table_empty.html`，用 `import` + `yield` 提供两段式契约。**

参照 `partials/bulk_bar.html` 的既有先例（`internal/templates/CLAUDE.md` 记录了
`import` + `yield` 的四条实测约束，**必须遵守**）：
`{{import ...}}` 写在 `{{extends "layout.html"}}` 之后、任何 `{{block}}` 之前；
`.["t"]` 不能直接写在 yield 参数里（先 `{{tr := .["t"]}}`）；
`{{yield b(args)}}` 不需要 `end`、`{{yield b(args) content}}` 必须配 `end`；
片段只收**结构 + 调用点传来的成品文案**。

契约设计为「**表头由调用点提供，空态文案由调用点提供，片段负责两者同时存在于 DOM**」：

```html
{{import "partials/table_empty.html"}}
…
{{if len(.Rows) == 0}}
  {{yield tableEmptyHead()}}
    …本页原有 <thead> 内容原样搬进来…
  {{yield tableEmptyHeadEnd()}}
    {{yield tableEmptyBody(title=tr("admin.admins.empty", "还没有管理员，点右上「新建管理员」创建一个。"),
                           desc="", actionLabel="", actionHref="")}}
{{else}}
  …正常分支…
{{end}}
```

片段内部（`partials/table_empty.html`）：

```html
{{block tableEmptyHead()}}
<div class="table-wrap table-scroll" tabindex="0" role="region">
  <table class="data-table">
    <thead>
{{end}}

{{block tableEmptyHeadEnd()}}
    </thead>
    <tbody><tr class="is-empty-row">
      <td colspan="99">
        <div class="empty-state">
          <p class="empty-title">{{ params.title }}</p>
          {{if params.desc}}<p class="empty-desc">{{ params.desc }}</p>{{end}}
          {{if params.actionLabel}}<div class="empty-actions">
            <a class="btn" href="{{ params.actionHref }}">{{ params.actionLabel }}</a>
          </div>{{end}}
        </div>
      </td>
    </tr></tbody>
  </table>
</div>
{{end}}
```

**`colspan="99"` 的理由**：表头列数是**动态的**（`administrators.html:63` 的 `col-check`
列受 `canDelete` 控制），片段不该假定列数（`CLAUDE.md` 第 4 条：片段不假定数量）。
浏览器对超出实际列数的 `colspan` 会**静默收敛**到表宽，是这里最省的写法。

**为什么不改成「始终渲染表格、空态用 `<tbody>` 的 `:empty` 伪类」**：
那要求 `{{range}}` 在外面、`{{if}}` 在里面，改动**每个页面的嵌套顺序**，
比加一个片段更侵入。**片段方案是叠加式的**：调用点只是把原有 `<thead>` 搬进 yield 里。

**CSS 侧**：`.empty-state` 已有样式。新增一条消掉单元格内边距对视效的影响：

```css
/* 空态行：表格里的空态不该继承数据行的内边距（会在空态周围留一圈空白） */
.data-table tbody tr.is-empty-row > td { padding: 0; border-bottom: 0; }
.data-table tbody tr.is-empty-row .empty-state { padding: var(--ui-sp-xxl, 32px) var(--ui-sp-lg, 16px); }
```

> **⚠️ 顺带发现的存量缺陷（与本项直接相关，建议同批修）**：
> `--ui-sp-xxl` 在**全仓没有任何定义处**：
> `grep -rn -- '--ui-sp-xxl:' internal/templates/static/css/` → **零命中**。
> 但 `theme.css:982` 的 `.empty-state { padding: var(--ui-sp-xxl) var(--ui-sp-lg); }`
> 与 `theme.css:994` 的 `.card-body > .form-grid + h2 { margin-top: var(--ui-sp-xxl); }`
> **都在用它，且没写兜底**。
> 而 `ui.css:290-297` 的 `:root` 兜底段只定义到 `--ui-sp-xl`（`--ui-sp-xxs`~`--ui-sp-xl`）。
>
> **后果（按 CSS 规范推导，需在浏览器实测确认）**：`var()` 引用未定义变量且无兜底时，
> 该声明在 **computed-value 阶段变为 invalid** → **整条 `padding` 声明被丢弃** →
> `.empty-state` 当前**没有任何 padding**，`32px` 的呼吸感一直没生效。
> `theme.css:994` 的 `margin-top` 同理失效（h2 的区块间距退化成 `--ui-sp-xl` 那条规则的值）。
>
> **修法（两选一，`--sp-xxl: 32px` 在 `theme.css:95` 已存在）**：
> ① **推荐**：在 `ui.css:290-297` 的 `:root` 兜底段补 `--ui-sp-xxl: var(--sp-xxl, 32px);`
>    —— 与同段其它 `--ui-sp-*` 写法完全一致，且**修的是根因**（兜底段的完整性）；
> ② 治标：给那两处引用加兜底 `var(--ui-sp-xxl, 32px)`。
> ①是本次改造的**唯一 token 新增**，也是 C.4 能拿到 `32px` 空态内边距的前提。
>
> **这条缺陷的存在说明「引用不悬空」的机器判据目前只覆盖了 `--sky-c-*`
> 命名空间**（`ui_css_test.go` 的第 ③ 条 / `backend_css_naming_contract_test.go` 的第 ② 条），
> `--ui-*` 语义量**没有对应的检查** —— 值得单开一条门禁（见 §D.3）。

**影响面**：**10 处**（`administrators` / `datarules` / `departments` / `menus` /
`masterdata_changes` / `products` / `blocks`×2 + 另有几处形式相同未逐一列出）；
新增 `partials/table_empty.html` 1 个文件；**其余 74 处 `.empty-state` 不动**
（大量 `.empty-state` 是**整页级**空态 —— 如「还没有站点工程」，那不是表格空态，
不该有表头）。这个区分很关键：**只改「空态取代了本应有表头的表格」的那 10 处**。

**验证**：

```js
// ① 空数据页面：表头和表体同时存在
document.querySelectorAll('.data-table thead th').length;   // 期望 > 0（改造前是 0）
document.querySelectorAll('.data-table tbody .empty-state').length;  // 期望 1
// ② 表头列与正常数据时的列一致（切到有数据的页面/或比对该页列定义）
[...document.querySelectorAll('.data-table thead th')].map(th => th.textContent.trim());
// ③ 布局未破坏：空态单元格不产生横向溢出
document.querySelector('.data-table').getBoundingClientRect().width <= document.querySelector('.table-scroll').clientWidth + 1; // true
document.documentElement.scrollWidth === document.documentElement.clientWidth;  // 375 视口下 true
// ④ 表体行数确为 1（空态占一行），不是 0 也不是 N
document.querySelectorAll('.data-table tbody tr').length;  // 期望 1
// ⑤ 键盘/读屏：空态在表格内，表头仍可被读屏按表关联
document.querySelector('.data-table thead').closest('table') === document.querySelector('.data-table'); // true
```

**风险**：
- **`<thead>` 的 yield 包夹**容易写错配平 —— `{{yield b(args) content}}` **必须配 `end`**
  （`CLAUDE.md` 第 3 条），而 `admin_template_integrity_test.go` 会立刻报红。
  **改动后必须跑 `go test ./internal/templates/...`**。
- **`col-check` 列的条件性**（`administrators.html:63` 的 `{{if canDelete}}`）——
  表头列数在空态与正常态下**本来就一致**（同一个 `<thead>`），yield 方案天然正确。
- **`colspan="99"` 在 `role="region"` + `tabindex="0"` 的容器里**：`table-scroll`
  的有无障碍语义不受影响，但**表头列数与 colspan 不匹配**在自动化 a11y 检查里可能报警。
  若项目有类似 `a11y_audit_test.go` 的表格检查，**先确认**（该测试目前只遍历**构建产物组件**，
  不覆盖 admin 模板，预期不受影响）。
- **`.empty-state` 的既有 74 处不动** —— 有人可能「顺手统一」，那会把整页级空态
  塞进表格，语义就错了。**改造说明里要写清这个边界。**

---

### C.5 抽屉头部固定提交按钮（`data-drawer-submit`）

**现状**：`layout.html:113-119` 的抽屉头只有 title + 关闭按钮；
`drawer.js:49` 只写 title；提交按钮在 `.form-actions`（**12 处**）。

**antd 契约**（`02-K` §2.3）：`Drawer.extra` —— 头部右侧固定取消/提交，滚动时始终可见。

**改法**

**模板侧契约（三种形态，按需选用，**全部可选**）：**

```html
<!-- 形态 A（推荐，最省）：抽屉打开时把当前表单的提交按钮「镜像」到头部 -->
<template id="tpl-dept-edit-3">
  <form method="post" action="/admin/departments/update" data-drawer-form>
    <input type="hidden" name="csrf_token" value="{{csrf}}">
    …
    <div class="form-actions">
      <button type="button" class="btn btn-ghost" data-drawer-close>取消</button>
      <button type="submit" class="btn btn-primary" data-drawer-submit>保存修改</button>
    </div>
  </form>
</template>
```

`data-drawer-form` 标在 `<form>` 上、`data-drawer-submit` 标在**底部那个提交按钮**上。
JS 把它的**克隆**放进抽屉头 `extra` 槽；点击克隆 → `form.requestSubmit(原按钮)`
（**关键**：`requestSubmit(submitter)` 会带上 name/value，且触发 J7 表单校验）。

**形态 B**：抽屉头声明自己的按钮（当底部不该有按钮时）：

```html
<button data-drawer-open="#tpl-x" data-drawer-submit="保存|btn-primary" …>
```

**不推荐形态 B** —— 按钮文案与 CSRF 表单耦合在触发处，会散落。**本方案只实施形态 A**。

**JS 侧（改 `drawer.js`，不新建文件）：**

1. `layout.html` 的抽屉头加一个槽（**唯一的模板改动**）：
   ```html
   <header class="drawer-head">
       <span class="drawer-title" data-drawer-title></span>
       <div class="drawer-extra" data-drawer-extra></div>
       <button type="button" class="drawer-close" data-drawer-close>✕</button>
   </header>
   ```
2. `openDrawer()` 在 `WBUI.scan(body)` 之后（`drawer.js:65` 附近）加：
   ```js
   var src = body.querySelector('[data-drawer-submit]');
   var slot = drawer.querySelector('[data-drawer-extra]');
   if (slot) {
       slot.innerHTML = '';
       if (src && src.form && src.form.hasAttribute('data-drawer-form')) {
           var mirror = document.createElement('button');
           mirror.type = 'button';                       // 关键：不做成 submit，避免脱离表单
           mirror.className = src.className;             // 复用既有 btn 样式，不新造外观
           mirror.textContent = src.textContent;
           mirror.setAttribute('aria-label', src.textContent);  // 头部上下文里语义要显式
           mirror.addEventListener('click', function () {
               src.form.requestSubmit(src);              // 带上 submitter，触发校验与 name/value
           });
           slot.appendChild(mirror);
       }
   }
   ```
3. `closeDrawer()` 里 `slot.innerHTML = ''`（`drawer.js:82` 附近，与 `body.innerHTML = ''` 同处）。

**为什么用 `requestSubmit(src)` 而不是 `src.form.submit()`**：
- `form.submit()` **绕过** `submit` 事件 → **`confirm.js` 的 `data-confirm` 会失效**，
  htmx 的 `hx-post` 表单也会失效（`htmx` 监听 form 的 submit 事件）；
- `form.submit()` **不带** submitter 的 name/value（`<button name="action" value="save">` 会丢）；
- `requestSubmit()` 同时解决两者，且触发 J7 的 `novalidate` / 校验链。
  `confirm.js:110` 已经用了同一手法（`form.requestSubmit()`），**先例一致**。

**为什么镜像按钮 `type="button"` 而不是 `type="submit" form="..."`**：
镜像按钮在 `<aside>` 里、不在 `<form>` 内，用 `form="id"` 关联也能提交，但
**按钮在抽屉头而表单在 body 里**，`form` 属性需要 form 有 `id` —— 而抽屉表单来自
`<template>` 克隆（`drawer.js:40`），`id` 会**与页面上其它同名 id 冲突**
（`menus.html` 那种「每行一份模板」的场景，52 个模板里的表单 id 极易撞车）。
`type="button"` + `requestSubmit` 避开整个 id 问题。

**CSS 侧（`ui.css` 抽屉段，第 183-197 行附近）**：

```css
.drawer-head { display: flex; align-items: center; gap: var(--ui-sp-sm, 8px); }
.drawer-head .drawer-title { flex: 1 1 auto; min-width: 0;
    overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }  /* 长标题不挤掉按钮 */
.drawer-extra { display: flex; align-items: center; gap: var(--ui-sp-sm, 8px); flex: 0 0 auto; }
```

**多端**：
- 抽屉宽度 `min(560px, 92vw)`（`ui.css:166`）—— 375px 视口下是 `92vw ≈ 345px`。
  头部要放「标题 + 按钮 + 关闭」三样，**标题必须能压缩**（上面 `flex: 1 1 auto; min-width: 0`
  + 省略号），否则按钮被挤出视口。
- 触屏：头部按钮沿用 `.btn` 的尺寸（`.btn` 在 `ui.css` 里已有触屏可用高度）；
  头部**不要**加 `:hover` 形态（已有的是 `hover: hover` 包裹的 `.drawer-close:hover`，
  符合规范）。
- **窄屏下头部按钮可能放不下**：`.drawer-extra` 在 375px 下若与标题争空间，
  标题省略号会吃掉所有宽度。建议在窄屏给按钮**只留图标**或**隐藏镜像按钮**并保留底部
  `.form-actions`（底部按钮**不删**，只是多了头部入口）：
  ```css
  @media (max-width: 480px) { .drawer-extra .btn { display: none; } }
  ```
  **但这会让手机用户失去头部入口** —— 与「触屏要有等价形态」冲突。
  **更稳的做法**：不隐藏，而是让底部 `.form-actions` 继续存在（它本来就在），
  头部按钮在窄屏改为文字省略 + `min-width: 0`。**两种都写进实施时的决策点。**

**影响面**：`data-drawer-open` **75 处 / 27 文件**（**零改动** —— 形态 A 是叠加的，
不加 `data-drawer-form` / `data-drawer-submit` 的抽屉**行为完全不变**）；
优先改造的是**字段最多的抽屉**（`mail.html` 5 模板、`inventory_warehouses` 24 标签、
`product_tags` 22、`product_categories` 20、`inventory_sources` 20、`mail_marketing` 20）；
`layout.html` 1 处（加槽）；`drawer.js` 2 处小改；`ui.css` 3 条规则。

**验证**：

```js
// ① 抽屉打开后头部出现镜像按钮
document.querySelector('[data-drawer-open]').click();
var extra = document.querySelector('[data-drawer-extra]');
extra.querySelectorAll('button').length;   // 期望 1
document.querySelector('[data-drawer-extra] button').type;  // 期望 'button'
// ② 点击头部按钮真的提交了表单（观察网络面板或加一次性监听）
var f = document.querySelector('[data-drawer-body] form[data-drawer-form]');
f.addEventListener('submit', e => window.__submitted = true, { once: true });
document.querySelector('[data-drawer-extra] button').click();
window.__submitted;   // 期望 true
// ③ requestSubmit 带上了 submitter（若底部按钮有 name/value）
//    在网络面板里确认请求体含该字段
// ④ 表单校验链生效：清空 required 字段后点头部按钮，不应发起请求
document.querySelector('[data-drawer-body] input[required]').value = '';
document.querySelector('[data-drawer-extra] button').click();
f.querySelector('input[required]').validity.valid;   // 期望 false，且浏览器弹出校验气泡
// ⑤ data-confirm 未被绕过（若表单带 data-confirm）
document.querySelector('[data-drawer-body] form[data-confirm]') !== null
  // 有此属性时：点头部按钮应弹确认框，而不是直接提交
// ⑥ 关闭后头部清空（下次打开不残留上一个表单的按钮）
document.querySelector('[data-drawer-close]').click();
document.querySelector('[data-drawer-extra]').children.length;  // 期望 0
// ⑦ 宽度不写死 + 无横向溢出（375 视口）
document.querySelector('.drawer').getBoundingClientRect().width <= innerWidth;  // true
document.documentElement.scrollWidth === document.documentElement.clientWidth;  // true
// ⑧ 键盘：Tab 顺序为 标题 → 头部按钮 → 关闭（焦点陷阱内可达）
//    在抽屉打开时按 Tab 遍历，确认镜像按钮在序列里且可聚焦
```

**风险**：
- **`requestSubmit` 的浏览器支持**：Chrome 76+ / Safari 16+ / Firefox 75+。
  项目已有先例（`confirm.js:110` 用了 `form.requestSubmit` + `form.submit()` 回退）。
  **照抄那个回退写法**，不要假设。
- **镜像按钮与底部按钮的双入口**：两个按钮文案相同、作用相同。
  **不是冗余** —— 这是 antd 的 `extra` 语义（滚动时可见），底部按钮服务「填完就提交」
  的线性流程。**但两者都不能删**。
- **`<template>` 克隆与 `WBUI.scan` 的时序**：`drawer.js:65` 先 scan 后聚焦；
  镜像逻辑必须放在 **scan 之后**（否则读不到增强后的 DOM），但要在 `focusableElements()`
  之前（否则焦点陷阱的 `first` 会落到底部按钮而不是头部）。
- **`data-drawer-form` 缺省时的行为**：没有这个属性 → 头部不渲染任何按钮（**安全默认**）。

---

### C.6 非 htmx 表单提交期间的按钮 loading（补 `admin.js` 的缺口）

**现状（修正 `02-K` §4 的表述）**：
- **htmx 路径已完整覆盖** —— `admin.js:224-321` 的 `initHtmxFeedback()` 对 25 处 `hx-*`
  自动 `WBUI.busy` + 进度条 + 失败 toast。这部分**不需要改**。
- **原生表单路径无任何反馈** —— 31 处 `?err=` 回带的 POST 走的是 `<form method="post">`。
  提交瞬间页面开始导航，浏览器**不会**保留页面的 loading 态（导航一开始就白屏），
  但也**没有任何提示**告诉用户「正在保存」。

**antd 契约**（`02-K` §4）：`Spin` / 按钮 loading —— 请求期间明确反馈。

**改法（`admin.js` 内新增一个 IIFE，与 `initHtmxFeedback` 并列）：**

```js
/* ===== 原生表单提交的忙碌态 =====
   与 initHtmxFeedback 的分工：那边管 hx-* 请求（局部替换，页面不导航），
   这边管原生 <form method="post">（整页导航，提交后页面就没了）。
   为什么仍然要做：提交到响应到达之间有网络往返（跨地域可能几百毫秒），
   期间页面**完全无反馈**，用户会重复点击 → 重复提交（本项目删除/批量操作都是 POST）。
   做到什么程度：点击提交按钮 → 该按钮进入忙碌态（复用基座 WBUI.busy）+ 进度条置位。
   **不能**拦截提交（不 preventDefault）—— 整页导航是这条路径的既定行为。 */
(function initNativeFormFeedback() {
    var bar = document.querySelector('[data-hx-progress]');
    document.addEventListener('submit', function (e) {
        var form = e.target;
        // 已被 confirm.js 拦截等待确认的提交：等确认后再标忙（否则用户停在确认框上，
        // 按钮已经转起来了，语义错误）。confirm.js 用 dataset.wbConfirmBypass 放行。
        if (form.dataset && form.dataset.wbConfirmBypass !== '1' && form.hasAttribute('data-confirm')) {
            return;
        }
        var submitter = e.submitter || form.querySelector('button[type="submit"], input[type="submit"]');
        // 用 setTimeout(0)：此刻标记会让浏览器在导航前**来不及绘制**，视觉上看不到任何变化。
        // 让出一个任务，浏览器先绘制忙碌态，导航紧随其后 —— 这几毫秒正是用户需要的反馈。
        setTimeout(function () {
            if (submitter && window.WBUI && WBUI.busy) { WBUI.busy(submitter); }
            if (bar) { bar.removeAttribute('hidden'); }
        }, 0);
    }, true /* 捕获阶段：在 confirm.js 的捕获监听之后、默认动作之前 */);
})();
```

**`e.submitter` 是新 API**（Chrome 81+ / Safari 15.4+ / Firefox 75+）。
回退到 `form.querySelector('button[type="submit"]')` —— 覆盖绝大多数情况。

**CSS 侧：零改动** —— 复用 `ui.css:256-265` 的 `.is-busy`（含
`@media (prefers-reduced-motion: reduce)` 的降级，`ui.css:264-266`）。

**为什么不做「提交后禁用整个表单」**：那会阻止用户改错重提，且整页导航即将发生，
表单状态本来就保不住。**只标忙碌，不改语义。**

**影响面**：**全部原生 POST 表单**（`grep -c '<form method="post"'` 覆盖 50+ 文件）；
`admin.js` +1 个 IIFE（约 25 行）；`ui.css` 零改动。

**为什么这条优先于原 §7 的其他项**（见 §D.1）：它**不加新属性、不改任何模板、
不动物件契约**，纯增量，且立即覆盖**数量最大的那条路径**（原生表单 > htmx 属性 2 倍以上）。

**验证**：

```js
// ① 提交时按钮进入忙碌态（用一个 beforeunload 拦下来观察）
addEventListener('beforeunload', e => { e.preventDefault(); e.returnValue = ''; }, { once: true });
var btn = document.querySelector('form[method=post] button[type=submit]');
btn.click();
// 在弹窗出现前查看：
btn.classList.contains('is-busy');          // 期望 true
btn.getAttribute('aria-busy');              // 期望 'true'
document.querySelector('[data-hx-progress]').hidden;  // 期望 false
// ② 忙碌态确实被绘制（不是「标记了但没画」）—— 用截图验证（AGENTS.md：无头环境不出渲染帧）
//    调用 ego_screenshot 后再读 is-busy 类，避免「标记存在但没渲染」的误判
// ③ 不拦截导航：默认动作未被 preventDefault
//    观察 beforeunload 弹窗确实出现（说明导航被发起）
// ④ 带 data-confirm 的表单：确认框弹出时按钮**不应**已转
//    点一个 data-confirm 表单的提交按钮 → 确认框出现 → 此刻 btn.classList.contains('is-busy') 期望 false
//    点「确定」→ 期望 true
// ⑤ prefers-reduced-motion 下动画关闭但状态仍在
//    media emulation 设 reduce 后：getComputedStyle(btn,'::after').animationName 期望 'none'
//    但 btn.classList.contains('is-busy') 仍为 true
```

**风险**：
- **`setTimeout(0)` 的双刃**：它让浏览器有机会绘制，但**极快的本地响应**（<几 ms）下
  可能在绘制前就导航了 —— 这是**可接受的**（用户感觉不到闪烁就是最好的结果）。
- **与 `confirm.js` 的顺序**：`confirm.js:102` 用**捕获阶段**监听 submit 并
  `preventDefault()`；本段也在捕获阶段。**同为捕获阶段时按注册顺序执行** ——
  `confirm.js` 先注册（`ui_scripts.html:12` 在 `admin.js` 之前），所以它先跑并
  设了 `dataset.wbConfirmBypass`？**不** —— `confirm.js:105` 检查的是
  `wbConfirmBypass === '1'`，首次提交时它是空的，所以 `confirm.js` 拦截并
  `preventDefault()`；本段**必须自己判断**（上面的 `data-confirm` 检查覆盖了这个场景）。
  **但首次提交时 `preventDefault()` 已被调用**，本段仍会标记忙 —— 所以
  **`data-confirm` 检查是本段的必需逻辑，不是优化**。
- **进度条 `[data-hx-progress]` 被复用**：它的 `data-msg-*` 是给 htmx 错误用的，
  本段只用来置位显隐，不会误用文案。但**语义上它是「htmx 进度条」** —— 若将来
  重命名，本段要同步改。**在注释里标注这个耦合**。

---

### C.7 树选择（`data-tree-select`，标注路径，不建树）

**现状**：5 处平铺 `<select>`，用 `Indent` 前缀模拟层级
（`menus.html:104` / `departments.html:105` / `navigations.html:85` /
`product_categories.html:142,189`）。

**antd 契约**（`02-K` §3）：`TreeSelect` —— 展开层级、缩进、排除自身及后代。

**决策：不实现真正的「折叠/展开树」，实现「树标注 + 祖先链搜索」。理由：**

1. **真树需要节点折叠状态**（哪些分支展开）—— 那要么进原生 select（**不可能**，
   原生 select 没有折叠语义），要么**脱离原生 select 自己维护状态**，
   那就**破坏了 `WBUI.select` 的渐进增强契约**（原生 select 必须是唯一真值）。
2. **层级深度有限**：本项目后台的菜单/部门/分类层级实测深度 ≤3
   （`02-I` §4.4 记录 `nav-nodes.html` 的 `depth > 3` 缩进封顶 36px）。
   平铺 + 缩进对 ≤3 层是**可读的**，真树带来的收益主要在深层级场景。
3. **`02-K` §7 已把树选择列为第 7 位（成本「中」）** —— 在 C.1/C.2/C.6 落地前，
   投入产出比不划算。

**本方案给出的是可落地的最小形态（等价交互契约的 80%）：**

**模板侧契约**（零新增属性 —— **复用 C.3 的分组渲染**）：

```html
<!-- 层级用"祖先链标签"表达，分组用 optgroup 表达，搜索复用 C.3 的 data-searchable -->
<select class="form-select" name="parent_id" data-searchable>
    <option value="0">{{ .["t"]("admin.menus.parent_root", "（根菜单）") }}</option>
    {{range _, p := .Parents}}
    <option value="{{p.ID}}" data-type="{{p.Type}}" data-path="{{p.Path}}">{{p.Indent}}{{p.Title}}</option>
    {{end}}
</select>
```

- **`data-path`**：服务端给出**祖先链**（如 `系统 / 权限 / 菜单管理`），
  这是真树里「面包屑」的等价物，且**可搜索** —— C.3 的搜索按 `opt.label` 匹配，
  需扩展为**同时匹配 `data-path`**（一行改动，见下）。
- **`Indent` 缩进的视觉等价**：`select.js` 渲染 `<li>` 时读 `data-depth`（若有）加左内边距：

```js
// rebuild() 里 li.textContent 赋值处增加：
if (opt.dataset.depth) { li.style.setProperty('--wbs-depth', opt.dataset.depth); }
```
```css
.wbs-option { padding-inline-start: calc(10px + var(--wbs-depth, 0) * 14px); }
/* 注意：用 padding-inline-start 而非 padding-left —— RTL 站点自动镜像
   （项目已有 RTL 先例：ui.css:176 的 [dir="rtl"] .drawer） */
```

**搜索扩展到祖先链**（C.3 改动 2 的延伸）：

```js
// 匹配：标签 或 祖先链。用户输入「系统」能命中「菜单管理」（路径含"系统"）
var q = query.toLowerCase();
var hit = (opt.label || '').toLowerCase().indexOf(q) >= 0
       || (opt.dataset.path || '').toLowerCase().indexOf(q) >= 0;
```

**排除自身（`product_categories.html:142` 的 `{{if o.ID != row.ID}}`）**：
**保留在模板侧** —— 它是**业务规则**（「不能把自己设成自己的父级」），
不是控件能力。控件的职责是渲染与筛选，不是知道「哪个 id 是当前正在编辑的行」。
**但这个规则在每个调用点重复了两遍**（第 142、189 行），
**建议抽到服务端**：handler 组装 `Parents` 时就把当前行排除掉，模板不再写 `{{if}}`。
这样**规则只有一份**，且控件保持无业务知识。

**影响面**：**5 处 / 4 个文件**（`product_categories` 2 处、`departments`、`menus`、`navigations`）。

**验证**：

```js
// ① 缩进生效且随层级递增
var s = document.querySelector('select[name=parent_id]');
var lis = [...s.closest('.wbs').querySelectorAll('.wbs-option')];
lis.map(li => getComputedStyle(li).paddingInlineStart);
// 期望：随 data-depth 递增的一组不同值
// ② 祖先链可搜索
var inp = s.closest('.wbs').querySelector('.wbs-search input');
inp.value = '系统'; inp.dispatchEvent(new Event('input', {bubbles:true}));
[...s.closest('.wbs').querySelectorAll('.wbs-option')].filter(li => !li.hidden).length;  // 期望 > 0
// ③ 排除自身的规则仍在（product_categories 页）
var rowId = s.form.querySelector('input[name=id]').value;
[...s.options].some(o => o.value === rowId);   // 期望 false
// ④ RTL 下缩进方向正确
document.documentElement.dir = 'rtl';
getComputedStyle(lis[1]).paddingInlineStart;   // 期望与 LTR 数值一致（logical property 生效）
document.documentElement.dir = '';
```

**风险**：
- **`data-path` 需要服务端提供祖先链** —— `menus.html` / `departments.html` 的
  `.Parents` 现在只有 `Indent` + `Title`。**需要 handler 侧补 `Path` 字段**。
  5 处的 handler 分布在 admin 与 product 两个模块，**这是本项的主要成本**（也是「中」的由来）。
- **`--wbs-depth` 用内联 `style.setProperty`**：与「颜色走 `--sky-c-*`」不冲突
  （这是**布局量**不是颜色）。但要注意 `ui_css_test.go` 的令牌检查**只看 CSS 源码**，
  内联 style 不被扫描，**安全**。
- **`padding-inline-start` 的浏览器支持**：现代浏览器全绿（Chrome 87+）。
  项目已用逻辑属性（`ui.css:176` 的 `[dir="rtl"] .drawer`、`ui.css:221` 的
  `[dir="rtl"] .wbc > input`），**一致**。

---

## D. 改造顺序与验证方法

### D.1 顺序（**与 `02-K` §7 不同，理由在后**）

| 序 | 项 | 为什么在这个位置 | 成本 | 可否独立上线 |
|---|---|---|---|---|
| **1** | **C.6 原生表单按钮 loading** | ① 不加新属性、不改模板、不动物件契约，**纯增量**；② 覆盖**数量最大**的路径（原生 `<form method="post">` 远多于 25 处 htmx）；③ 是 C.1/C.5 的**前置**——它们都会在「提交中」这个状态上与它交互 | **极低**（`admin.js` +25 行） | ✅ 完全独立 |
| **2** | **C.4 表格空态保留表头** | ① 纯模板 + 一个片段，**零 JS**；② 10 处、模式统一、机械可替换；③ 独立于所有其它项 | **低** | ✅ 完全独立 |
| **3** | **C.2 日期范围控件** | ① 只 4 页 8 处，形态完全一致；② 新控件 `range.js` **不动 `select.js`**（影响面可控）；③ 它的「非法组合提示」**依赖 C.1 的 `.form-error` 通道** → **必须在 C.1 之前或与之同批** | 低 | ✅ 独立（但提示部分依赖 C.1） |
| **4** | **C.1 字段级错误提示** | ① `02-K` §7 列第 1 位 —— 用户受伤最深（长表单页顶一句等于没提示）；② **CSS 已就绪**（`ui.css:622-635`），主要是 Go 侧 + `shell.Prepare`；③ **C.2 的非法组合提示、C.7 的排除自身提示都要复用它** → 它是**基座依赖** | **中**（Go 10 文件 + shell + 片段 + 1 个 JS） | ⚠️ 需与 C.2 协调 |
| **5** | **C.5 抽屉头部提交按钮** | ① 叠加式（75 处零改动），但**优先改造字段最多的 6 个抽屉**；② 依赖 C.6 的忙碌态语义（头部按钮点击后要有反馈） | 低 | ✅ 独立 |
| **6** | **C.3 可搜索 / 可分组下拉** | ① **影响面最大**（169 处 select、`select_contract_test.go` 回归门）；② **但收益也最大** —— 它同时解决 `blocks` 16 项、所有 >10 项枚举；③ 放在后面是因为**它最需要前面积累的信心**（`WBUI.select` 是全站下拉的基座，动它要格外确定） | 中 | ⚠️ **必须**跑 node 契约测试 |
| **7** | **C.7 树选择** | ① 5 处、但**需要 handler 侧补祖先链字段**（跨 admin/product 两模块）；② **完全复用 C.3 的分组 + 搜索** → **必须在 C.3 之后**；③ `02-K` §7 列第 7 位成本「中」，且真树方案会破坏渐进增强契约（见 C.7 决策） | 中 | ❌ 依赖 C.3 |

**与 `02-K` §7 的两处调整及理由：**

- **C.6 从第 6 位提到第 1 位**：`02-K` §4 的「`WBUI.busy` 未被普遍使用」经核实**不准确**
  （htmx 路径已全局接入，`admin.js:239-253`）。真正的缺口在**原生表单**这条更大的路径上，
  而它的成本是全部七项里最低的（不改模板、不加属性）。低垂果实。
- **C.1 从第 1 位降到第 4 位**：`02-K` §7 估它「成本：中（要改 handler 回带 + 模板）」——
  但它同时也是 **C.2 与 C.7 的依赖**（两处「就地提示」都要走 `.form-error` 通道）。
  把它单独排第一会让 C.2 无法独立完成。**先做零依赖的 C.6/C.4/C.2，再做 C.1，
  然后用 C.1 的通道收口 C.2 的提示部分** —— 这样每一步都能独立验证。

### D.2 每步的验证方法（可执行）

**共同前提**：改完必须跑
```bash
go test ./internal/templates/...          # 模板完整性 / 契约 / i18n 判据
go test ./internal/builder/...            # 基座类清单与 ui.css 一致性
```
涉及 `select.js` 时**额外**：
```bash
go test ./internal/templates/ -run 'TestDropdown|TestSelect' -v   # node 探针（需 node 在 PATH）
```

**每项的专项验证命令已在 C.1~C.7 各自的「验证」块给出**，此处只列**跨项的统一验收**：

```js
/* ① 三视口无横向溢出（1440 / 768 / 375 各做一次） */
document.documentElement.scrollWidth === document.documentElement.clientWidth;   // 期望 true
[...document.querySelectorAll('*')]
  .filter(el => el.getBoundingClientRect().right > document.documentElement.clientWidth + 1)
  .map(el => el.className || el.tagName);   // 期望 []

/* ② 控件扫描无错误（所有新控件都经 WBUI.scan） */
window.__wbuiErrors = [];
document.addEventListener('wbui:error', e => window.__wbuiErrors.push(e.detail.error.message));
WBUI.scan(document);
window.__wbuiErrors;   // 期望 []

/* ③ 增强幂等（htmx 替换后不叠第二套监听）—— 连续扫描三次 */
WBUI.scan(document); WBUI.scan(document); WBUI.scan(document);
document.querySelectorAll('.wbs-menu').length;
[...document.querySelectorAll('select')].filter(s => s.closest('.wbs')).length;
// 期望：后者 == 前者（没有 wbs 嵌套 wbs）

/* ④ 触屏形态存在（模拟触屏后 hover 规则不生效、等价形态生效） */
//    CDP: Emulation.setEmitTouchEventsForMouse / 或直接匹配媒体查询
matchMedia('(hover: none)').matches;   // 触屏模拟下应 true
//    此时确认：隐藏的 hover 规则确实不生效、@media (hover:none) 的等价规则生效

/* ⑤ 键盘全路径（每个新控件都要走完） */
//    展开 → 方向键 → Home/End → Enter 选中 → Esc 收起 → Tab 移出
```

**触屏必须用真实输入路径验证**（`AGENTS.md` 明确要求）：
程序化 `el.click()` 覆盖不到「用触屏滑一下」。用 CDP 发真实事件：

```js
// 触屏滑动（验证 .wbs-menu 的 overscroll-behavior: contain 是必要的且生效）
// CDP: Input.dispatchTouchEvent { type: 'touchMove', ... }
// 期望：菜单滚动到底后页面不跟着滚
// 鼠标滚轮（验证长菜单滚动）
// CDP: Input.dispatchMouseEvent { type: 'mouseWheel', deltaY: 100 }
// 期望：菜单内滚动，菜单外页面滚动
```

### D.3 全局风险与门禁

| 风险 | 触发条件 | 对策 |
|---|---|---|
| **`ui.css` 令牌检查失败** | 新样式漏了 `var()` 兜底，或用了未定义的 `--sky-c-*` 槽 | `ui_css_test.go` 三条约束会在 `go test ./internal/templates/` 报红；**每次改 CSS 都跑** |
| **`--ui-*` 语义量引用悬空（无门禁）** | 用了未定义的 `--ui-sp-*` / `--ui-r-*` 且未写兜底 | **现状已知一处**：`--ui-sp-xxl` 被 `theme.css:982,994` 引用但全仓无定义（见 C.4）。现有门禁只覆盖 `--sky-c-*` 的「引用不悬空」，`--ui-*` 无检查。**建议在 `ui_css_test.go` 补一条**：扫描 `--ui-(sp|r|motion|shadow|duration|ease|tr)-*` 的引用与定义，要求每个引用要么被 `ui.css:290-297` 的 `:root` 定义、要么带兜底 |
| **产物基座类清单漏项** | 新增 `form-error` 之外的类（如 `.wbs-group`）却没进 `ui_script.go:38-57` | `TestUIBaseClassListMatchesUICSS` 会报红。**注意**：`.wbs-group` 是**子元素**类，不进清单也对（清单只管「用了控件」的判定类，`.wbs` 已在列）；但要在实施时**确认这个判断**（测试只校验清单里的类**在 ui.css 里存在**，不校验「ui.css 里的类都在清单里」） |
| **i18n key 漏网** | 新控件内置中文文案（range 快捷按钮、搜索框 placeholder） | `admin_group_f_i18n_test.go` 的 key 判据；**所有文案必须由模板 `t()` 传入**，JS 零中文 |
| **`WBUI.select` 契约破坏** | `items` 与 `sel.options` 下标脱钩 | `select_contract_test.go` 的 node 探针 + C.3 验证 ② |
| **抽屉头部按钮挤爆窄屏** | 375 视口下 title + 按钮 + 关闭争空间 | C.5 的 `flex: 1 1 auto; min-width: 0` + 省略号；**窄屏决策点写进实施步骤** |
| **`?ferr=` 被门禁拦** | `scripts/check-no-internal-error-leak.sh` 的「重定向 query」检查项 | `ferr` 必须带**白名单文案**（`AGENTS.md` 的「错误文案三件套」），**同批跑该脚本确认** |
| **`requestSubmit` 兼容** | 老浏览器 | 照抄 `confirm.js:110` 的 `requestSubmit` + `submit()` 回退 |
| **无头环境不出渲染帧** | 忙碌态「标记了但没画出来」 | `AGENTS.md` 记录：`IntersectionObserver` / `rAF` / `scroll` 在无头下都不派发。**验证时先 `ego_screenshot` 强制渲染帧**再读状态 |

---

## 附录 A：影响面汇总（全部为 `grep` 实测）

| 项目 | 计数 | 口径 |
|---|---|---|
| `<select>` | **169** | `grep -rn '<select' internal/templates/admin/ --include=*.html \| wc -l`（52 个文件） |
| `optgroup` | **2** | 同上，改 `optgroup` |
| `type="date"` | **8** | 4 个文件（`inventory` / `analytics` / `customers` / `masterdata_changes`） |
| `data-drawer-open` | **75** | 27 个文件 |
| `<template id="tpl-…">` | **52** | 抽屉表单载体 |
| `.form-actions` | **12** | 6 个文件（admin 六领域各 2 处） |
| `.empty-state` | **84** | 51 个文件（其中**仅 10 处**是「空态取代了本应有表头的表格」） |
| `class="data-table"` | **77** | — |
| `hx-*` 属性 | **25** | `hx-post` / `hx-get` / `hx-put` / `hx-delete` |
| `hx-indicator` | **1** | 且为注释 |
| 页顶错误条 | **60** | `badge badge-warning" role="alert"`，50 个文件 |
| Go 侧 `&err=` | **31** | 10 个文件 |
| `aria-invalid` | **0** | 非 CSS 处 —— **CSS 规则已就绪但零消费** |
| `form-error` | **0** | 同上（仅 `ui_script.go:44` 的类清单） |
| 树选择候选（平铺 select 表达层级） | **5** | 4 个文件 |
| `--ui-sp-xxl` 定义处 | **0** | 但 `theme.css:982,994` 在引用它（无兜底）—— 存量缺陷，见 C.4 |

## 附录 B：不做什么（明确排除，避免过度设计）

1. **不引入任何前端框架运行时**（React / Vue / Alpine / Lit）—— 硬约束。
2. **不新建 `static/js/ui/` 之外的 JS** —— C.6 加在 `admin.js`（既有文件，属「待收敛」清单内）；其余全部在 `ui/`。
3. **不为 C.1 引入 flash / session 传错** —— 沿用 query 回带这条既有路径（31 处同一形态）。
4. **不实现真 `TreeSelect`** —— 理由见 C.7 决策（会破坏渐进增强契约，且收益在 ≤3 层时不成立）。
5. **不自绘日期面板** —— 手机上的系统日期选择器优于任何自绘实现（C.2）。
6. **不删页顶错误条** —— 字段级提示是**补充通道**，页顶那条继续承担「说不清是哪个字段」的错误。
7. **不动 74 处整页级 `.empty-state`** —— 只有「空态取代了本应有表头的表格」的 10 处要改（C.4）。
8. **不重构 `select.js` 的整体结构** —— 只做 `rebuild()` 输出分组 + 可选搜索 + `sync()` 清残留三处最小改动。

## 附录 C：本次调研中发现的、与改造无直接关系但应记录的缺陷

1. **`--ui-sp-xxl` 悬空引用**（见 §0 ④ 与 C.4）：`theme.css:982,994` 用了它且无兜底，
   全仓无定义 → 整条声明失效。**与 C.4 同批修最省**（补一行 `:root` 定义）。
2. **`select.js:122` 漏判 `optgroup` 的 `disabled`**：`li.hidden` 只看了
   `opt.parentElement.hidden`，没看 `parent.disabled` —— 整个分组被禁用时，
   项仍然渲染为可见（但 `available()` 会正确返回 false，所以是「看得见点不动」）。
   **C.3 一并修**（一行改动）。
3. **`mediafield.js` 的选图弹窗没走 `WBUI.modal`**（`mediafield.js:78-99` 自建
   `.media-pick-mask` div）—— 缺 `<dialog>` 的焦点陷阱与 `inert` 背景。
   `modal.js:1-29` 的注释把「弹窗该有的样子」列得很清楚，这个弹窗恰好缺了那几样。
   不影响本次任何改造项，但值得单独收口。
