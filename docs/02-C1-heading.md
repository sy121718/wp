# 02-C1 · 标题组件规范 (Heading Component Spec)

本文档规范 `core.heading`（标题组件）的功能定位、特有属性、CMS 绑定逻辑与编译输出规则。基础盒模型与通用样式继承自 `02-C0`。

## 1. 架构定位

- **组件标识**：`core.heading`
- **策略归属**：`BuildStatic`（Go Publish Compiler 编译期直出纯静态 HTML，零客户端 JS）。
- **核心职责**：用于全站主副标题、区块大字、卡片标题等短文本场景，严格控制 SEO 语义层级与文字排版。

## 2. 特有属性与配置描述

### SEO 与语义控制

- **HTML 语义标签**：`h1` ~ `h6`，以及 `div` / `span`（纯视觉大字，避免干扰整页 SEO 结构）；默认 `h2`。

### 字体排版与响应式 (Typography)

- **字号与行高**：桌面/平板/移动三端独立；支持 `px` / `rem` / `em` / `vw` / `clamp()` 流式字号（白名单正则校验）。
- **字重**：主题 Token（regular=400 / medium=500 / semibold=600 / bold=700）或直接数值（100~900，步进 100）。
- **字间距**：`letterSpacing`（长度值白名单）。
- **文字转换**：none / uppercase / lowercase / capitalize。
- **文字对齐**：left / center / right / justify（三端独立）。
- **文本装饰**：underline / line-through + 装饰线颜色微调。

### 视觉修饰与截断

- **文字颜色**：色值或主题 Token（`var(--color-primary)`）。
- **多行截断 (Line Clamp)**：1~6 行，标准 `-webkit-box` 组合（`display: -webkit-box` + `-webkit-line-clamp` + `-webkit-box-orient: vertical` + `overflow: hidden`）。
- **文字阴影**：预设 `subtle` / `strong`（复杂背景图可读性）。

### CMS 数据绑定 (Dynamic Binding)

- **绑定字段映射**：白名单字段路径（`post.title` / `product.name` / `category.name` 等，正则 `^[a-z][a-z0-9_]*\.[a-z][a-z0-9_]*$`）。
- **后备默认值 (Fallback)**：绑定字段为空时回退 Fallback；Fallback 为空则回退静态文本 `text`。
- 解析契约：`core.ContentResolver.ResolveString(field)`，编辑/编译上下文注入（`WithContentResolver`），未注入时绑定节点编译报明确错误。

## 3. 继承自 02-C0 的通用能力

三端 Margin/Padding（四向独立）、Align-self、宽度模式、边框/圆角/阴影/透明度、三端显隐、自定义 Class（禁 `wp-` 前缀）与 Element ID（锚点，全文档唯一）。

## 4. 编译输出规则

- 单层语义标签，无外层包装节点；样式的 Tailwind 类（text-2xl/font-bold 等）仅为规范示例写法，实际产物以编辑器内联的 `CompliedPage.CSS` 输出等价纯净 CSS（`font-size`/`font-weight`/`line-clamp` 等），HTML 端只保留节点类名与自定义 class。
- **静态文本**：`<h2 class="wp-c-h1 custom-heading">核心产品特性介绍</h2>`
- **CMS 绑定**（发布期已静态填入）：`<h1 class="wp-c-h1">2026 年度旗舰款无线降噪耳机</h1>`

## 5. 实现映射

| 规范条目 | 实现 |
|---|---|
| 组件数据模型与校验 | `internal/builder/components/heading/heading.go`（Props/Typography/Decor/Binding + 白名单校验） |
| CMS 绑定解析契约 | `internal/builder/core/render.go` `ContentResolver` + `Compile` 选项 `WithContentResolver` |
| 三端排版编译 | `heading.go` `compileCSS`（桌面默认 + 平板/手机媒体查询） |
| 多行截断 | `-webkit-box` 四件套规则 |
| 单元测试 | `public/test/builder/unit/heading_test.go`（6 组 + 12 个校验拒绝场景 + XSS 转义） |

内容安全：文本统一 `html.EscapeString` 转义，杜绝脚本注入。