# 02-C2 · 正文组件规范 (Text / RichText Component Spec)

本文档规范 `core.text`（正文组件）的功能定位、特有属性、模式切换逻辑、CMS 绑定及编译输出规则。基础盒模型与通用样式继承自 `02-C0`。

## 1. 架构定位

- **组件标识**：`core.text`
- **策略归属**：`BuildStatic`（Go Publish Compiler 编译期直出纯静态 HTML，零客户端 JS）。
- **核心职责**：用于短说明、副标题描述、段落长文、产品详情等各类文本承载场景。
- **富文本能力来源**：`internal/builder/core/richtext.go`——全站**唯一白名单来源**，`core.text` 与 card / quote / infobox / faq 的富文本字段共用；组件内禁止复制白名单。
- **编辑器**：Trix 2.x（本地 vendor `internal/templates/static/vendor/trix/`，`workbench/layout.html` 引入；取代早期 TinyMCE 方案，不走 CDN）。

## 2. 特有属性与配置描述

### 文本模式切换 (Content Mode)

- **富文本模式 (richtext，默认)**：长篇介绍、产品说明、多段落/列表/链接长文；编辑器为 **Trix 2.x**（本地 vendor，零 CDN），工具条与构建期白名单对齐：加粗/斜体/删除线/代码（含 `pre` 代码块，语言经 `language` 属性归一到 `class="language-xxx"`）/有序·无序列表/引用块/行内超链接（`target="_blank"` + `rel="nofollow"`）/标题（h1~h5 原样保留，**不做层级降级**）/附件（图片，粘贴、拖入、工具条插图）；编译产物为语义 HTML 片段（内部 `<p>`/`<ul>`/`<blockquote>` 等）。
- **工具条与白名单的差异**：Trix 2.x 默认工具条产出 `p`/`br`/`strong`/`em`/`s`/`a`/`ul`/`ol`/`li`/`blockquote`/`pre`/`h1`/`figure`（含 `img`+`figcaption`）；`b`/`i`/`u`/`del`/`h2`~`h4` 来自粘贴内容或历史数据，同样在白名单内一并保留。
- **纯文本模式 (plaintext)**：卡片副标题、简短提示、按钮下方小字、单行说明；纯文本输入框无格式工具条；标签可选 `<p>`/`<span>`；编译产物单层直出，零多余嵌套。

### 富文本字段清单 (Rich Text Fields)

以下字段的 `ct` kind 均为 `richtext`（检查器渲染 Trix 编辑器），构建期统一经 core 白名单处理后由模板 `unsafe` 原样输出：

| 组件 | 字段 | 长度上限 | 渲染位置 |
|---|---|---|---|
| `core.text` | `text`（`mode=richtext`，默认） | maxlen 30000 | `text.jet` → `{{ unsafe(.V.SanitizedContent) }}` |
| `core.card` | `text` | maxlen 1000 | `card.jet` → `<div class="sky-card-text">{{ unsafe(.V.Text) }}</div>` |
| `core.quote` | `text` | maxlen 1000 | `quote.jet` → `<div class="sky-quote-text">{{ unsafe(.V.Text) }}</div>` |
| `core.infobox` | `text` | maxlen 2000 | `infobox.jet` → `<div class="sky-infobox-text">{{ unsafe(.V.Text) }}</div>` |
| `core.faq` | `FaqItem.answer` | maxlen 2000 | `faq.jet` → `<div class="sky-faq-answer">{{ unsafe(item.Answer) }}</div>` |

- **统一入口**：card / quote / infobox / faq 的 `BuildView` 调 `core.RichTextHTML`（白名单清洗 + 存量纯文本段落化），视图字段再交给模板 `unsafe` 输出。
- **core.text 例外**：富文本分支直接调 `core.SanitizeRichHTML`（`text` 包私有别名薄转发），不做纯文本段落化——`core.text` 自身有 `mode=plaintext` 模式承担该场景。
- **纯文本模式回退**：`core.text` 的 `mode=plaintext` 在检查器里回退多行输入（`textarea`），不启用 Trix。
- **faq 编辑面板**：`faqPanel`（`controls/repeater.js`）把答案字段单独成块——Trix 需要横向空间，不塞进 repeater 行。

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

三端 Margin/Padding、Align-self、宽度模式、边框/圆角/阴影/透明度、三端显隐、自定义 Class（禁 `sky-` 前缀）与 Element ID。

## 4. 安全清洗规则（编译期）

> 唯一实现：`internal/builder/core/richtext.go`（`allowedRichTags` / `SanitizeRichHTML` / `RichTextHTML`）；组件内不得复制白名单。

