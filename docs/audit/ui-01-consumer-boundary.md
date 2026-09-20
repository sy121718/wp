# UI-01 · 共享控件基座的消费端边界评估

> 来源：[gpt-2026-09-19 审查报告](./gpt-2026-09-19/report.md) §UI-01（P2 · 持续改进）
> 范围：**只读评估**。本文档不新增迁移、不改任何 `.go` / `.jet` / `.css` / `.sql` 文件。
> 结论一句话：**「一份源、两种投递」的模型成立，三份样式表的分工也基本落地；但边界在三个地方漏了 ——
> 后台宿主选择器随 `ui.css` 的 forms 段进产物、后台外壳专属样式段可被产物 class 命中、默认主题在取值层复制后台色板。
> 报告要求的「后台壳、站点主题与组件交互保持独立」应当落成**可机器判据化的四条边界**，而不是继续靠注释维持。**

---

## 1. 结论摘要

### 1.1 报告原文与核实结果

| 报告论断 | 核实结果 |
|---|---|
| 已有 UI token | **成立**。`--sky-c-*` 是两端统一的命名空间：产物侧由 `ThemeVarsCSS` 生成（`internal/builder/theme_settings.go:297`），后台侧由 `theme.css` 的 `[data-theme]` 别名段转供（`internal/templates/static/css/theme.css:120`） |
| 按需资产已经成立 | **成立**。`internal/builder/ui_css_split.go:179` 按段注入、`internal/builder/ui_script.go:262` 按特征选脚本；两者都说「没命中就是零字节」，实测纯自定义类页面注入 0 字节（§3.1） |
| reduced-motion 已有 | **成立**。基座侧两处：`ui.css:264`（按钮忙碌态）与 `ui.css:369-372`（把 `--ui-duration-fast/base` 归零、取消按钮按压位移）；后台壳的 `.hx-progress` 自带一份（`admin/layout.html:38-40`）；产物增强侧的动效词汇表见 `docs/02-F-ui-kit.md:437-457` |
| 共用控件成立 | **成立**。`internal/templates/static/css/ui.css:1-6` 自述「后台/前台共用这一份」，`internal/templates/static_embed.go:21-33` 是同一份源的运行时 / 构建期两个出口 |
| 抽屉问题说明共用层的错误会同时扩散 | **成立**。抽屉外观在基座（`ui.css:150`），后台壳只留结构（`internal/templates/admin/layout.html:112-119`）；`internal/templates/ui_css_ownership_test.go:160` 把这条归属钉成断言 |
| 工作台工具栏仍有仅符号名称的按钮 | **本次未复核**。属无障碍与文案条目（与 I18N-02 重叠），不在本票的「消费端边界」范围内 |
| RTL、读屏与触屏覆盖不完整 | **本次未复核**。同上，属 I18N-02 与既有 UIK 条目的覆盖面问题 |
| 不把整个后台 CSS/JS 打进所有静态页 | **成立**。`internal/builder/ui_script.go:74-83` 的 `UIAssetFiles()` 只列 `_util.js` + 控件 + `index.js`，不含 `admin.js` / `workbench.js`；`internal/templates/ui_assets_test.go:46` 把「后台只经公共入口加载控件」钉成断言 |
| 无 `hx-*` 页面不注入 htmx | **成立**。`internal/builder/ui_script.go:64` 是前缀命中（`hx-`），`internal/builder/ui_script.go:268` 在无命中且无基座类且无片段时直接返回零字节 |

### 1.2 本评估新增的四条实测

1. **后台宿主选择器确实随产物投递**（§3.1）。`ui.css:539-656` 的「后台裸控件兜底」整段位于 `forms` 段内，而 `forms` 段由作者会写的 `.form-group` / `.form-input` 触发：实测「页面只有一个 `.form-group`」时产物 CSS 从 0 变成 13335 字节，其中含 `6041` 字节的后台兜底规则（占 forms 段 `6481` 字节的 93%）。
   这段规则的选择器是 `.admin-layout :where(...)`，**在产物里不命中任何元素**，所以不造成视觉污染；它造成的是**字节污染 + 语义污染**（产物样式表里带着后台宿主假设）。
2. **后台外壳专属段可被产物 class 直接命中**（§3.2）。`ui.css:789-882` 的 `langswitch` 段自述是「后台外壳，多语言 P1 第二步」，但它按 class 触发：实测「页面写一个 `.lang-switch`」时产物 CSS 多出 4108 字节的后台外壳样式，且**没有任何门禁拦它**。
3. **反向没有回路**（§3.3）。后台与工作台只 `<link>` 自己的静态样式（`admin/layout.html:23-25`、`workbench/layout.html:12-20`），而产物 CSS 是构建期拼出的字符串（`internal/builder/builder.go:717-745`）—— 它不是一份能被后台引用的文件。`theme.css` / `workbench.css` / `media-lib.css` / `workbench-a11y.css` 的清单在 `internal/templates/backend_css_naming_contract_test.go:33-38` 被显式标注为「不进产物」。
4. **默认主题是取值层复制，不是引用**（§3.4）。`internal/builder/theme_default.go:9-10` 的注释直说「色值取后台设计语言（admin theme.css 的亮色主题）」，`theme_default.go:14-24` 把 11 个色值写成了与 `theme.css:8` 起同一批字面量。**没有任何测试**钉住这两处相等。

