# internal/templates 约定

后台页面与片段的 Jet 模板。渲染入口是 `NewJetHTMLRender`（内嵌文件系统，生产不依赖工作目录）。

## 可选数据键必须用 isset 判断

```jet
{{if isset(.Blueprints)}} … {{end}}   {* 正确 *}
{{if .Blueprints}} … {{end}}          {* 数据里没有这个键时整页渲染中断 *}
{{if len(.Blueprints) > 0}} … {{end}} {* 同上 *}
```

Jet 的 `if` 要求 bool，而缺失的 map 键求值成 nil —— 类型不符会让渲染在那一行中断。
现象有很强的欺骗性：**HTTP 状态码仍是 200**，中断点之前的 HTML 正常输出，之后的内容
整块消失（列表、表格全没了）。从「列表里少了一行数据」这种表象，几乎不可能定位到
模板中间某一行 if。

因此：只有**保证存在**的键才直接参与判断；可选键（新增的、只由部分渲染路径提供的）
一律用 `isset` 包裹。`admin/pages.html` 的蓝图下拉踩过一次（直接渲染模板的单测不带该键）。

## 错误分支也必须给齐模板必需键

**取数失败的分支不是「少渲染一块」，而是会把整页渲染打断**：Jet 缺 key 的报错点在模板那一行，
现象是 **HTTP 200 + 那一行之后的 HTML 整块消失**。本项目实测过：mail 三个页面的错误分支只设了 `Err`，
没给 `title` / `Accounts` / `AutoTotal`，于是「取数失败」在运营眼里是一张**白页**，而日志里连渲染错误都没有。

两条做法（择一，别每处手工补）：
- 错误分支复用**同一个装配助手**（如 `mailPageErrData(...)` / `mailMarketingErrData(...)`）：助手负责给齐
  该页模板需要的全部键（标题、空列表、筛选回显、分页数），调用点只传错误；
- 或者就地给每个必需键补零值，并在旁边注明「错误分支也要给，否则整页断」。

回归守卫：`public/test/mail/feature/mail_error_leak_test.go` 那类用例会断言「错误路径的响应里有 `</html>`
（整页渲染完）」+「归口文案出现」—— 新增页面时照抄这两条断言。

## CSRF token：两种写法等价

取值链只有一条：渲染数据里的 `csrf_token`（后台页面由 `shell.Prepare` 注入，访客页面由
`user.Handle.render` 注入），经 chain 索引取出。两种写法**都存在、都合法**：

```jet
{{ .["csrf_token"] }}                     {* A：直接取，每处独立求值 *}
{{csrf := .["csrf_token"]}} … {{csrf}}    {* B：顶部声明一次，本文件复用（41 个模板 / 157 处的事实主流）*}
```

`{{csrf}}` 是 Jet 的**模板内 let 变量，不是全局函数** —— `funcs.go` 的 `injectGlobals` 没有注册它，
Go 侧也没有任何 `"csrf"` 数据键。未声明就裸用会报
`identifier "csrf" not available in current … or parent scope, global, or default variables`，
且声明必须写在首次使用之前（作用域链让 include 进来的片段也能读到）。

**禁止 `{{.csrf_token}}`（点号无索引）**：同样是缺 key，`{{ .["csrf_token"] }}` 输出空串（map 末级
缺 key 安全），而 `.csrf_token` 直接运行时报错中断整页 —— 这正是上面「状态码 200、后面整块 HTML
消失」的另一个来源。需要判断存在性时用 chain 写法：`{{if .["DevLogin"]}}`（`admin/login.html` 有先例）。

**fragment 模板例外**：`fragments/*.jet` 的 data 是 **struct**（字段 `CSRFToken`），
`{{ .["csrf_token"] }}` 会报 `can't use csrf_token as field name in struct type`，只能写
`{{ .CSRFToken }}`。`user_account_sessions.jet` 的 `{{csrfToken := .CSRFToken}}` 是写法 B 的片段版。

后台页面两种写法任意选，不需要为了「统一」互相改；改了模板记得跑
`go test ./internal/templates/...` 确认页面仍能渲染。

## 键名大小写

`admin/layout.html` 按 `{{.title}}` / `{{.menu}}` 取小写键，各页面的列表数据用大写
（`{{.Pages}}` / `{{.Projects}}`）。新增页面时对齐既有页面的写法，不要自创一种。

## 数据准备

页面数据由各 handle 的 `templateMap()` 组装（gin.H），模板只做渲染、不做查询。

## 片段复用：include 与 import + yield

列表页的公共结构（批量条、分页、工具条）走**片段**，不要复制 HTML。两种机制分工：

- `include` —— 无参数、或参数就是最终值时的直接插入；
- `import` + `yield` —— 需要**调用点插入自己的内容**时（批量条要包住本页的按钮与字段）。
  片段放 `admin/partials/`，样板见 `admin/partials/bulk_bar.html`：
  `bulkBarOpen(count)` 与 `bulkBarClose()` 成对，中间可 yield 任意个 `bulkBtn(...)`
  （`label` / `cls` / `confirm` / `danger` / `formaction` / `name` / `value`）。

四条实测约束（踩过，别重试）：

1. `{{import ...}}` 必须写在 `{{extends "layout.html"}}` **之后**、任何 `{{block}}` **之前**；
2. `.["t"]` 不能直接写在 yield 参数里（Jet 报 `unexpected node type`），必须先把
   `{{tr := .["t"]}}` 声明成变量再用 `tr(...)`；**取词留在调用点，片段只收成品文案** ——
   否则 `admin_group_f_i18n_test.go` 的 key 判据会把词条算成孤儿；
3. yield 有两种形态且配平要求**相反**：`{{yield b(args)}}` 不需要 `end`，
   `{{yield b(args) content}}` 必须配 `end`。配平判据（`admin_template_integrity_test.go`）
   已同时识别两者，写错立刻变红；
4. 片段只收「结构 + 由调用点传进来的成品文案」，本轮 26 个列表页共用了一个片段，
   调用点的字段与按钮数量各不相同（0~N 个），片段不假定数量。
