# 02-F · 前端控件基座（已落地）

> 状态：基础能力已实现；2026-09-12 的收敛与剩余问题见 §13。历史验收不代表后续修改自动通过。
> 覆盖：下拉 / 抽屉 / 图标字段 / 确认框 + 模态提示 / 弹窗 / **轻提示** / 明暗切换 + 外观层。

## 1. 这一层解决什么

后台主题设置页的「下拉框点中部不展开」，暴露出一件事：同一个交互能力，项目里有**三处各写一份**。

| 位置 | 形态 | 谁在用 | 覆盖范围 |
|---|---|---|---|
| `static/js/enhance.js` | 普通脚本，构建期**按需内联** | 前台产物（访客） | 8 个**组件级**增强：计数器/轮播/图集/倒计时/灯箱/卡片环/堆叠/全屏分页 |
| `static/js/workbench/` | **ES module** | 只有工作台 | 属性字段、颜色编辑与文档交互（普通下拉已归公共层） |
| ~~`select-enhance.js`~~ | 普通脚本 | ~~只有后台~~ | 已并入基座，删除 |

**根因**：没有"原始控件"这一层。①是组件级能力（轮播要滚、灯箱要开），②只有工作台能加载，
两者都不回答"一个 `<select>` / 一个按钮在任意页面该长什么样、怎么交互"——于是同一个下拉被写了三遍。

## 2. 三层各司其职（现状）

```
③ 组件增强   static/js/enhance.js    轮播/灯箱/卡片环…   按 data-* 特征，构建期按需内联
② 编辑器控件 static/js/workbench/    属性编辑与文档回写   仅工作台（ES module）—— 边界见 §8
① 原始控件   static/js/ui/           下拉/抽屉/确认框/…   后台 + 工作台 + 前台产物**共用一份**
        外观  static/css/ui.css      .wbs-* / .btn / .card / …
```

### 基座清单（①）

| 控件 | 文件 | 声明式用法 | 备注 |
|---|---|---|---|
| 下拉 | `ui/select.js` | 自动接管单选 `<select>`；`data-wb-native`、多选与列表模式保留原生 | 自绘替身 + 动态选项同步；**检查器面板的字段 select 也走它**（原先各写一份） |
| 抽屉 | `ui/drawer.js` | `data-drawer-open` / `data-drawer-title` | 打开时对新内容 `WBUI.scan` |
| 图标字段 | `ui/iconfield.js` | `data-icon-field`（+ hidden 存值）、`data-icon-name` | 图标库 766KB 懒加载 |
| 颜色字段 | `ui/colorfield.js` | `data-color-field` | 文本框是真值来源（可留空/写 `var()`）；**预览两处**：输入框左侧色带 + 右侧色块（色块同时是取色入口） |
| 按钮忙碌态 | `ui/busy.js` | —（纯 API） | `WBUI.busy(btn[, promise][, {label}])`；记住原禁用态，成功失败都恢复 |
| 确认框 | `ui/confirm.js` | `data-confirm` / `-title` / `-ok` / `-cancel` / `-danger` | `<dialog>` 承载，取代原生 confirm；另有 `WBUI.confirm` / **`WBUI.alert`** 给 JS 里调用 |
| 弹窗 | `ui/modal.js` | `data-modal`、`data-modal-open` / `-close` / `-static` / `-nokeyboard` / `-autofocus` | `<dialog>` 承载；`WBUI.modal.open/close`；广播 `wbui:modal-open/close` |
| 轻提示 | `ui/toast.js` | —（纯 API） | `WBUI.toast(msg, {type, duration, dismissible})`；非模态、自动消失，与 `WBUI.alert` 分工 |
| 明暗切换 | `ui/themetoggle.js` | `data-theme-toggle` | 维护 `aria-pressed`；导出 `WBUI.theme` |
| 入口 | `ui/index.js` | 自动 | DOM 就绪扫描 + `htmx:afterSwap` 重扫 |
| 助手 | `ui/_util.js` | — | `ready` / `$$` / `each` / `markOnce` / `register` / `scan` |

### 外观层（`ui.css`）

`.wbs-*`（下拉）、`.wb-confirm-*`+`.is-alert`（确认框/模态提示）、`.wb-modal*`（弹窗）、`.wb-toast*`（轻提示）、`.wbc*`（颜色字段）、`.is-busy`（按钮忙碌态）、`.btn`+`.btn-primary|secondary|ghost|danger|sm|icon`、
`.card-*`、`.data-table`+`.table-wrap`、`.form-*`+`.checkbox`、`.badge-*`+`.dot-*`、
`.pagination-*`、工具类、`.theme-toggle`。

