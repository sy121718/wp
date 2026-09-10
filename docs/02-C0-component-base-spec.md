# 02-C0 · 原子组件通用属性规范 (Component Base Spec)

本文档规范除容器外，所有原子组件（Heading、Text、Button、Image 等）共同继承的通用属性与控制面板。

## 1. 架构逻辑：继承与组合

编辑器 Inspector 面板统一分为两层：

- **内容与专属配置**：每个组件自身的特有功能（如 Heading 的标签等级、Button 的链接跳转、Image 的图片选择）。
- **高级/通用配置（Advanced）**：全站所有原子组件无差别继承的基础控制项。

分层实现原则：**Props 定义各管各的，校验与 CSS 编译共享一份**。容器保留自己的样式体系（02-A 的 layout/box/visual/interaction），原子组件为"专属配置 + `advanced` 字段"；间距、边框、阴影、显隐等重叠子集的校验与编译逻辑在 core 中只写一份（`ValidateAdvanced` / `CompileAdvanced`），保证容器与原子组件、原子组件彼此之间的行为完全一致。

## 2. 通用基础属性清单（每个原子组件都具备）

### 盒模型与间距 (Spacing & Sizing)

- **外边距 (Margin)**：四向独立数值（`{top, right, bottom, left}` 结构化建模，支持面板锁定联动=等值），支持负边距做微叠放（单侧下限 -300px 限幅）；桌面/平板/移动端三端独立响应式。
- **内边距 (Padding)**：四向独立 + 三端响应式（按钮、图文块等内留白组件）；不允许负值。
- **宽度与对齐 (Width & Alignment)**：
  - 自身宽度 `widthMode`：`auto` 自适应内容（默认）/ `full` 铺满父容器 / `fixed` + 自定义宽度值。
  - `alignSelf`：Flex 容器中的自身对齐，覆盖父容器统一对齐（start/center/end/stretch/baseline）。

### 视觉修饰 (Visual Decoration)

- **边框与圆角**：边框三要素（粗细/线型 solid·dashed·dotted·double/颜色，需同时提供）；四角独立圆角（左上/右上/右下/左下，可做不规则圆角）。
- **阴影 (Box Shadow)**：预设 Token（sm/md/lg/xl，与容器 02-A 阴影对齐）。
- **不透明度 (Opacity)**：0~100 百分比。

### 响应式显隐控制 (Responsive Visibility)

- 断点隐藏开关：`hideOn.desktop / tablet / mobile`。三端全开时编译器照常输出（保持哑与确定性），由编辑器层提示。

### 层级与开发者标识 (Attributes)

- **Z-index**：[-100, 100] 有界整数（负边距叠放所需的层级控制）。
- **自定义 Class**：白名单字符，禁止 `sky-` 保留前缀（防碰撞编译产物命名空间）。
- **自定义 Element ID**：锚点跳转用，全文档唯一（复用节点 ID 查重）；与节点类名 `sky-c-<节点ID>` 是两回事（前者进 `id` 属性，后者进 class 做样式挂载）。

## 3. 非目标（相对 WordPress/Elementor 的克制）

- 不提供裸自定义 CSS 框（击穿白名单与确定性构建）。
- 不提供 per-atom transform（rotate/scale）与任意 hover 动画；入场动效仅复用容器 02-A 的两种纯 CSS 预设（fade-in/slide-up，默认关），hover 反馈仅在语义上需要的组件专属层实现（如 Button）。
- 差异化排版靠三端显隐开关，不靠动画。

## 4. 设计收益

- **消除冗余层级**：单个组件即可独立微调边距/边框/显隐，无需为排版强套容器；AST 树深度显著降低。
- **统一 CSS 编译管线**：所有组件的 Margin/Padding/Visibility/Border 走同一份生成逻辑（`core.CompileAdvanced`），产物行为一致、编译高效。

## 5. 实现映射（代码位置）

| 规范条目 | 实现 |
|---|---|
| 结构化四向间距 | `core/base.go` `Spacing`（四向独立、`CSS()` 简写拼接）+ `ResponsiveSpacing` |
| AdvancedProps 数据模型 | `core/base.go` `AdvancedProps`（margin/padding/width/alignSelf/border/radius/shadow/opacity/hideOn/zIndex/customClasses/customId） |
| 共享校验 | `core.ValidateAdvanced`（全原子组件一份规则；自定义 ID 进 ids map 全文档查重） |
| 共享编译 | `core.CompileAdvanced`（三端 bucket + 显隐 display:none 分断点输出） |
| 白名单上移 | `core.IsSafeCSSValue` / `SafeValueRe`（容器与原子组件共用唯一入口） |
| 组件接入样例 | `components/image`（Props 嵌入 `advanced` 字段；自定义 class 织入 img class，customId 注入 id 属性/包裹链接） |