### 1.3 建议（按性价比排序）

| 优先级 | 动作 | 成本 | 说明 |
|---|---|---|---|
| **P0** | 段表加「消费端归属」：给 `uiCSSSection` 增 `owner` 维度，产物注入时跳过 `backendOnly` 段 | 改 1 个文件 + 1 条测试 | 一次同时解决 §3.1 与 §3.2：兜底规则与 `langswitch` 段都不再进产物。**已落实（2026-09-20，`65fc9c23`）**：`owner` 维度已进段表，命中判定与拼接两处都跳过 `ownerBackend` 段（只跳一处会留下「算命中但不输出」的隐性耦合）；门禁见 §4.2 改动清单第 1 条 |
| **P0** | 把后台兜底从 `forms` 段拆出（或移入 `theme.css`） | 改 `ui.css` 段结构 + 段表 | 产物会**少 6041 字节**；代价是产物字节变化，需按「基座层内部收口」单独一批。**已落实（2026-09-20，`65fc9c23`）**：兜底切为独立段 `bareform`（锚点沿用原段标题行，CSS 选择器一字未动），标 `ownerBackend`；实测 forms 段 6481 → 362 字节，`.form-group` 页面注入 13335 → 7216 字节 |
| **P1** | `DefaultThemeSettings` 与 `theme.css` 亮色主题加对账测试 | 1 条测试 | 现在是「注释约定」，一方改另一方静默分叉 |
| **P1** | 同步 `docs/02-F-ui-kit.md` §12.2 | 文档 | 该节写「命中就注入**整份** `ui.css`，**它不看 class 名**」，与 `ui_css_split.go` + `ui_script.go:262-265` 的实现不符。**已落实（2026-09-20，`65fc9c23`）**：§12.1–§12.4 已按实现改写（触发条件 = 属性特征**或**基座外观类；注入 = 按段；后台专属段永不进产物） |
| **P2** | `ui.css` 段表补「每段属于哪个消费端」的注释 | 文档 | 让「这段该不该进产物」不再需要读代码推 |
| **不做** | 把 `ui.css` 拆成多个文件 | — | `ui_css_split.go:10-13` 已论证：拆文件会让「一份真源」变成多份，后台 `<link>` 也要跟着改 |
| **不做** | 把 `theme.css` 打进产物 / 把产物 CSS 引回后台 | — | 前者会把后台色板带进站点产物（与「站点主题独立」相反），后者没有载体也不用做（§3.3） |

---

## 2. 现状盘点

### 2.1 四个消费端与各自的资源清单

| 消费端 | 入口 | 样式 | 脚本 |
|---|---|---|---|
| **后台页面** | `internal/templates/admin/layout.html` | `theme.css` + `ui.css`（`:23-25`）、按需内联 `KeyframesCSS`（`:28`）、壳自带 `.hx-progress`（`:33-41`） | `partials/ui_scripts.html`（`:120`）+ `admin.js`（`:121`） |
| **工作台** | `internal/templates/workbench/layout.html` | `theme.css` + `ui.css` + `trix.css` + `workbench.css` + `workbench-a11y.css`（`:12-20`） | 同一份控件入口 + 工作台的 ES module（`workbench/index.js`） |
| **站点产物** | `internal/builder/document.jet:11-14` | 产物 CSS（`builder.go:717-745`）+ 按段注入的 `ui.css`（`ui_css_split.go:179`）+ 片段基座（`fragment_base.go:117`） | `track.js` + 按特征挑选的 `enhance.js` 块 + 控件脚本（`builder.go:845`） |
| **运行时片段** | `internal/builder/builder.go:773-805` | **不注入任何 CSS**（`fragCtx.CSS = core.DiscardCSS()`，`:792`）—— 片段依赖页面已内联的样式 | 不注入脚本（`fragCtx.Features = nil`，`:795`） |

> 第四行是理解整个边界的关键：**片段是「只有 HTML」的消费端**。它的样式来源是「宿主页面按 `/_fragments/` 引用选出的片段基座」
> （`ui_script.go:122`：`hasHXAttr(attrs) || strings.Contains(c.HTML, fragmentPathPrefix)`），所以「片段样式」的验收必须回到**页面产物**上做，不能在片段响应里找。

### 2.2 共享的那一份（共享层）

四样东西**必须只有一份**，否则就是「同一个控件两套实现」：