### 一并归位的还有 CSS 效果层

`internal/builder/core/effects.go` 是**组件库外观**那一半：13 类效果词汇表
（surface / border / background / focus / text / motion / image / button / table / loader /
shadow / shape / viewport），组件只声明词汇、编译期出 CSS，颜色全部走 `--sky-*`。

**分工**：`effects.go` 管组件的效果与外观，`ui.css` 管基础控件的外观，`ui/*.js` 管基础控件的行为。
三者都跟主题走。

## 3. 一份实现、三个消费方

```
static/js/ui/*.js + static/css/ui.css
   ├── 后台    admin/layout.html         <link ui.css> + partials/ui_scripts.html
   ├── 工作台  workbench/layout.html     同一脚本入口；复杂字段使用 `WBUI.select.create`
   └── 前台    构建期按 data-ui-* 特征内联（CSS 与 JS **同进同出**）
```

**为什么构建期走 embed 而不是读磁盘**：页面编译发生在运行时，而生产部署可能只带二进制、
磁盘上没有源码树。embed 保证「构建不依赖文件路径」（与组件模板同一条原则）。

**为什么用普通脚本而不是 ES module**：三个消费方里有两个吃不了 `import` ——
后台是 `<script src>`，前台是构建期拼进 `<script>` 正文。②的模块形态只在工作台成立。

**为什么 builder 要注入而不是自己 embed**：`builder` 不依赖 `internal/templates`（后者含 gin 依赖），
所以走 `WithUISources` / `WithUIStyle` / `WithEnhanceSource`（与 `WithComponentSet` 同一条路）。

**为什么控件脚本与样式必须同进同出**：产物内联了脚本却没样式，访客看到的是**没有外观的空壳**。
`uiStyleFor` 与 `uiScriptFor` 用同一套特征判定，纯内容页两个都不注入。

## 4. 变量取法

```css
var(--sky-c-primary, var(--c-primary, 兜底))
     ↑ 站点主题         ↑ 后台配色      ↑ 都取不到
```

- `--sky-*` 由 `ThemeVarsCSS` 注入 → 前台/站点主题下自动跟随；
- `--c-*` 是后台配色（`theme.css`）→ 后台与工作台视觉不变；
- 都取不到时用兜底值 → `ui.css` **不依赖任何其它样式表**，单独引也得到一致外观。

尺寸与动效同理：`:root` 里用 `--ui-sp-*` / `--ui-r-*` / `--ui-tr-*` / `--ui-shadow-*`
桥接到 `--sp-*` / `--r-*` / `--tr-*` / `--shadow-*`（theme.css 有就继承，没有就用自带值）。

## 5. 新增一个控件的标准动作

1. **特征**：组件模板在需要它的元素上打 `data-ui-<控件>`（后台用的控件可以不登记特征）；
2. **登记**：`internal/builder/ui_script.go` 的 `uiBlocks` 加一行（文件名 + 特征）。
   **漏登记 = 用到它的页面静默失去该控件增强**，所以这一步不能忘；
3. **行为**：写 `static/js/ui/<control>.js`，用 `WBUI.register(function (scope) { … })` 登记，
   内部用 `WBUI.each` / `WBUI.$$`，**每个元素第一件事是 `WBUI.markOnce(el, '<Name>')`**；
4. **外观**：写进 `ui.css`，颜色走 `--sky-*`（回退 `--c-*`）；尺寸走 `--ui-*`；
5. **测试**：`ui_script_test.go` 补拼装断言（命中/未命中/源码缺失），
   必要时加产物实测（CDP 真实事件）。

### 三条硬规矩

- **渐进增强**：原生元素保留、表单提交不变、无 JS 时功能不丢。控件只是让交互更可控，
  不是替代原生语义；
- **`markOnce` 必须打**：htmx 局部替换后会重扫，没标记就会在同一元素上叠出第二套菜单；
- **业务逻辑不塞进控件**：抽屉只负责开合与扫描，上级菜单过滤这类规则留在页面自己的脚本里，
  挂在 `wbui:drawer-open` 事件上。

## 6. 迁移过程中发现并修掉的真问题

