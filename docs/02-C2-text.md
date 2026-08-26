# 02-C2 · 正文组件规范 (Text / RichText Component Spec)

本文档规范 `core.text`（正文组件）的功能定位、特有属性、模式切换逻辑、CMS 绑定及编译输出规则。基础盒模型与通用样式继承自 `02-C0`。

## 1. 架构定位

- **组件标识**：`core.text`
- **策略归属**：`BuildStatic`（Go Publish Compiler 编译期直出纯静态 HTML，零客户端 JS）。
- **核心职责**：用于短说明、副标题描述、段落长文、产品详情等各类文本承载场景。

## 2. 特有属性与配置描述

### 文本模式切换 (Content Mode)

- **富文本模式 (richtext，默认)**：长篇介绍、产品说明、多段落/列表/链接长文；编辑器提供加粗/斜体/下划线/删除线/代码/有序无序列表/引用块/行内超链接（`target="_blank"` + `rel="nofollow"`）；编译产物为语义 HTML 片段（内部 `<p>`/`<ul>`/`<blockquote>` 等）。
- **纯文本模式 (plaintext)**：卡片副标题、简短提示、按钮下方小字、单行说明；纯文本输入框无格式工具条；标签可选 `<p>`/`<span>`；编译产物单层直出，零多余嵌套。

### 排版样式与排布 (Typography)

- 基准字号/行高：三端独立（`px`/`rem`，白名单校验）。
- 段落间距：富文本模式统一控制段落上下留白（作用于内部 `p`/`ul`/`blockquote`）。
- 文本对齐：左/中/右/两端，三端独立。
- 文字与链接颜色：色值或主题 Token。

### 多行截断与摘要 (Clamp & Excerpt)

- Line Clamp：1~10 行，超出省略号（标准 `-webkit-box` 四件套）。
- 摘要模式：富文本绑定长文时 strip 全部标签取纯文本，截取前 N 字符（上限 400），仅限 richtext + binding 场景。

### CMS 数据绑定与安全过滤 (Dynamic Binding & Security)

- 字段映射：纯文本绑定字符串字段（`post.excerpt`/`category.description`）；富文本绑定正文字段（`post.content`/`product.description`），且富文本结果始终经 **XSS 白名单清洗**。
- Fallback：绑定字段为空时的兜底文本。

## 3. 继承自 02-C0 的通用能力

三端 Margin/Padding、Align-self、宽度模式、边框/圆角/阴影/透明度、三端显隐、自定义 Class（禁 `wp-` 前缀）与 Element ID。

## 4. 安全清洗规则（编译期）

| 类别 | 规则 |
|---|---|
| 标签白名单 | `p br strong b em i u s del code ul ol li blockquote a h2 h3 h4`（正文禁 h1 保 SEO 层级） |
| 非白名单标签 | 剥壳保留内部文本（`<div>/<script>/<img>` 标签剥离） |
| a 属性 | 仅 `href`（http/https/mailto/站内相对路径/`#` 锚点）、`target="_blank"`、`rel`（nofollow/noreferrer/noopener 白名单拆分校验） |
| 其余属性 | 一律剥离（`onerror=` 等事件属性、class/style 注入全部清除） |
| 注释/声明 | 剥离 |
| 文本输出 | 纯文本 `html.EscapeString`；属性值 `&amp;/&quot;` 转义 |

## 5. 编译输出规则与产物示例

- **纯文本**（单层直出）：`<div class="wp-c-t1"><p>这是一款兼顾便携与降噪的日常通勤耳机。</p></div>`
- **富文本**（结构化片段，外套单层节点容器）：`<div class="wp-c-t1"><p>核心使用指南：</p><ul><li>长按 3 秒开启蓝牙配对。</li></ul></div>`
- Tailwind 类为规范示意，实际产物以 `CompiledPage.CSS` 输出等价纯净 CSS。

## 6. 实现映射

| 规范条目 | 实现 |
|---|---|
| 组件数据模型与校验 | `internal/builder/components/text/text.go`（Props/Mode/PlainTag/Typography/Binding + 12 项白名单校验） |
| 富文本 XSS 清洗 | `internal/builder/components/text/sanitize.go`（tokenizer 白名单过滤，`golang.org/x/net/html`） |
| 摘要模式 | `stripRichTags` + `truncateRunes`（strip 标签截前 N 字符） |
| 段落间距 / 截断 | `compileCSS`（内部块级规则 + `-webkit-box` 四件套） |
| 单元测试 | `public/test/builder/unit/text_test.go`（9 组 + 12 个校验拒绝场景 + 注入载体拦截） |