| 共享物 | 真源 | 投递 |
|---|---|---|
| 原始控件行为 | `internal/templates/static/js/ui/*.js`（htmx / `_util` / select / drawer / modal / confirm / toast / colorfield / iconfield / busy / themetoggle / index） | 后台与工作台经 `/static`（`partials/ui_scripts.html:6-16`）；产物由构建期按特征内联（`ui_script.go:59-72`） |
| 原始控件外观 | `internal/templates/static/css/ui.css` | 后台整份 `<link>`；产物按段内联 |
| 令牌**命名** | `--sky-c-*` 命名空间 | 两端同名；取值各供：产物侧 `ThemeVarsCSS`，后台侧 `theme.css` 的 `[data-theme]` 别名段 |
| 一份源两个出口 | `internal/templates/static_embed.go:21-33` | 运行时 `/static`（gin.Dir）+ 构建期 `embed`（`pipeline/compile_assets.go:84-91`） |

为什么令牌只统一「名字」而不统一「取值」：`ui_css_delivery_boundary_test.go:3-26` 把它写清楚了 —— 产物的令牌集合随主题设置变化
（没配主色就根本没有 `--sky-c-primary`），后台的集合由后台色板决定。两端集合不同，由 `var(--sky-c-X, 兜底)` 的兜底吸收；
`ui_css_test.go:75` 断言「每个 `--sky-c-*` 引用都必须带兜底」，`ui_css_delivery_boundary_test.go:101-116` 把「只服务单端的槽」逐条登记。

### 2.3 必须分开的部分（独立层）

| 独立物 | 真源 | 为什么不能合并 |
|---|---|---|
| 后台壳结构与布局 | `admin/layout.html` + `partials/sidebar.html` + `theme.css:180-535` 的 `.admin-layout` / `.admin-main` / `.admin-topbar` / `.admin-content`（`:180` / `:185` / `:190` / `:208`）与 `.rail` / `.subnav`（`:486` / `:511`）段 | 站点产物没有侧栏、二级栏、窄屏导航遮罩这些宿主概念 |
| 后台壳的一次性样式 | `admin/layout.html:33-41` 内联的 `.hx-progress` | 只服务后台壳这一处，且用主题令牌取色；产物没有 htmx 进度条 |
| 工作台密度与布板 | `workbench.css` / `workbench-a11y.css` | 工作台是构建器前端（ES module、画布、检查器），密度与后台页面本就不同 |
| 后台页面私有布局 | `theme.css` 里带页面前缀的段（`source-` / `purchase-` / `masterdata-` / `pricing-` / `attr-` / `locale-` / `theme-` …） | 归位判据写在 `backend_css_naming_contract_test.go:225-246`：「可复用控件外观 → 基座；单页面私有布局 → 页面级」 |
| 站点主题取值 | `ThemeSettings` → `ThemeVarsCSS`（`theme_settings.go:297`） | 站点主题是**每个工程**的，后台色板是**产品固定的**；两者取值必须能独立演进 |
| 组件私有外观 | `.sky-c-{nodeId}` 作用域，经 `core.CSSBuckets` 编译（`core/component_css.go:61`、`core/css.go:464`） | 每个组件实例一个作用域，两个实例互不污染 |
| 片段基座 | `fragment_base.go:44-52` 的两族（cart / orders） | 片段样式只有产物侧有消费方；未归基座的能力在 `fragment_base.go:69-88` 逐条登记 |
| 动效系统 | `internal/builder/core/effects.go` + `keyframes_*` | 只有产物侧：`docs/02-F-ui-kit.md:437-457` 明确「后台页面上写动效词不会生效」 |

### 2.4 一份段表：`ui.css` 的 19 段与两个消费端

`uiCSSSections()`（`ui_css_split.go:55-77`）把 `ui.css` 切成 19 段，每段要么是**公共段**（`sectionTriggerPublic`，随任一命中段一起带），
要么是**类触发段**（`sectionTriggerClasses`，段内类名出现在页面上才带）。实测各段字节（§附录 A 的探针）：

| 段 | 触发 | 字节 | 备注 |
|---|---|---|---|
| `head` | 公共 | 328 | 文件头注释 |
| `select` | 类/资源 | 2656 | `.wbs-*` |
| `confirm` | 类/资源 | 1196 | |
| `modal` | 类/资源 | 2188 | |
| `toast` | 类/资源 | 1715 | |
| `drawer` | 类/资源 | 2699 | 抽屉外观归基座（`ui_css_ownership_test.go:160`） |
| `colorfield` | 类/资源 | 2934 | |
| `busy` | 类/资源 | 1062 | |
| `base-doc` | 公共 | 1281 | 自述「后台、工作台、前台产物共用一份」 |
| `tokens` | 公共 | 1145 | `--ui-sp-*` / `--ui-r-*` / `--ui-motion-*` 兜底（`ui.css:289`） |
| `buttons` | 类/资源 | 2728 | |
| `cards` | 类/资源 | 3201 | |
| `tables` | 类/资源 | 5159 | |
| **`forms`** | 类/资源 | **6481** | **其中 `ui.css:539-656` 是后台裸控件兜底（6041 字节）—— 见 §3.1** |
| `badges` | 类/资源 | 1900 | |
| `pagination` | 类/资源 | 1797 | |
| `utilities` | 公共 | 899 | 工具类不单独触发（`ui_css_split_test.go:103`） |
| `themetoggle` | 类/资源 | 711 | 明暗切换，两端都用 |
| **`langswitch`** | 类/资源 | **4108** | **注释自述「后台外壳」—— 见 §3.2** |

