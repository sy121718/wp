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
- **自定义 Class**：白名单字符，禁止 `wp-` 保留前缀（防碰撞编译产物命名空间）。
- **自定义 Element ID**：锚点跳转用，全文档唯一（复用节点 ID 查重）；与节点类名 `wp-c-<节点ID>` 是两回事（前者进 `id` 属性，后者进 class 做样式挂载）。

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

原子组件接入方式：Props 内嵌 `Advanced core.AdvancedProps`（json: `advanced`），`Validate` 末尾调 `core.ValidateAdvanced(&p.Advanced, node.ID, ids)`，`Render` 中调 `extraClasses, customID := core.CompileAdvanced(node.ID, &p.Advanced, ctx.CSS)` 并把附加 class/ID 织入 HTML。后续 Heading/Text/Button 照此继承。