| 问题 | 原来 | 现在 |
|---|---|---|
| 抽屉里新插入表单的控件**不被增强** | 抽屉内一套原生控件、外面一套自绘 | 打开时 `WBUI.scan(body)` |
| 下拉**不跟随动态选项** | 菜单建一次就完了，用户看到过期列表 | `MutationObserver` 重建 + `choose` 忽略禁用项 |
| 列表图标 htmx 局部刷新后**永远是空的** | 只在页面加载渲染一次 | 随扫描执行 |
| 确认框是**系统对话框 + 内联 JS** | 12 处 `onsubmit="return confirm(…)"` | `<dialog>` 自绘框，模板只留 `data-confirm` |
| 主题切换按钮**读屏读不到状态** | 只有 `title` | 补 `aria-pressed` |
| 焦点顺序 | 先聚焦原生 select（扫描后会变成视觉隐藏元素） | 先扫描再聚焦 |
| 弹层**贴在页面左上角**（不居中） | 只写了 `position: fixed`，靠 UA 样式的 `dialog { margin: auto }` 居中 | `.wb-confirm` / `.wb-modal` 显式写 `position: fixed; inset: 0; margin: auto` —— `theme.css` 的 `* { margin: 0 }` 把 UA 的居中一起清掉了。**确认框也中招**，属迁移时就带进来的回归，只在核对计算样式时才暴露（`margin=0px`、`rect.x=0`） |
| 后台页**另开标签页后写请求全 403** | token 只放在 `body[hx-headers]`（HTMX 用）与登录页写入的 `sessionStorage` 里，而 `media-lib.js` 找的是 `<meta name="csrf-token">`（workbench 有、admin 没有） | `admin/layout.html` 补上 meta，与 workbench 对齐；上传/分类增删改不再依赖 `sessionStorage` |
| 弹窗**按 Esc 关不掉** | 依赖 `<dialog>` 的原生 Esc —— 那是浏览器的 default action，合成的键盘事件不产生它 | 控件自己接管 `keydown` Escape（modal 与 confirm 都是），并按「最上层优先」`stopPropagation`，避免同时开着的抽屉被一起关掉 |
| 上传弹窗的拖拽区**键盘够不到** | `#ml-drop` 是纯 div，只挂了 click | 补 `role="button"` + `tabindex="0"` + Enter/Space |
| 媒体库删除**弹系统原生对话框** | JS 里 12 处 `confirm()` / `alert()`（详情删除、分类删除、已保存、已复制…） | 全走 `WBUI.confirm` 与新增的 `WBUI.alert`（同一个 `<dialog>`）；基座缺席时退回原生，功能不丢 |
| 按钮忙碌态**手写了三遍** | 媒体库生成变体 / 工作台恢复历史 / 工作台保存设置各写一遍，且都漏同一件事：结束时一律 `disabled = false`，把本来就该禁用的按钮错误启用；reject 分支还常忘了恢复 | 收敛成 `WBUI.busy`：记住原禁用态、成功失败都恢复、忙碌中带 `aria-busy` |
| 主题设置页的颜色**只能手敲 hex** | 早先为了「留空 = 跟随内置默认」刻意放弃 `input type=color`（它没有未设置状态，空值会被补成 #000000），代价是没有取色入口 | 基座补 `ui/colorfield.js`：文本框仍是唯一真值来源，旁边色块点开系统取色器；留空显示棋盘格 + 斜线 |
| 操作反馈**只有模态一种强度** | 成功类反馈（已复制/已保存）也弹模态框，用户必须点一下「知道了」—— 打断，却什么都没改变 | 补齐 `WBUI.toast`：非模态、底部居中、3 秒自消；`notify`（轻反馈）与 `notifyError`（失败仍走模态）在页面脚本里分流 |
| 下拉在**检查器面板里是第二份实现** | 基座 `ui/select.js` 跳过 `data-wb-path`，工作台 `controls/selects.js` 用 `wbDropdown` 再升一次级 —— 同一件事（原生 select 在 Linux/Chromium 上「点开即选」）的第三份实现 | 基座接管简单和复杂字段：`upgradeNativeSelects` 扫描原生字段，复杂字段调用 `WBUI.select.create`；单位字段用 `data-wb-native` 显式排除。旧下拉实现与样式已删除 |

### 踩到的坑

| 坑 | 现象 | 现在怎么防 |
|---|---|---|
| 拼装时多套 `<script>` | 产物里明明有基座代码，运行时 `WBUI` 是 undefined——外层模板已套 `<script>`，再套一次成了嵌套，整段语法错误 | `uiScriptFor` 只返回正文；测试断言禁止出现 script 标签 |
| `aria-selected` 被当违规 | 走查页断言"tabs 不该输出 aria-selected"误伤基座（它合法使用该属性） | 判据改用 ARIA tab 专属词 |
| 增强源码曾是包级变量 | 测试不注入也有值，改成注入后 golden 立刻漂移 | `document.html` golden 与改动前**逐字节一致** |
| 跳过判定写在 `markOnce` **之后** | 被跳过的元素也留下了「已增强」标记，将来解除跳过时它们再也不会被增强，且标记只在 DOM 上、代码里看不出来 | 跳过判定必须在 `markOnce` 之前 —— 迁检查器下拉时正是这一条挡住了 `data-wb-native` 的元素（现在它们无标记） |
| air 不监听 js/css | 改磁盘文件后台立刻生效、产物却是旧版 | `.air.toml` 的 `include_ext` 加上 js/css |