合计 44188 字节（`templates.UICSS()` 实测）。

### 2.5 构建期 CSS 桶（`CSSBuckets`）的归属

`core.CSSBuckets`（`core/css.go:77-95`）是**产物 CSS 的唯一收集器**，按三个维度组织：

| 维度 | 字段 | 输出位置 |
|---|---|---|
| 断点 | `desktop` / `tablet` / `mobile` | `@layer sky-base` 内按断点顺序 |
| 输入形态 | `hover`（包 `@media (hover: hover)`）/ `active`（不包，触屏唯一可靠反馈） | 同上 |
| 层级 | `containersAuto` / `containersTheme` / `containersLocal`、`topLevel`（`@property` 注册） | `@layer sky-auto / sky-theme / sky-local`，注册类规则留在层外（`builder.go:728-736`） |

最终拼装（`builder.go:717-745`）的层序是固定的：

```css
@layer sky-base, sky-plugin, sky-auto, sky-theme, sky-local;
```

两点与本票相关：

1. **`CSSBuckets` 是产物独占的**。后台与工作台不经过它 —— 后台的样式是手写 CSS 文件，工作台的也是。
   所以「后台壳的样式被产物继承」不可能通过这条路径发生；可能发生的只有 §3.1 那条（共用文件 `ui.css` 的段内混入后台规则）。
2. **组件外观与后台无共享**：组件样式写 `sky-*` 类（`docs/02-F-ui-kit.md:423-424`），经 `.sky-c-{nodeId}` 作用域编译；
   后台页面不存在 `.sky-c-*` 宿主，所以这条方向天然隔离。

---

## 3. 串味实测

先给判据，免得「串味」被当成一个笼统的感觉：

| 方向 | 判据 | 实测结论 |
|---|---|---|
| 后台 → 产物 | 后台专属选择器、宿主类、外观出现在产物 CSS 里 | **成立两处**（§3.1 / §3.2） |
| 产物 → 后台 | 产物生成的 CSS/JS 被后台页面加载 | **不成立**（§3.3） |
| 后台 → 产物（取值） | 后台色板被写死进产物 | **不成立**（产物只走 `--sky-c-*` 变量与自带兜底）；但**默认主题复制了后台色值**（§3.4） |

### 3.1 后台宿主选择器进产物（实测确证）

`ui.css:539-551` 的注释自己声明了意图：

> · 只覆盖 `.admin-layout` —— 这个类只出现在 `admin/layout.html`，前台产物与工作台都没有它，
>   基座注入不会把后台外观带进产出页面；

这句在**「不命中元素」**这个意义上是对的（产物里确实没有 `.admin-layout` 宿主），但它**不等于「不进产物」**：
规则所在的 `forms` 段由类触发，而 `.form-group` / `.form-label` / `.form-input` 正是作者会写的类。

实测（附录 A 的探针，真实 `templates.UICSS()` 输入）：

| 页面 HTML | 注入字节 | 含 `.admin-layout` |
|---|---|---|
| `<button class='btn btn-primary'>` | 9582 | 否 |
| `<div class='form-group'><input class='form-input'>` | 13335 | **是** |
| `<div class='card'><div class='card-body'>` | 6854 | 否 |
| `<div class='sky-hero'>`（纯自定义类） | 0 | 否 |

第二行的 13335 字节里，后台兜底占 6041 字节（`ui.css:539-656`，118 行）—— 即 forms 段的 93%。
换句话说：**任何在产物里用了表单控件的页面，都会带上一份只为后台准备的兜底规则**。

影响面：

- **不造成视觉污染**（选择器不命中）。
- **造成字节污染**：约 6KB/页（gzip 后小得多，但乘上全站页数与 CDN 出流量仍是白付）。
- **造成语义污染**：产物样式表里出现「后台宿主」这个假设，读产物 CSS 的人会以为产物存在 `.admin-layout` 世界。
- 现有门禁只覆盖「宿主唯一」这一半：`ui_css_ownership_test.go:113-125` 断言 `.admin-layout` 只出现在 `admin/layout.html`，
  **没有任何断言说它不该进产物**。