## 6. 组件实现硬约束（2026-09 积累）

> 这些约束都是**产物在真实浏览器/设备上失效**才暴露出来的，违反时构建与测试都不会报错。
> 当前处于开发阶段：与这些约束冲突的既有实现直接改，不留兼容层。

### 6.1 有几何计算的组件必须 `box-sizing: border-box`

逐卡/逐元素几何（扇形收敛、环形半径、每屏高度）一律按 props 里的数值计算。
默认的 `content-box` 下「宽 240px + 内边距 24px」实际外宽 288px，几何整体偏移；
全屏分页更直观 —— 一屏卡片会比视口高出一个内边距，滚动吸附永远对不齐。

### 6.2 CSS 降级链：旧值在前、新值在后

```css
height: 100vh;    /* 旧 —— 先写 */
height: 100dvh;   /* 新 —— 后写，覆盖前者 */
```

反过来写的话新特性会被旧值永久盖掉，等于白写（而且不报错，只是永远走降级分支）。

### 6.3 CSS 属性独占：动画与 transform 不能共用

同一元素上 `animation` 与 `transform` 若都改 transform，后者会被动画顶掉。两条出路：

- **并列多动画**：`animation: a 1s both, b 2s infinite` 配合同样并列的
  `animation-timeline` / `animation-range`，各动画互不干扰（见 cardstack 的切换动画 + 当前屏高亮）；
- **换属性**：循环效果优先挑 `filter` / `opacity` 类词汇（`sky-loop-glow` / `sky-loop-flash`），
  `sky-loop-swing` / `wobble` / `pulse` 等改 transform 的词汇**不能**用在已有位移的元素上。

### 6.4 手势轴：`touch-action` 必须跟着交互轴走

横向交互用 `pan-y`（纵向留给页面滚动）、纵向交互用 `pan-x`。写死一个值会让另一半方向
在触屏上完全失效 —— 手势被浏览器拿去做页面滚动，`pointermove` 根本收不到。

### 6.5 需要 `preventDefault` 时必须 `{ passive: false }`

`wheel` / `touchstart` 等事件默认是 passive 的，那种情况下 `preventDefault()` 会被浏览器
**直接忽略**。另外：非循环场景滚到头要**放行**给页面，否则用户被困在组件里。

### 6.6 动效不要自造：复用 `core/keyframes_animate.go` 的通用词汇

33 条 `sky-*` 已覆盖入场/循环/叙事。组件侧只做「类型 → 词汇名」映射 +
`CSSBuckets.NeedKeyframes(name)` 标记，构建期统一注入且同名只注入一次、按需注入。

### 6.7 模板：单行大文件不要 `read` → `write` 往返

`read` 工具对单行文件有 2000 字符截断，写回时尾部被静默丢弃（曾导致组件模板丢掉外层
`</div>`，产物 HTML 结构破坏、同页后续区块被吞进容器）。**整段重写**，并用标签计数校验配平。

### 6.8 交互组件的验收要覆盖每种输入方式

程序化调用（直接改状态、合成单一事件）覆盖不到「用鼠标滚一下」「用触屏滑一下」。
详见 `AGENTS.md` §测试 的交互改动验证清单。

原子组件接入方式：Props 内嵌 `Advanced core.AdvancedProps`（json: `advanced`），`Validate` 末尾调 `core.ValidateAdvanced(&p.Advanced, node.ID, ids)`，`Render` 中调 `extraClasses, customID := core.CompileAdvanced(node.ID, &p.Advanced, ctx.CSS)` 并把附加 class/ID 织入 HTML。后续 Heading/Text/Button 照此继承。
### 6.9 组件必须适配多端（硬规则，2026-09 确立）

**由来**：cardstack 第一版只在桌面浏览器 + 鼠标下开发验证，上线到手机连爆两个问题 ——
触屏没有悬停导致四种形态完全无响应；卡片宽度与环绕半径写死 px，620px 的卡片塞进
390px 屏幕，文档被撑宽、页面能左右拖。两个问题的共同点是**只在一种环境下验证过**。