## 7. 验证方式

- **像素级**：控件样式搬家前后，在同一页面上对比 16 项计算样式（按钮 4 种/卡片/表格 3 项/
  徽章 2 项/表单 2 项/分页 3 项/主题按钮）—— 零差异；
- **行为**：CDP 发真实鼠标与键盘事件（点中部展开、Enter/↓/Enter 选中、Esc 关闭、
  取消不提交、确定真提交、抽屉内控件被增强、动态选项重建）；
- **产物**：golden 逐字节比对 + 产物内联内容断言 + `go test ./...`。

## 8. 暂不做

- **schema 与复杂字段下拉均接入基座**：`controls/selects.js` 为原生字段设置通用状态键并触发 `WBUI.scan`，选后经 `panel.onchange` 回写 AST；复杂字段使用 `WBUI.select.create`，选后通过工作台 `commit` 回写。
  属性编辑逻辑仍应留在工作台，两类原因：
  · **检查器字段构造器**（`base.field` / `spacing.*` / `color` / `corners` / `media`）：
    API 形如 `(ctx, label, path)` —— 面向 AST 数据路径与 `[data-wb-path]` 回写委托，
    是「属性表单的构造器」，不是页面上的元素；
  · **组件形状编辑器**（`repeater.js` 的 `faqPanel`/`bindRepeaterPanel`/`navPanel`…、
    `misc.schemaField`）：那是**组件知识**（每个组件一种字段形状），进基座等于把组件库塞进
    控件层。
  **判据**：它回答的是「这个元素长什么样、怎么交互」，还是「这个组件的这个属性该怎么填」——
  前者进基座，后者留在工作台。
- **`effects.go` 里的 ◻️ 项**（噪点/极光/逐字入场/模糊过渡/悬停展开/断点变量化）。
- ~~**媒体库不进基座**~~ → **边界已细分**：业务脚本（`media-lib.js` 是 API 客户端与渲染工具库、
  `media-admin.js` 是媒体库页业务逻辑）不进基座 —— 硬搬会把应用逻辑塞进控件层；
  但页面里的**原始控件该进**：两个弹窗已改用 `ui/modal.js`（`<dialog>` + `data-modal-*`），
  `media-admin.js` 只剩「先渲染好选项再打开」「确认后调接口」这类业务动作。

## 9. 关于"能不能不用 JS"

常被问到的三个替代方案，结论写在前面：**都不能整体替代**。

- **Jet（构建期）**：它算的是"此刻页面该长什么样"，产物即固化。而增强要的是访客的当下
  （滚到哪、拖多远、现在几点）——不是能力问题，是构建期根本拿不到那个变量；
- **HTMX**：它解决"去服务器拿一段 HTML 换进来"，不碰客户端瞬时状态。而且前台是纯静态
  产物、访问面不查库不执行模板，用 HTMX 做倒计时等于每秒打后端，架构上不成立。
  HTMX 的正确位置是后台（已用）与前台白名单 Runtime Fragment（库存/购物车那类）；
- **CSS**：能替一部分——`scroll-snap` 做全屏分页/部分轮播很干净，`@property` 能做数字滚动。
  但代价通常是**DOM 复杂化 + 可访问性退步**（纯 CSS 交互几乎都要 radio+label 或 `:target`
  这类 hack，cardstack 与 tabs 当初就因此丢了键盘可达）。判据是**DOM 语义不退化**，
  而不是"能省则省"。

**倒计时（需要真实当前时间）与拖拽（需要指针事件）永远得留 JS。**

---

## 10. 原始控件层：类名、硬规则与迁移进度

> 这一节是「新页面该用什么类」的唯一依据。写页面时先看这里，别再看别的页面抄。

### 10.1 为什么会有这一节

控件视觉一度是**绑在容器类上**的（`.pages-form input { … }`）。后果是：**不套那个容器就没样式**。
画布页的侧栏用了自己的 `.auto-form` 容器，输入框就退化成了浏览器默认外观 ——
看起来像「掉样式」，实际是「没进容器」。这类问题每写一个新页面就复现一次。