| 类别 | 规则 |
|---|---|
| 标签白名单 | `p br strong b em i u s del code pre ul ol li blockquote a h1 h2 h3 h4 h5 hr img figure figcaption div table thead tbody tfoot tr th td caption details summary` |
| 归一与降级 | **标题级别原样保留**：h1~h5 是正文结构的一部分（曾经的「h1 统一降级 h2」已取消）；`div`（Trix 2.x 的段落容器）归一为 `p`，并带**嵌套保护**——已处于段落内时只剥壳，不产出 `<p><p>` |
| 非白名单标签 | 剥壳保留内部文本（`<script>` 等标签剥离） |
| 长度上限 | `core.MaxRichLen = 30000`：富文本与存量纯文本同口径，超长直接判空（防畸形/滥用输入膨胀产物） |
| 存量纯文本兼容 | `core.RichTextHTML`：无标签输入 → 整段 `html.EscapeString` 后按空行分段包 `<p>`，段内换行转 `<br>`（顺序不可颠倒，否则 `<br>` 自身会被转义成可见文本） |
| a 属性 | 仅 `href`、`target="_blank"`、`rel`（nofollow/noreferrer/noopener 白名单拆分校验）。href 协议与 Trix 的 URI 白名单取齐：http/https/ftp/ftps/mailto/tel/callto/sms/cid/xmpp/matrix + 站内相对路径/`#` 锚点；**`javascript:`/`data:`/`vbscript:` 永不放行**，判定前先剥掉空白与控制字符（` javascript:`、`java\tscript:`、`JaVaScRiPt:` 全部拒绝） |
| img 属性 | 仅 `src`（过协议白名单，拒 `javascript:`/`data:`）、`alt`、`width`/`height`（纯数字或常见 CSS 单位）、`loading`（lazy/eager）。**alt 回填**：figure 内没有 alt 的 img，用同级 figcaption 的纯文本（去标签、去首尾空白、上限 200 字符）补上 —— 已有 alt（含显式 `alt=""`）不动，figcaption 本身不改 |
| pre 属性 | 仅代码语言，且**归一**为 `class="language-<值>"`：Trix 的 `language` 属性（`htmlAttributes: ["language"]`）与既有的 `class="language-xxx"` 收敛成同一个 class；值限 `[A-Za-z0-9+#-]{1,32}`，非法（带引号/空格/尖括号/超长）时整个属性丢弃 |
| 其余属性 | 一律剥离（`onerror=` 等事件属性、class/style 注入全部清除） |
| 注释/声明 | 剥离 |
| 文本输出 | 文本节点统一 `html.EscapeString`（防 `&lt;script&gt;` 实体经 tokenizer 解码后复活为真标签，见 `core/richtext.go` 的 C2 存储型 XSS 修复）；属性值 `&amp;/&quot;` 转义 |

## 5. 编译输出规则与产物示例

- **纯文本**（单层直出）：`<div class="sky-c-t1"><p>这是一款兼顾便携与降噪的日常通勤耳机。</p></div>`
- **富文本**（结构化片段，外套单层节点容器）：`<div class="sky-c-t1"><p>核心使用指南：</p><ul><li>长按 3 秒开启蓝牙配对。</li></ul></div>`
- **存量纯文本兼容**（无标签输入，card/quote/infobox/faq 四字段经 `core.RichTextHTML`）：输入 `第一段\n\n第二段\n换行` → `<p>第一段</p><p>第二段<br>换行</p>`
- Tailwind 类为规范示意，实际产物以 `CompiledPage.CSS` 输出等价纯净 CSS。

## 6. 实现映射

| 规范条目 | 实现 |
|---|---|
| 组件数据模型与校验 | `internal/builder/components/text/text.go`（Props/Mode/PlainTag/Typography/Binding + 12 项白名单校验） |
| 富文本白名单与清洗（唯一来源） | `internal/builder/core/richtext.go`：`allowedRichTags` / `SanitizeRichHTML` / `RichTextHTML` / `HasRichMarkup` / `StripRichTags` / `MaxRichLen`（tokenizer 过滤，`golang.org/x/net/html`） |
| text 包薄转发 | `internal/builder/components/text/sanitize.go`：保留包内私有名 `sanitizeRichHTML` / `stripRichTags` 转发到 core，包内调用点与既有测试名不变 |
| 四字段富文本渲染 | `components/{card,quote,infobox,faq}/jet.go` 的 `BuildView` 调 `core.RichTextHTML`，模板 `unsafe` 原样输出（见 §2 字段清单） |
| 检查器控件 | `core/controls.go` 的 `ControlRichText`（`ct:"richtext"`）+ `dashboard/.../inspector_handle.go` 输出 `slot=richtext` + 客户端 `methods/controls/text.js`（`richTextField`）/ `misc.js` 分派 |
| 摘要模式 | `core.StripRichTags` + `truncateRunes`（strip 标签截前 N 字符） |
| 段落间距 / 截断 | `compileCSS`（内部块级规则 + `-webkit-box` 四件套） |
| 单元测试 | `internal/builder/core/richtext_test.go`（纯文本段落化 / 清洗 / 标题级别保留 / 长度上限 / 幂等）+ `richtext_attrs_test.go`（链接协议 / 代码语言 / 图片 alt 回填）+ `components/text/sanitize_test.go`、`sanitize_fuzz_test.go`（薄转发后仍覆盖白名单与注入拦截） |