### 3.2 后台外壳样式段可被产物 class 命中（实测确证）

`ui.css:789-791` 的段注释写明它的归属：

> 语言切换（后台外壳，多语言 P1 第二步）：GET /admin/lang 表单，零 JS 依赖

但段表给它的触发策略是 `sectionTriggerClasses`（`ui_css_split.go:75`），触发类由段内类名自动推导 ——
于是产物页面只要写了 `.lang-switch` / `.lang-select`，就会把 **4108 字节的后台外壳样式**带上。

实测：

| 页面 HTML | 注入字节 | 含 `.lang-switch` |
|---|---|---|
| `<select data-ui-select>` | 6309 | 否 |
| `<div class='lang-switch'><select class='lang-select'>` | 7761 | **是** |

这条比 §3.1 更值得修，因为它是**开放的**：段表里没有任何机制表达「这一段属于哪个消费端」，
未来任何一个后台专属外观段（下一个 `.xxx-switch`）都会自动获得「可进产物」的资格。

### 3.3 反向：产物样式没有回路进后台（实测为「不存在」）

逐条排查了「产物 → 后台」的所有可能载体：

| 可能的载体 | 实际 |
|---|---|
| 后台 `<link>` 产物 CSS | 不存在。后台只引 `/static/css/theme.css` 与 `/static/css/ui.css`（`admin/layout.html:23-25`） |
| 工作台 `<link>` 产物 CSS | 不存在。工作台引 `theme.css` / `ui.css` / `trix.css` / `workbench.css` / `workbench-a11y.css`（`workbench/layout.html:12-20`） |
| 后台 `<script>` 产物 JS | 不存在。后台只经 `partials/ui_scripts.html` 引控件 + 自己的 `admin.js` |
| 产物 CSS 作为文件被后台引用 | 不存在。产物 CSS 由 `builder.go:717-745` 在内存里拼成字符串，经 `document.jet:11-14` 内联；它不是一份有 URL 的样式表 |
| 构建期把后台静态样式读进产物 | 不存在。`pipeline/compile_assets.go:84-91` 只注入 `enhance.js` / `track.js` / `ui/*.js` / `ui.css` |

`backend_css_naming_contract_test.go:31-38` 把这份清单写成了代码里的常量（`backendStaticCSSFiles`），注释直说「不进产物，由 admin 页面 `<link>` 引入」——
所以这条边界目前是**有载体的**：新增一份后台静态样式时，要么进这个常量、要么进 `ui.css` 段表，绕不过去。

### 3.4 取值层耦合：默认主题复制后台色板

`internal/builder/theme_default.go:9-10`：

> 色值取后台设计语言（admin theme.css 的亮色主题），让「后台长什么样、前台默认也长什么样」

`theme_default.go:14-24` 的 11 个色值与 `theme.css:8` 起的亮色主题 `--c-*` 逐项同值。这是**有意的产品取向**（新站默认配色与后台一致），
但它实现成了**字面量复制**：改 `theme.css` 的主色不会让默认主题跟着变，两处只会静默分叉。

可观测的症状：新建工程的站点默认主题色与后台主色不再一致，而没有任何测试或日志提示 —— 与「对照关系」有关的问题都属于这一类。
（注意这**不是**「后台样式被产物继承」：产物拿到的始终是 `ThemeSettings` 的取值，不是 `theme.css` 的取值。）

### 3.5 文档与实现漂移（`docs/02-F-ui-kit.md` §12.2）

该节写：

> **CSS**：`uiAssetsFor()` 只判断「**有没有任何控件命中**」，命中就注入**整份** `ui.css` —— **它不看 class 名**。

实现已经不同（两部分都不成立）：

| 文档说法 | 实现 |
|---|---|
| 只看 `data-ui-*` 特征 | `ui_script.go:262-265`：`needStyle := needStyleFromAttrs \|\| hasUIBaseClass(scan.classes)` —— **class 也触发**（`uiBaseClasses` 清单在 `ui_script.go:38-57`） |
| 命中即注入整份 | `ui_css_split.go:179-219`：按段注入，公共段随行、类触发段按需（`ui_css_split_test.go:76-100` 断言「只用按钮不注入表格与分页」） |

§12.4 的「一句话判据」因此也要改：现在是**两句话** —— 产出页面「先有 `data-ui-*` 标记**或**基座外观类，再有样式」。

> **已落实（2026-09-20，`65fc9c23`）**：`docs/02-F-ui-kit.md` §12.1–§12.4 已按实现改写 —— 触发条件补齐「**或**基座外观类」、注入内容从「整份」改为「**按段**」、并补记「后台专属段（`owner=backend`）永不进产物」与三类 class 的区分；§12.4 的旧判据已替换。本节的原始判据与证据行保留。

---

## 4. 建议边界与改动清单

### 4.1 四层边界（建议口径，与实现现状对齐）