同一个控件还被写了十几遍：`.pages-form input` / `.attr-form-head input` / `.locale-add input` /
`.theme-font-input` …，字号各不相同（13 与 14），focus 态只有一份，
而主按钮的颜色甚至有两套 —— `.pages-form .btn-primary` 写死蓝色 `#2563eb`，
而 `--c-primary` 其实是深灰 `#3d444f`。**同一个主题下，表单里的主按钮和别处的主按钮颜色不一样。**

### 10.2 类名清单（唯一来源：`static/css/ui.css`）

| 控件 | 类名 | 说明 |
|---|---|---|
| 文本输入 | `form-input` | 含 `:focus` / `:focus-visible` / `:disabled` / `[aria-invalid=true]` |
| 下拉 | `form-select` | 同上 |
| 多行文本 | `form-textarea` | 同上，`resize: vertical` |
| 字段容器 | `form-group` | 下边距 |
| 标签 | `form-label` | 必填标记用内部 `<span class="req">` |
| 提示 / 错误 | `form-hint` / `form-error` | 小字说明 |
| 行内多列 | `form-row` | 一行放几个字段 |
| 复选框 | `checkbox` | 包 `<input type="checkbox">` |
| 按钮 | `btn` + `btn-primary` / `btn-secondary` / `btn-ghost` / `btn-danger` / `btn-sm` / `btn-icon` | `btn` 是基类，必须带 |
| 卡片 | `card` + `card-header` / `card-title` / `card-body` / `card-footer` | |
| 表格 | `data-table` | |
| 徽标 / 状态点 | `badge(-success/-warning/-danger/-mute)` / `dot(-…)` | |

工具类：`w-full` `text-sm` `text-xs` `text-mute` `text-right` `mt-*` `mb-*` `gap-*` `flex` `items-center` `justify-between`。

### 10.3 硬规则

1. **新页面直接用类**：`<input class="form-input">`、`<select class="form-select">`、`<button class="btn btn-primary">`。
   **不要**写「容器选择器给内部控件上样式」（`.xxx-form input { … }`）—— 那是掉样式的根源，
   而且同一个控件会被重写很多遍。
2. **容器类只写布局**：宽度、间距、排列、栅格可以写；边框 / 圆角 / 内边距 / 字号 / 焦点态**不写**。
3. **视觉只有一处定义**：`ui.css`。发现某处视觉不一致时改 `ui.css`，不要在页面里覆盖。
4. **语义色走变量**：`var(--c-primary)` / `var(--c-danger)` …，不要写死十六进制。
   主题切换与暗色模式依赖它们（写死色的那些规则在暗色下必然错）。
5. **多端**（与组件同一条硬规则）：输入宽度写 `min(100%, <设计宽度>)`；
   触屏只依赖原生控件（不要自造）；键盘焦点必须可见（`:focus-visible` 已在基座里）。

### 10.4 存量桥接与迁移进度

存量页面（约 630 个控件、540 个仍靠容器类上样式）**不可能一次改完**，所以 `ui.css` 里有一组
**桥接选择器**：把 `.pages-form input` / `.attr-form-head input` / `.locale-add input` 等容器选择器
列进**同一组视觉规则**。

于是：

- 视觉仍然只有一处定义（没有第二套值）；
- 存量页面**不会掉样式**；
- 新页面用类名，天生不依赖容器。

**桥接只为存量，不再扩大使用面。** 逐页迁移的做法：给控件加 `class="form-input"` / `form-select` /
`form-textarea`，然后把该页容器规则里的**视觉部分删掉、只留宽度**。

已迁移：画布侧栏（`mail_automation_canvas.html`）。
待迁移：product / inventory / masterdata / theme / navigation 等页面（迁移时逐页浏览器验证，
不要一次全改 —— 一处视觉回归在几十个页面里很难定位）。

### 10.5 验证方式

改完控件样式必须**在浏览器里读计算值**，不能只看自己写的 CSS：

```js
getComputedStyle(document.querySelector('.form-input')) // padding / borderRadius / fontSize / borderColor
```

再配合三种视口（1440 / 768 / 375）与四种输入（鼠标 / 滚轮 / 触屏 / 键盘）各过一遍。
本项目多次出现「核对了自己写的配置、没核对系统实际做的事」导致的误判。

另一个常见陷阱：**改完 CSS 记得硬刷新**（`Page.reload { ignoreCache: true }`）——
浏览器缓存会让你以为改动没生效。

---

## 11. 存量迁移：收益、风险与决策

> 这一节回答「要不要把 500 多处靠容器类的控件全迁到类名」。结论：**值得迁，但必须分批，且桥接最终必须删除**。

### 11.1 先看一个反直觉的事实：桥接已经压住了存量规则