适用范围：**所有新建与修改的组件**。产出要在访客的各种设备与各种输入方式下正确工作。

#### 必做的四件事

1. **宽度不写死**：元素的宽度写 `min(100%, <设计宽度>)`，不要只写 `<设计宽度>`。
   容器变窄时它跟着缩，容器够宽时它保持设计值 —— 两端都对。
2. **绝对值必须带上限**：编译期算出的任何 px（半径 / 位移 / 间距 / 尺寸）都可能在窄屏越界，
   用 `min()` / `clamp()` 按视口封顶。上限**要让元素自身尺寸参与计算**：
   `calc((100vw - <元素宽>) / 2 - 边距)`；用经验比例（如 `40vw`）在极端组合下仍会溢出。
3. **触屏是独立环境**：`AddHover` 输出的规则包在 `@media (hover: hover)` 里，**触屏上根本不输出**。
   任何依赖 `:hover` 的形态都必须用 `CSSBuckets.AddHoverNone` 给出触屏等价形态，
   否则手机端这个功能等于不存在。
4. **按压反馈不包 hover**：用 `AddActive`（不带媒体查询），触屏按下同样触发。

#### 可用的机制（不要自己造）

| 需求 | 用什么 |
|---|---|
| 三端各自不同的值（内距/字号/显隐） | `Responsive` 类型 + `core.BreakpointDesktop/Tablet/Mobile` |
| 随容器宽度连续变化 | `min()` / `clamp()` / `cqw`（注意 `container-type` 会困住 fixed 后代） |
| 触屏 vs 鼠标 | `AddHover` / `AddHoverNone` / `AddActive` |

#### 验收（合并前自检）

- **三种视口**各看一次：桌面 1440 / 平板 768 / 手机 375；
- **四种输入**各操作一次：鼠标、滚轮与触摸板、触屏、键盘；
- 窄视口下在浏览器控制台确认**没有横向溢出**：
  `document.documentElement.scrollWidth === clientWidth`，
  且不存在 `getBoundingClientRect().right > clientWidth` 的元素。

### 6.10 无障碍基线（每个组件都必须满足）

产物是静态 HTML，读屏与键盘用户直接面对它。以下约束由
`public/test/builder/unit/a11y_audit_test.go` **自动守着**（遍历全部组件编译产物，新组件自动纳入；
逆向验证过：去掉某个组件的 `alt` 会立刻报红）：

| # | 规则 | 为什么 |
|---|---|---|
| 1 | `<img>` 必须有 `alt` 属性（空值合法 = 装饰性图片） | 缺属性时读屏念文件名 |
| 2 | `<button>` 必须有可访问名（文本或 `aria-label`） | 图标按钮否则只念「按钮」 |
| 3 | `<label>` 内不得出现 `<a>` / `<button>` | 规范禁止；点击不触发放大靠浏览器隐式规则，不可依赖 |
| 4 | 文本类控件必须有关联 label（包裹或 `for`/`id`） | 否则读屏只念「编辑框」，不念字段名 |
| 5 | `role="tab"` 必须挂在可聚焦元素上 | `<label>` 不可聚焦，键盘进不去 |
| 6 | 禁止：重复 id、正 `tabindex`、`aria-hidden` 子树里的可聚焦元素、`<ul>` 直接子元素非 `<li>` | 分别破坏 AT 关联或 Tab 顺序 |
| 7 | 零 JS 方案**不得写静态 ARIA 状态**（如 `aria-selected`） | 状态不跟着切换走就是假状态，比不写更糟 |

**键盘可达是硬指标**：任何「零 JS 交互」都要能用键盘走完 —— 原生 radio group 的方向键、
checkbox 的空格、`<details>` 的 Enter。隐藏控件用 **sr-only**（`position:absolute` + 1px +
`clip-path: inset(50%)`），**不要用 `display:none` / `hidden`** —— 那会把控件踢出键盘序列，
触屏之外全废（tabs 与 nav 折叠菜单都踩过这个坑）。

**实际验证方式**：`a11y_page_fixture_test.go` 会生成含 tabs / nav / form / card / infobox 的
完整走查页（`/tmp/a11y-page.html`），用 CDP 发**真实按键事件**验证方向键切换、空格展开、
`input.labels` 关联 —— 组件级断言只能证明标记结构对，证明不了键盘真的走得通。