报告要求的四层，在本仓库对应的**落点**如下（第三、四层是当前缺口的所在）：

| 层 | 内容 | 落点 | 跨消费端约束 |
|---|---|---|---|
| ① 设计令牌 | `--sky-c-*` 命名空间 + `--ui-sp-*` / `--ui-r-*` / `--ui-motion-*` 语义量 | 产物：`ThemeVarsCSS`；后台：`theme.css` 的 `[data-theme]` 别名段 | 两端**同名**；引用**必须带兜底**（`ui_css_test.go:75`）；单端槽**必须登记**（`ui_css_delivery_boundary_test.go:101`） |
| ② 无业务原始控件 | `js/ui/*.js` + `ui.css` 的控件段 | 同一份源、两种投递 | 同进同出：命中控件就要带脚本 + 外观（`ui_script.go:258-270`） |
| ③ 后台 / 站点各自组合 | 后台 `admin/layout.html`；站点 `document.jet` + `CSSBuckets` 层序 | **各自独立** | **`ui.css` 的每一段必须声明属于哪个消费端**（缺口：§3.1 / §3.2） |
| ④ 模板 / 主题视觉参数 | 后台 `theme.css`；站点 `ThemeSettings` | **各自独立** | 取值可不同；**同一语义若两边都写死，必须有对账测试**（缺口：§3.4） |

### 4.2 改动清单

| # | 动作 | 文件 | 成本 | 验收 |
|---|---|---|---|---|
| 1 | `uiCSSSection` 增 `owner` 字段（`shared` / `backend` / `product`），产物注入时跳过 `backend` 段 | `internal/builder/ui_css_split.go` | 小 | 新增测试：`form-group` 页面注入的 CSS **不含** `.admin-layout`；`lang-switch` 页面注入的 CSS **不含** `.lang-select` |
| 2 | 后台裸控件兜底从 `forms` 段拆出为独立段（`owner=backend`），或整段移入 `theme.css` | `internal/templates/static/css/ui.css` + 段表 | 中 | 产物字节下降 ≈6041/页；`ui_css_ownership_test.go:97`（兜底必须存在且用 `:where`）与 `:113`（宿主唯一）继续通过；产物侧仍无视觉变化 |
| 3 | `DefaultThemeSettings` ↔ `theme.css` 亮色主题对账测试 | 新测试（`internal/builder/theme_default_test.go` 已有，可扩展） | 小 | 逐色项相等；任一侧改动而另一侧未改则失败 |
| 4 | `docs/02-F-ui-kit.md` §12.2 / §12.4 按实现改写 | 文档 | 小 | 与 `ui_script.go:262-265`、`ui_css_split.go:179` 一致 |
| 5 | 段表注释补「每段属于哪个消费端」 | `ui_css_split.go` 段表 | 小 | 新增段时能直接照抄归属，不必读全仓 |

改动 1 与 2 的产物字节会变（少字节），需要按「基座层内部收口」单独一批并重建存量产物；
改动 3 / 4 / 5 不触及产物字节。

---

## 5. 不适用 / 不在本票范围

| 报告或任务里提到的东西 | 结论 |
|---|---|
| `site/themes/` 目录路径 | **不适用**。本仓库**没有**这个目录（`find . -type d -name themes` 无结果）。站点主题是数据库表 `themes`（`internal/module/project/model/theme_model.go:15`）+ `ThemeSettings` 结构；产物侧主题只以 `:root --sky-c-*` 变量块出现（`theme_settings.go:297`）。**主题不经过文件系统目录**，所以「站内 themes 目录的边界」这条判据在源码里没有对应物 |
| 「组件模板」目录 | 组件模板在 `internal/templates/components/*.jet`，组件外观经 `core.CSSBuckets` 编译成 `.sky-c-{nodeId}` 作用域（`core/component_css.go:61`）。它**不参与**后台/站点共享的讨论 —— 后台没有 `.sky-c-*` 宿主，天然隔离 |
| 「组件交互」独立性 | 本仓库的组件交互由 `enhance.js`（按 `data-*` 特征挑选块，`builder.go:845`）+ 基座控件承担；组件模板内不写 JS。所以「组件交互与后台壳独立」这条**已经成立**，不需要改动 |
| 工作台工具栏按钮只有符号名称 | 属无障碍与文案条目，与 I18N-02 / 既有 UIK 条目重叠，本票不评估 |
| RTL、读屏、触屏的完整覆盖 | 同上；本票只给「跨消费端的验收清单」（§6），不复核既有覆盖面的完整性 |
| 动效系统是否该与后台共享 | **不适用**。`docs/02-F-ui-kit.md:437-457` 已判定：动效只属于产物侧，后台只有 3 个自有的反馈动画 |
| 存量 `pages-*` 桥接类的继续清理 | 不在本票范围。`docs/02-F-ui-kit.md:248-253` 有迁移清单，`ui_css_ownership_test.go:129` 与 `backend_css_naming_contract_test.go:249` 已钉住「不得回流」 |