`ui.css` 的桥接选择器写成：

```css
.pages-form input:not([type="checkbox"]):not([type="radio"]):not([type="hidden"])
```

`:not()` 的**特异性等于其参数的特异性**，三个 `:not` 叠加后这组选择器的特异性是 **0,4,1** ——
远高于 theme.css 里 `.attr-form-head input[type="text"]`（0,2,1）这类存量规则。

于是：**存量那些规则其实已经失效了**。

实测（`/admin/product-attributes`，`.attr-form-head input[type=text]`）：

| | 规则里写的 | 浏览器实际计算值 |
|---|---|---|
| padding | `8px 10px` | **`8px 12px`** |
| border-radius | `8px` | **`6px`** |

实际值来自基座 —— 也就是说**视觉确实统一了**，但统一的方式是「靠 `:not` 堆出来的特异性把旧规则压住」，
旧规则变成了**看起来在工作、实际已失效的死代码**。

### 11.2 收益

| 收益 | 说明 |
|---|---|
| **消除死规则** | 存量里十几处「写着视觉但已被压制」的规则，读代码时会**误导后来者**（改它没效果，却看不出为什么） |
| **让局部覆盖重新可行** | 现在任何页面想微调（`.my-page input { padding: 4px }`，特异性 0,1,1）都会被桥接（0,4,1）压掉 —— **改不动**。迁移后特异性回到 0,1,0，局部覆盖恢复正常 |
| **降低脆弱性** | 当前的一致性依赖「`:not` 叠出来的特异性 + 加载顺序」这种巧合，不是结构保证 |
| **新页面** | 与迁移无关 —— 用类名的页面已经不受影响 |

**注意**：收益**不包括**「消除视觉不一致」—— 视觉早已统一（见 11.1）。

### 11.3 风险（三个，都是真的）

**① 存量规则里视觉与布局混写。** 例如：

```css
.attr-form-head input[type="text"] {
    padding: 8px 10px; font-size: 13px; border: 1px solid …; border-radius: 8px;  /* 视觉：删 */
    width: min(100%, 220px); min-width: 0;                                    /* 布局：留 */
}
```

删除时必须**逐条区分**，把布局部分留下。一刀切删会产生窄屏溢出这类回归。

**② 级联：删一条会露出下一条。** 存量规则分散在多个特异性层级
（`.purchase-page .attr-form-head input[type="text"]`、`.attr-form-head input`、`.attr-value-row input` …），
删掉高的那条，低的就浮上来。所以**不能只删一半**。

**③ 没有视觉自动化测试。** `go test` 只覆盖模板「渲染得出来」，**不检查任何视觉**。
所以每一次迁移的验收都只能靠**在浏览器里逐页读计算值**（见 §10.5）。
存量约 630 个控件分布在 35 个页面 —— 这是一次性的人工成本，也是风险的主要来源。

### 11.4 决策

**做，但按这个节奏：**

1. **不搞一次性大迁移**。按模块分批（product → inventory → masterdata → theme/navigation），
   每批：给控件加类名 → 删该模块容器规则里的**视觉部分（保留布局）** → 浏览器逐页读计算值 → 单独提交；
2. **优先迁还在活跃开发的模块**（product / inventory 最近都在改）；稳定模块最后迁；
3. **桥接最终必须删除**。它不是「兼容层」而是「特异性陷阱」：
   留着意味着**任何页面都无法局部覆盖控件样式**，而且旧规则永远是死代码。
   迁完最后一批就把它删掉 —— 删掉之后 `ui.css` 才是真正意义上的「唯一视觉来源」；
4. **防新增比迁移更划算**：code review 里拦住「又写了 `.xxx-form input { … }`」这类容器选择器，
   比事后迁 500 处便宜得多。这才是长期成本的真正来源。

**不迁的代价**（如果决定停在这里）：

- 视觉仍是一致的，**不会掉样式**，新页面也正常；
- 但「改不动存量控件样式」这个坑会留着，且存量规则会持续误导读者；
- 所以更准确的说法是：**不迁不是错误，只是把债留在记账上**。

### 11.5 迁移时的检查清单

- [ ] 加 `class="form-input"` / `form-select` / `form-textarea`（**排除** hidden / checkbox / radio / file / color / range / submit / button）
- [ ] 删该页容器规则里的**视觉**，**保留宽度等布局**
- [ ] 逐页浏览器读 `getComputedStyle`（padding / borderRadius / fontSize / borderColor）
- [ ] 三视口（1440 / 768 / 375）确认无横向越界
- [ ] 暗色主题下看一眼（写死颜色的规则在暗色下会错）
- [ ] 单独提交，便于出问题时回滚单页

