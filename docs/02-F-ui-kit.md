# 02-F · 前端控件基座（已落地）

> 状态：**已实现**（2026-09）。`go test ./...` 全绿。
> 覆盖：下拉 / 抽屉 / 图标字段 / 确认框 + 模态提示 / 弹窗 / **轻提示** / 明暗切换 + 外观层。

## 1. 这一层解决什么

后台主题设置页的「下拉框点中部不展开」，暴露出一件事：同一个交互能力，项目里有**三处各写一份**。

| 位置 | 形态 | 谁在用 | 覆盖范围 |
|---|---|---|---|
| `static/js/enhance.js` | 普通脚本，构建期**按需内联** | 前台产物（访客） | 8 个**组件级**增强：计数器/轮播/图集/倒计时/灯箱/卡片环/堆叠/全屏分页 |
| `static/js/workbench/` | **ES module** | 只有工作台 | 13 个编辑器原语（`wbDropdown` / `wbColorPicker` …） |
| ~~`select-enhance.js`~~ | 普通脚本 | ~~只有后台~~ | 已并入基座，删除 |

**根因**：没有"原始控件"这一层。①是组件级能力（轮播要滚、灯箱要开），②只有工作台能加载，
两者都不回答"一个 `<select>` / 一个按钮在任意页面该长什么样、怎么交互"——于是同一个下拉被写了三遍。

## 2. 三层各司其职（现状）

```
③ 组件增强   static/js/enhance.js    轮播/灯箱/卡片环…   按 data-* 特征，构建期按需内联
② 编辑器控件 static/js/workbench/    12 个检查器原语      仅工作台（ES module）—— 边界见 §8（下拉已归 ①）
① 原始控件   static/js/ui/           下拉/抽屉/确认框/…   后台 + 工作台 + 前台产物**共用一份**
        外观  static/css/ui.css      .wbs-* / .btn / .card / …
```

### 基座清单（①）

| 控件 | 文件 | 声明式用法 | 备注 |
|---|---|---|---|
| 下拉 | `ui/select.js` | 自动接管原生 `<select>`；`data-wb-native` 与 `.wb-dd-src` 跳过 | 自绘替身 + 动态选项同步；**检查器面板的字段 select 也走它**（原先各写一份） |
| 抽屉 | `ui/drawer.js` | `data-drawer-open` / `data-drawer-title` | 打开时对新内容 `WBUI.scan` |
| 图标字段 | `ui/iconfield.js` | `data-icon-field`（+ hidden 存值）、`data-icon-name` | 图标库 766KB 懒加载 |
| 颜色字段 | `ui/colorfield.js` | `data-color-field` | 文本框是真值来源（可留空/写 `var()`），色块只是取色入口 |
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
   ├── 后台    admin/layout.html         <link ui.css> + 7 个脚本（_util → 各控件 → index）
   ├── 工作台  workbench/layout.html     同上（wbDropdown 靠 data-wb-path 被自动跳过）
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
| 下拉在**检查器面板里是第二份实现** | 基座 `ui/select.js` 跳过 `data-wb-path`，工作台 `controls/selects.js` 用 `wbDropdown` 再升一次级 —— 同一件事（原生 select 在 Linux/Chromium 上「点开即选」）的第三份实现 | 基座接管检查器面板：`upgradeNativeSelects` 只触发一次 `WBUI.scan`；7 个 `wb-unit-select` 用 `data-wb-native` 显式排除，`wbDropdown` 的隐藏载体 `.wb-dd-src` 也被跳过 |

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

- **工作台原语：13 个 → 12 个**。**下拉已经迁完** —— `controls/selects.js` 的
  `upgradeNativeSelects` 现在只触发一次 `WBUI.scan`，实现只剩基座一份（选后派发冒泡
  change，仍由检查器既有的 `panel.onchange` 委托回写 AST，回写通道一个没变）。
  剩下的 12 类**不是「暂不迁」，而是不该迁**，两类原因：
  · **检查器字段构造器**（`base.field` / `spacing.*` / `color` / `corners` / `media`）：
    API 形如 `(ctx, label, path)` —— 面向 AST 数据路径与 `[data-wb-path]` 回写委托，
    是「属性表单的构造器」，不是页面上的元素；
  · **组件形状编辑器**（`repeater.js` 的 `faqPanel`/`tabsPanel`/`accordionPanel`/`navPanel`…、
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