---

## 6. 跨消费端验收清单

按消费端列「一个控件在四个场景下必须跑同一批用例」。做 UI 改动时逐行勾，**不要只在后台看一眼就交**。

### 6.1 后台（admin）

| # | 检查 | 判据 |
|---|---|---|
| A1 | 控件可用 | 打开后台页面，`select` / 抽屉 / 确认框 / 主题切换都工作（`partials/ui_scripts.html` 12 个脚本无 404） |
| A2 | 样式来源唯一 | DevTools 里 `.form-input` 的计算值来自 `ui.css`，不是 `theme.css` 的页面段 |
| A3 | 后台专属样式没有进产物 | `grep -c admin-layout` 在产物 CSS 里为 0（改动 1/2 后成立） |
| A4 | 暗色主题 | `data-theme=dark` 下所有 `--sky-c-*` 有值（别名段供给），无写死亮色 |
| A5 | 新增后台样式归位 | 可复用控件外观 → `ui.css` 基座段；单页面布局 → `theme.css` 带页面前缀的段或页面内联 `<style>`（`backend_css_naming_contract_test.go:225-246`） |

### 6.2 工作台（workbench）

| # | 检查 | 判据 |
|---|---|---|
| W1 | 控件入口唯一 | `workbench/layout.html` 仍只经 `partials/ui_scripts.html`（`ui_assets_test.go:46`） |
| W2 | 密度归属 | `wb-*` 类只出现在 `workbench.css` / `ui.css`（`.wb-btn` 在基座），页面不另抄一份 |
| W3 | 工作台样式不进产物 | `backendStaticCSSFiles` 不含 `workbench.css`（现状如此），产物 CSS 里无 `.wb-canvas` 等 |

### 6.3 站点产物（artifact）

| # | 检查 | 判据 |
|---|---|---|
| P1 | 按需注入 | 纯自定义类页面产物 CSS 长度为 0（实测为 0）；只用按钮的页面不出现 `.data-table` / `.pagination-btn` / `.lang-select` / `.wbs-trigger` |
| P2 | 同进同出 | 命中 `data-ui-*` 时脚本与样式一起进；`needStyleFromAttrs` 为假（只命中 htmx）时不带控件基座 |
| P3 | 主题取值来自站点主题 | 改工程主题色 → 产物 `:root` 的 `--sky-c-*` 变化，且 `theme.css` 的任何值都不出现在产物里 |
| P4 | 片段基座按引用选取 | 页面只写 `hx-post="/_fragments/cartAdd"`（没有 cart 组件）时，产物 CSS 仍含 cart 族基座（`fragment_base.go:117`） |
| P5 | 无 `hx-*` 不注入 htmx | 产物 HTML 里 `grep -c 'hx-'` 为 0 时，产物 `<script>` 里不含 htmx 源码 |
| P6 | 产物不带后台宿主规则 | 产物 CSS 里 `grep -c 'admin-layout\|\.lang-switch'` 为 0（改动 1 后成立） |

### 6.4 运行时片段（fragment）

| # | 检查 | 判据 |
|---|---|---|
| F1 | 片段响应不含 CSS/JS | 片段 HTML 里没有 `<style>` / `<script>`（`builder.go:790-795` 的设计） |
| F2 | 与静态产物同源渲染 | 同一 node id 经 `RenderNodeHTML` 与整页编译产出的 HTML 一致（类名由 `core.NodeClass(id)` 派生，`core/css.go:464`） |
| F3 | 样式依赖宿主页面 | 用「有片段的页面」验证：片段刷新出来的 HTML **有外观**；若页面引用了片段但没带基座，属构建缺陷（`ui_script.go:268` 的三条件） |
| F4 | 能力登记对表 | 新增片段能力必须进 `fragmentStyleGroups` 或 `fragmentUnstyledCapabilities`（`fragment_base.go:69-88`），由 `TestFragmentCapabilityTablesCoverEachOther`（`internal/builder/fragment_base_test.go:122`）与 `TestFragmentStyleTablesCoverRegistry`（`internal/module/runtimefragment/fragment_style_contract_test.go:48`）守住 |

### 6.5 横向（每个消费端都要跑）

| # | 检查 | 判据 |
|---|---|---|
| X1 | 三种视口 | 1440 / 768 / 375 各看一次；窄视口下 `document.documentElement.scrollWidth === clientWidth` |
| X2 | 四种输入 | 鼠标 / 滚轮与触摸板 / 触屏 / 键盘各操作一次（`AGENTS.md` 的交互改动验证清单） |
| X3 | 触屏等价形态 | 依赖 `:hover` 的形态必须有 `AddHoverNone` 等价的触屏形态（`ui.css` 的 `@media (hover: hover)` 在触屏上不输出） |
| X4 | 减少动态效果 | `prefers-reduced-motion: reduce` 下控件与增强动画都关闭 |
| X5 | 键盘可达与焦点可见 | 所有控件可用 Tab 到达、`:focus-visible` 可见、Esc 可关（`<dialog>` 自带） |