## 12. 后台页面 vs 站点组件：同一份基座，两种投递

> 写代码前先确认自己在哪个世界 —— **源是同一份**，但**投递方式不同**，这决定了「能不能直接用控件类」。

### 12.1 同一份源，两种投递

`static/js/ui/` 与 `static/css/ui.css` 是**唯一一份**原始控件基座，两个世界都用它：

> `ui/index.js` — 原始控件基座入口（**后台页面直接引用；前台由构建期内联**）。
>
> 源文件在 `internal/templates/static/js/ui/`：**运行时经 /static 给后台页面，构建期由装配层读出后注入**
> （builder 不依赖 internal/templates）。

| | 后台页面 | 站点组件（前台产物） |
|---|---|---|
| **源** | `static/css/ui.css` + `static/js/ui/*` | **同一份**（`//go:embed static/css/ui.css`） |
| **投递** | `admin/layout.html` 用 `<link>` / `<script>` **常驻加载** | 构建期**按需内联**进产物 |
| **触发条件** | 无 —— 打开后台就都有 | 产物 HTML 命中 `data-ui-*` 特征 |
| **命中后的效果** | 任何控件类都可用 | **整份** ui.css 注入 → 所有控件类都可用 |
| **没命中** | 不适用（总是命中） | **一个都没有** —— 写了 `.form-input` 也没样式 |

### 12.2 触发条件是 `data-ui-*` 特征，不是 class 名

这是最容易踩错的地方。`internal/builder/ui_script.go` 的机制是「**特征命中才注入**」：

```go
var uiBlocks = []uiBlock{
    {file: "select.js", feats: []string{"data-ui-select"}},
    {file: "modal.js",  feats: []string{"data-modal"}},   // 同时命中 data-modal-open / -close
}
```

- **JS**：按 `data-ui-*` 特征逐个控件注入（源文件来自 `js/ui/`）；
- **CSS**：`uiStyleFor()` 只判断「**有没有任何控件命中**」，命中就注入**整份** ui.css ——
  **它不看 class 名**。

官方注释解释了为什么必须同进同出：

> 控件脚本进了产物却没样式，访客看到的就是没有外观的空壳 —— 所以两者必须同进同出。

反面也有兜底：纯内容页（一个特征都没命中）**不注入任何东西**，不为空增强付流量。

### 12.3 所以：怎么用

**后台页面**（`internal/templates/admin/*.html`）—— 直接用类即可，**总是可用**：

```html
<form class="pages-form" method="post" action="/api/xxx/save">
  <input type="hidden" name="csrf_token" value="{{ .["csrf_token"] }}">
  <p class="w-full">
    <label class="form-label" for="f-name">名称</label>
    <input class="form-input" id="f-name" type="text" name="name" required>
    <span class="form-hint">显示在列表页的标题</span>
  </p>
  <p class="w-full"><button type="submit" class="btn btn-primary">保存</button></p>
</form>
```

- `btn` 是**基类必须带**（`class="btn btn-primary"`）；
- `<select>` 由 `select.js` **自动接管**（增强后类名变成 `form-select wbs-native`）；
  后台**不需要**写 `data-ui-select` —— 那个标记是给**构建期**判断「产物要不要内联」用的；
- htmx 片段由 `htmx:afterSwap` → `WBUI.scan` 自动重扫，不用手动初始化。

**站点组件**（`internal/templates/components/*.jet`）—— 分两种情况：

1. **组件自己的外观** → 用组件级类名（`sky-*`，如 `sky-form-field` / `sky-form-submit`）
   走 `core.CSSBuckets` 编译。这些**总是**在产物里，不用操心注入；
2. **想用基座控件**（下拉替身 / 弹窗 / 以及它们的样式）→ 在模板上写 `data-ui-select` / `data-modal`。
   命中后 ui.css **整份**注入，于是 `.form-input` / `.btn` 这些类**也能用**。

**不要**只写 `.form-input` 而指望有样式 —— class 名不触发注入，没触发就是「类在、样式不在」。

### 12.4 一句话判据

> **源是同一份**；差别只在投递。
> 后台：常驻加载，随便用。产物：命中 `data-ui-*` 才注入 ui.css，所以**先有标记、再有样式**。

多端硬规则对两者同样适用（见 §10.3 第 5 条）：宽度 `min(100%, …)`、触屏用 `AddActive` 给按压反馈、
`AddHover` 的规则在触屏上不输出必须补等价形态。
### 12.5 动效系统不在这份基座里（与控件的关键差异）