---

## 7. 未验证 / 不确定

1. **产物体积下降的实际值未测**：§3.1 的 6041 字节是 `ui.css:539-656` 的原文长度，
   未测 gzip / br 后的差值，也未测全站页数下的总节省。按「未测不宣称收益」的惯例，这里只给原文字节。
2. **`langswitch` 段在真实站点产物里是否已被命中未验证**：本评估只证明「可被命中」（构造输入），
   没有扫存量产物统计实际命中率。若全站无一页写过 `.lang-switch`，它的优先级可以下调。
3. **后台页面的 `.admin-layout` 兜底是否真被消费未验证**：`ui.css:550-551` 的注释自己记了一条待办
   （`login.html` 不在 `.admin-layout` 内、且只引 `theme.css` 不引 `ui.css`，「那 3 个输入框走不到这里」），本次没有在浏览器里逐个复核。
4. **段表新增 `owner` 维度对插件样式的影响未评估**：`ui_css_split.go:17` 说明「未切分源原样返回」是给插件样式的路径；
   `owner` 只对有段标题的源生效，插件自带样式不受影响 —— 但这是从代码读出的推论，未用插件实例实测。
5. **报告提到的 RTL / 读屏 / 触屏覆盖**：本评估没有复核，只在 §6 给出验收清单，不宣称这些能力已经完整。

---

## 附录 A · 复现步骤（只读，不改仓库）

§3.1 / §3.2 / §2.4 的数据来自一个**临时**同包探针（跑完即删，不进提交）。复现方式：

```bash
cd /home/sky/project/go/wp-ui01
# 1. 在 internal/builder/ 下临时建一个 *_test.go（package builder），内容是：
#    · splitUICSS(templates.UICSS()) → 逐段打印 id / trigger / len(text)
#    · uiCSSFor(src, collectHTMLScan('<div class=\'form-group\'><input class=\'form-input\'>')) → 打印长度与 strings.Contains(css, '.admin-layout')
#    · 同上，输入换成 <div class='lang-switch'><select class='lang-select'>
# 2. go test -count=1 -v -run <该探针名> ./internal/builder/
# 3. rm 掉探针文件，git status --short 应无输出
```

实测输出（本次）：

```text
PROBE total-bytes 44188 sections 19
PROBE SECTION head      public    328      PROBE SECTION forms  classes  6481
PROBE SECTION tokens    public   1145      PROBE SECTION langswitch classes 4108
PROBE CASE button-only  bytes 9582   adminLayout false
PROBE CASE form-group   bytes 13335  adminLayout true
PROBE CASE card-only    bytes 6854   adminLayout false
PROBE CASE no-ui-class  bytes 0      adminLayout false
PROBE CASE data-ui-select bytes 6309 adminLayout false wbsTrigger true
PROBE CASE lang-switch  bytes 7761   langSwitch true
```

§3.1 的 6041 字节可用纯 shell 复核（不需要 Go）：

```bash
sed -n '539,656p' internal/templates/static/css/ui.css | wc -c   # → 6041
sed -n '539,656p' internal/templates/static/css/ui.css | wc -l   # → 118
```

## 附录 B · 现有门禁与它们的覆盖缺口

| 门禁 | 覆盖 | 与本票相关的缺口 |
|---|---|---|
| `internal/templates/ui_css_test.go:110` | 基座引用 ↔ 后台别名对表（双向） | 只管令牌，不管**规则**的消费端归属 |
| `internal/templates/ui_css_delivery_boundary_test.go:40` | 别名段位置、共享槽、单端槽登记 | 同上 |
| `internal/templates/backend_css_naming_contract_test.go:133` | 后台静态样式单命名空间 + 引用有来源 + 无死定义 | 它管「后台静态样式不进产物」，但**管不到 `ui.css` 里的后台段** |
| `internal/templates/ui_css_ownership_test.go:97` | 后台兜底必须存在、必须用 `:where`、`.admin-layout` 宿主唯一 | **不检查它是否进了产物**（§3.1 的缺口） |
| `internal/templates/ui_css_ownership_test.go:160` | 抽屉外观在基座、不在 `theme.css` | — |
| `internal/builder/ui_css_split_test.go:76` | 段切分按用量注入、公共段跟随、资源有归属 | 只验证「用到的段」，**没有「某段不该被某些消费端用到」的概念** |
| `internal/builder/ui_feature_crosscheck_test.go` | 渲染期登记 vs HTML 扫描双向一致 | — |
| `internal/templates/ui_assets_test.go:46` | 控制面只经公共入口加载控件、顺序正确、文件都在 | — |