控件是「**一份源、两种投递**」；**动效不是** —— 它只属于产物侧。

| | 控件基座 | 动效系统 |
|---|---|---|
| **源** | `static/css/ui.css` + `static/js/ui/*` | `internal/builder/core/effects.go` + `keyframes_animate.go` |
| **后台页面** | ✅ 常驻加载，任何控件类可用 | ❌ **用不到** |
| **站点产物** | ✅ 命中 `data-ui-*` 后整份注入 | ✅ 编译进产物 CSS |
| **谁写的** | 手写 CSS / JS | **Go 编译期从 props 生成**（`InteractionProps` → keyframes） |
| **是否共用** | **是** | **否** |

动效的实际规模（`builder/core`）：入场 ×40（含 Animate.css 拆解 24 词）、循环 ×17（拆解 7 词）、
悬浮 ×8（触屏治理包 `@media (hover: hover)`）、滚动触发、吸顶、`prefers-reduced-motion` 无障碍。
全部走 `InteractionProps` + `ValidateInteraction` 白名单，由构建期编译成产物 CSS。

**后台自己只有 3 个反馈动画**（不是动效系统）：`wb-toast-in`（提示弹入）、`wb-busy-spin`（加载转圈）、
`wb-panel-in`（工作台面板）。且 `ui.css` 同样守 `@media (prefers-reduced-motion: reduce)`。

所以：**在后台页面上写动效词（如 `fade-up`）不会生效** —— 那是产物的词汇表。
后台要动效只能按需手写 CSS（目前只有上面那三个反馈动画）。

> 文档状态易误读：`docs/02-F-motion-animation.md` 写「选型评估完成，**未落地**」指的是
> **第三方动画库（GSAP 等）**未落地；**自建的 CSS 动效词汇表已经落地**（`builder/core` 里有实现与全量测试）。
两件事不是一回事。

## 13. 2026-09-12 基础收敛

控制面的脚本清单归到 `internal/templates/partials/ui_scripts.html`：后台、工作台和验证页共享，按助手、控件、扫描入口顺序加载。访问面继续按能力裁剪，不能直接引入整套控制面资产。

公共外观补齐 `.wb-btn` / `.wb-icon-btn` 与复选框：工作台通过密度、布局和状态规则调整；输入框的全宽规则排除 checkbox/radio，避免重复项中的「默认展开」被拉成整行。schema select 补可访问名称，增强层同步禁用态。下拉边框损坏与暗色 `--c-surface` 缺失已修复。

控件微动效在 `ui.css` 定义 `--ui-duration-fast/base`、`--ui-ease-out/spring`，组合成 `--ui-motion-fast/base`。面板与 toast 共享 `wb-ui-enter`；toast 退出清理由 `WBUI.transitionTime` 读取 CSS 过渡时间，不再手写第二份毫秒值。减少动态效果设置下关闭这些动画。组件的动效词汇、关键帧白名单与产物编译仍属于构建器，边界不变。

`WBUI.scan` 不再吞掉初始化异常：记录日志、派发 `wbui:error`、返回错误集合并继续其它控件。检查器调用方据此显示就地提示。公共入口和工作台整页渲染有测试，JS 强制按正确脚本模式解析，检查命令为 `bash scripts/check-workbench.sh`。

复杂面板的 `wbDropdown` 与 `.wb-dd-*` 已删除。`WBUI.select.create(choices, current, {key, label, onChange})` 返回 `{root, value, open, close}`；程序赋值只同步外观，真实选择才派发 input/change，同值选择不重复提交。`data-ui-key` 用于重建前后的展开态恢复，控件层不解释工作台字段路径。

下拉使用 WeakMap 记录实际节点的实例，克隆外观后重新扫描会重建行为，不会照抄「已增强」标记；重复扫描不增加触发器。动态选项文字、禁用、隐藏以及禁用选项组会同步，方向键跳过不可选项。多选和列表模式保留原生，原生表单 reset 后同步可见值。快捷键保护改为识别公共 `.wbs`，焦点留在下拉按钮时 Delete 不会误删组件。

本批验证包含公共控件事件回归、工作台模块加载和真实面板交互：原生与复杂字段选择、禁用项跳过、长菜单滚轮（scrollTop 从 0 到 340）、整块克隆重扫、展开态跨重建恢复（控件 ID 确实变化）、动效字段修改后的编译与撤销/重做。375 / 768 / 1440 视口均核对无横向溢出。浏览器工具未提供触屏手势，本批没有把真实触屏验证记为通过。

仍需继续清理后台 `pages-*` 桥接类、工作台部分输入框覆盖与复杂属性表单生成。
