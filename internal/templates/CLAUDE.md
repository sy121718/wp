# internal/templates 约定

后台页面与片段的 Jet 模板。渲染入口是 `NewJetHTMLRender`（内嵌文件系统，生产不依赖工作目录）。

## 可选数据键必须用 isset 判断

```jet
{{if isset(.Blueprints)}} … {{end}}   {* 正确 *}
{{if .Blueprints}} … {{end}}          {* 数据里没有这个键时整页渲染中断 *}
{{if len(.Blueprints) > 0}} … {{end}} {* 同上 *}
```

Jet 的 `if` 要求 bool，而缺失的 map 键求值成 nil —— 类型不符会让渲染在那一行中断。

**中断的真实后果**（2026-09 实测推翻了一个流传很广的旧说法）：渲染器先渲到 buffer
（`internal/templates/jet_render.go` 的 `jetInstance.Render`），`Execute` 失败走 `renderError`
→ `http.Error(500, "页面暂时无法显示，请稍后重试")`，**buffer 里的半截内容被丢弃**。于是：

- 整页：用户看到的是通用错误文案页，日志里有 `scene=template` 的渲染错误；
- **htmx 片段：htmx 2.0.4 的默认 `responseHandling` 把 5xx 判成 `swap:false` —— 片段根本不换，
  用户在浏览器里看不到任何反应**（点了保存没有任何动静，比白页更难查）。

旧说法「HTTP 200 + 那一行之后的 HTML 整块消失」是渲染器**加缓冲区之前**的行为，仓库里还有若干
处沿用它（AGENTS.md、若干 handler 与测试注释），不要再据此推理「反正状态码是 200、能看出来」。

因此：只有**保证存在**的键才直接参与判断；可选键（新增的、只由部分渲染路径提供的）
一律用 `isset` 包裹。`admin/pages.html` 的蓝图下拉踩过一次（直接渲染模板的单测不带该键）。

## 错误分支也必须给齐模板必需键

**取数失败的分支不是「少渲染一块」，而是会把整页渲染打断**：Jet 缺 key 的报错点在模板那一行，
渲染直接失败（后果见上节：500 + 通用错误文案，buffer 里的半截内容被丢弃）。本项目实测过：
mail 三个页面的错误分支只设了 `Err`，没给 `title` / `Accounts` / `AutoTotal`，于是「取数失败」
在运营眼里就是一条通用错误提示 —— 那段本该说清失败原因的具体文案一个字都到不了页面。

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
缺 key 安全），而 `.csrf_token` 直接运行时报错、整个响应失败（后果见上节：500 + 通用文案，htmx 档
因 5xx 不 swap 而毫无反应）。需要判断存在性时用 chain 写法：`{{if .["DevLogin"]}}`（`admin/login.html` 有先例）。

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
  片段放 `admin/partials/`（**只有跨模块共用的**；模块专属片段跟模块走，见文末「admin/ 的目录结构」），
  样板见 `admin/partials/bulk_bar.html`：
  `bulkBarOpen(count)` 与 `bulkBarClose()` 成对，中间可 yield 任意个 `bulkBtn(...)`
  （`label` / `cls` / `confirm` / `danger` / `formaction` / `name` / `value`）。

四条实测约束（踩过，别重试）：

1. `{{import ...}}` 必须写在 `{{extends "layout.html"}}` **之后**、任何 `{{block}}` **之前**；
2. **`.["t"](...)` 不能出现在「赋值右侧 / 单目表达式」里** —— Jet 报
   `unexpected node type .["t"] in unary expression`（实测复现，探针见改动记录）。
   这包括 yield 参数，也包括 `{{x := .["t"]("k", "兜底")}}` 这种写法；一律先
   `{{tr := .["t"]}}` 声明成变量再用 `tr(...)`。**取词留在调用点，片段只收成品文案** ——
   否则 `admin_group_f_i18n_test.go` 的 key 判据会把词条算成孤儿；
3. yield 有两种形态且配平要求**相反**：`{{yield b(args)}}` 不需要 `end`，
   `{{yield b(args) content}}` 必须配 `end`。配平判据（`admin_template_integrity_test.go`）
   已同时识别两者，写错立刻变红；
4. 片段只收「结构 + 由调用点传进来的成品文案」，本轮 26 个列表页共用了一个片段，
   调用点的字段与按钮数量各不相同（0~N 个），片段不假定数量；
5. **`include` / `import` / `extends` 的路径都相对「当前模板文件所在目录」解析**，不是相对模板根 ——
   实测（Jet 的 InMemLoader 最小探针，两个同名文件分放同目录与根）：`dir/outer.html` 里写
   `{{include "inner.html"}}` 命中的是 `dir/inner.html`。
   **三种引用形态各怎么写，见文末「admin/ 的目录结构：按后端模块分组」一节** —— 那是分目录后的唯一口径。

## 非基座增强脚本必须自己挂 `htmx:afterSwap`

增强脚本（如 `product-create-form.js`）若**不走 `WBUI.register`**，htmx 把片段 swap 进来后**不会**重新初始化它
—— `WBUI.scan` 只跑 `WBUI.controls` 里登记过的控件。症状是「直接打开页面时正常、被 swap 后静默失效」，
而且**没有任何测试会红**（藏在 JS 里、不编译、不被断言覆盖）。

做法：自己挂 `htmx:afterSwap`，挂法照 `ui/index.js`（在 `document` 上听、以 `e.target` 为范围；
注意 htmx 对 `outerHTML` 替换派发 afterSwap 时目标可能**已脱离文档**，要 `closest` → 目标内查找 → document 兜底）。

**幂等标记必须落在「内容节点」上，不能落在容器上**：容器（`<form>`）会被 htmx 复用、只换里面的内容，
容器级的 `dataset` 标记会让第二次 swap 进来的新控件被当成「已增强」而跳过。

## 写表单失败的「原地留住输入」分档（htmx 路径 A）

**用户的输入比错误文案贵**：一个业务错误把整屏已填内容清掉，是最容易被容忍、也最伤的缺陷。
表单保留原生 `method` / `action`（无 JS 时照旧能提交），另加 `hx-post`（htmx 优先拦住 submit），
服务端按 `HX-Request` 分档：

| 请求 | 失败 | 成功 |
|---|---|---|
| htmx（`HX-Request: true`） | **200 + 片段自身**（错误槽 + 回填后的表单） | `HX-Redirect`（**不能是 302**：XHR 会自己跟随，最终响应里读不到 `Location`，整页 HTML 会被塞进片段的位置） |
| 原生 | `302 + ?err=` 回**本页** | `302` 到目标页 |

四条实测约束：

1. **成功路径也必须分档** —— 写表单的分档不是「失败才分」，成功那一支漏了照样坏页面；
2. **回填键名要带前缀**（`FormEcho` / `FormEchoChecked` / `FormEchoMulti`）：片段与页面共用同一份
   渲染 data，通用键名会撞上页面已有键 —— 商品列表页的 `Form` 是批量改价的 **结构体**，
   撞名时片段里的 `{{.Form.name}}` 命中结构体、渲染直接失败；
3. **多选按「值」命中**（遍历本次提交的同名多值逐个比对），不能用「该字段提交过」判 ——
   后者会把整组选项一次勾满（用户只勾了一个，回填后变成一片）；
4. **错误槽放在 `<form>` 之外、host 之内** —— 放进 form 里会被下一次提交整块换掉。

片段自带 host（`<div data-product-create-host>`），表单用
`hx-target="closest [data-product-create-host]"` + `hx-swap="outerHTML"`，
失败时服务端渲染的就是**这个片段自身**（一个真源，两条路径）。

**字段清单与模板必须正反双向断言**：模板读的每个回填字段都必须在 handler 的清单里，
反向也要钉（清单里的字段模板得真的在读）。漏列不是「页面截断」，而是渲染失败 → 500 →
htmx 不 swap（用户点了保存**什么都看不到**，见本文首节）。

样板：`admin/product/product_create_form.html` + `internal/module/product/inbound/http/`
的 `productCreateFail` / `productCreateFormFields`；判据、审查结论与浏览器实测见
`docs/02-T-write-fail-echo-batch1.md`。

## Jet 模板内可以做简单计算（不必为计数改 handler）

Jet v6 支持**三元表达式与算术**，所以「折叠卡 summary 带计数」这类需求**不需要在 handler 里暴露新键**：

```jet
{{kinds := (len(.ArtifactPatrol.OrphanSchemas) > 0 ? 1 : 0) + (len(.ArtifactPatrol.MissingSchemas) > 0 ? 1 : 0) + …}}
{{if kinds > 0}} <details …><summary>… {{kinds}} 类不一致</summary> … {{end}}
```

两个实测结论：
- **Jet v6.3.2 不支持 `??`（空值合并）运算符** —— 缺键兜底要写 `{{x := "默认"}}{{if isset(.X)}}{{x = .X}}{{end}}`
  （对已声明的模板内变量赋值有效，能影响外层作用域）。
- 类名语义：`.page-sub` **只在 `.page-head .page-sub` 下有样式**，写在卡内就退化成普通段落；
  `.card-body`（**不带 `card`**）是「无边框无背景、但保留 `.card-body > h2` 小节标题样式」的写法 ——
  卡里分小节时用它，避免「卡中卡」。

## 表格列数：数据行的 `td` 数必须与表头列数一致

**加列 / 合并表格时最容易犯的错**：表头写了 N 列、数据行只写 N-1 个 `<td>`。
浏览器**不报错**，而是把缺的那一列补在**行尾** → 整表**左移一列**（第一列显示的是第二列的值），
模板层没有异常、纯计数断言也抓不到（`<table>`/`<tr>` 数都对）。

实测踩过：`page_translations.html` 合并组表后表头 6 列（组件/字段/原文/译文/状态/来源），
数据行只给了 5 个 `td` → 字段值显示进了「组件」列。**这类缺陷只有渲染后逐行数字段才能发现**，
所以合并表格时要写一条「**每行 `td` 数 == 表头列数**」的渲染断言，别只断言表的数量。

配套注意：空态行与「分组行 / 复用行」这类跨列行要用 `colspan` **精确等于列数**，
它们与数据行的 td 数不同（一个是 1 个 td 配 colspan，另一个是 N 个 td）。

## 同文件复用块要包起来；`.help-pop` 不能放进 overflow 容器

**① Jet v6：同文件里 `{{block}}` 定义会立即渲染一次。** 想在**同一个文件内**定义一份可复用的块
（例如面板级空态）再多次使用，直接写 `{{block x()}}…{{end}}` 会在定义处就渲染一遍，页面上凭空多出一块。
两个可行写法：

- 用 `{{if false}}…{{end}}` 包住定义（解析期注册、执行期跳过）—— `analytics.html` 的面板级空态用这条，
  守卫是 `admin_empty_actions_test.go` 的「恰好 5 处 `.empty-actions`」（Jet 若改掉这个行为会立刻变红）；
- 或者把块挪进**独立片段文件**再 `{{import}}` —— `partials/bulk_bar.html` 用这条。

另：块参数**必须命名传递**，位置参数会被渲染测试当场打回（实测踩过，2026-09 第二次）。

**② `.help-pop`（悬浮说明）不能放在 `.table-scroll` 内的 `<td>` 里。** 那个容器是 `overflow: auto`，
会把 `<td>` 内绝对定位的悬浮层裁掉（表头 `th` 在容器顶部、向下展开才有空间）。
实测：`customers.html` 把「待激活 / 已锁定 / 失败次数未清零」三句说明从行内 `colspan` 提示行移到
**状态列表头的 `.help`** 才显示正常 —— 顺带解决了「同一句在每一行重复」。

## admin/ 的目录结构：按后端模块分组

`admin/` 下**按后端模块分子目录**，不再平铺（2026-09 重构，判据与实测见 `docs/02-U-admin-template-restructure.md`）：

- 根只留 `layout.html` / `login.html` / `dashboard.html` 三个壳页面；
- `partials/` 只放**跨模块 / 通用**片段（bulk_bar / pagination / toolbar_create / rich_editor /
  sidebar / nav-nodes / media_field / locale_rows）—— 判据是**被几个模块引用**，不是名字像谁；
- 其余按模块：`system/`（**= 后端 admin 模块**：管理员 / 角色 / 权限 / 菜单 / 部门 / 数据权限 / 词条）、
  `product/ inventory/ order/ content/ page/ project/ navigation/ mail/ analytics/ block/
  contenttemplate/ plugin/ media/ masterdata/ user/`；
- **模块专属片段跟模块走**（如 `product/product_create_form.html`）。

### 引用路径怎么写（include / import / **extends** 都相对「当前文件所在目录」解析）

| 谁引用谁 | 写法 |
|---|---|
| 根下页面 → 共享片段 | `partials/x.html` |
| 子目录页面 → 共享片段 | `../partials/x.html` |
| 子目录页面 → 同模块片段 | `x.html`（裸名） |
| 任何子目录页面 → layout | `../layout.html` |

### 改文件名 / 搬文件时的硬要求

**先 `rg` 出这个文件名的全部出现形态**——最容易漏的**八种**（全都**不会**在编译期报错）：
① 相对同目录的**裸名** include；② `{{import}}`；③ **不带 `.html` 后缀**的模板名（`c.HTML(…, "admin/settings", …)`）；
④ **`{{extends}}`**（漏了 = 整页 500）；⑤ 测试里的**文件系统路径**（`os.ReadFile("admin/x.html")`、
`filepath.Glob("admin/*.html")`、基线表的 key）；⑥ 正则字符类写窄（`[a-z_]` 会漏掉含数字的 `i18n`）；
⑦ **引用落点写错**（不是「没改到」）—— `fragments/` 在**模板根**下，从 `admin/<模块>/` 出发要写
`../../fragments/x.html`：实测把 `content/article_edit.html` 的 seo 片段写成 `../fragments/`（少一层），
content 包 4 条渲染测试当场红；⑧ **验证范围不足** —— 只跑 `./internal/templates/...` 会漏掉模块包里的
硬编码路径（`internal/module/**/inbound/http/*_test.go` 实测红了 4 条），只扫「能直接打开的 URL」
会漏掉带参数的编辑页（`article_edit` 正是这样逃过抽查的）：搬完模板至少跑 `go test ./internal/...`。

改完必须：**反查残留** + 走一遍**真实 HTTP 路径**（编译通过 ≠ 引用正确）。
门禁里的 glob 要**递归**：`filepath.Glob("admin/*.html")` 在分目录后只匹配到 3 个壳页面 ——
门禁会静默缩水成「只查 3 个文件」，它仍然绿，但不再守任何东西。
测试请用 `adminTemplateFiles` / `adminTemplatePath` / `adminTemplateSource` 按 basename 解析，不要硬编码路径。

**落点有专门的守卫**：`internal/templates/admin_template_resolve_test.go` 的
`TestAdminTemplateReferencesResolve` 静态检查 admin/ 下每个模板的 `extends` / `import` / `include`
落点是否存在（当前 76 个模板 / 134 处引用，并带坏样本自检防它退化成空转）。它不渲染、不连库，
能在改动当场把「路径少一层」这类问题一次报全 —— 加模板或搬目录后先跑它。

## i18n key 扫描：三种取词形态都要扫

算「模板在用、但 `sys_i18n` 里没有」的 key（补词条、查漏）时，**只扫一种调用形式会少一半** ——
本轮实测：只认 `{{ .["t"]("key", "兜底") }}` 得到 128 条，补上复用形态后是 **183 条**（+43%）。

三种形态都要覆盖：

```jet
{{ .["t"]("admin.x.y", "兜底") }}                  {* ① 直调 *}
{{tr := .["t"]}} … {{tr("admin.x.y", "兜底")}}      {* ② 复用 —— 模板的事实主流 *}
{{gfTr := .["t"]}} … {{gfTr("admin.x.y", "兜底")}}  {* ③ 任意局部变量名 *}
```

- **②③ 才是主流**：Jet 里 `.["t"](…)` 不能出现在赋值右侧与 yield 参数里（见本文「片段复用」节），
  所以凡是 range / 循环内取词都得先声明变量再调用 —— 变量名由作者定（`tr` / `gfTr` / …），
  **按变量名列举的白名单一定会漏**。判据按**形状**给最稳：点分 key 后面紧跟逗号与第二个字符串参数，
  `\(\s*"[a-z][a-zA-Z0-9_.]*\.[a-zA-Z0-9_.]+"\s*,\s*"`。
- **Go 侧完全不在模板面上**：`shell.TranslateFor(c)` 拿到的 `tr("admin.x", "中文")` 要单独扫
  `internal/module/**/*.go`（本轮 3 条 key 只在这里找得到）。
- **交叉验证**：再用「模板里所有小写点分字符串字面量」当上界算一遍，排除文件名（`*.html`）、
  域名示例、`settings.*` 与表单字段名 —— 两种口径的结果应当一致，不一致就说明还有漏掉的形态。
  但别把上界口径直接当结论：它会混进 JS 方法名与模板文件名（实测 3182 条候选里大部分是噪声）。
- **真源是数据库的 `sys_i18n`**（主键 `(item_key, lang)`），不是迁移文件 —— 判定要看库里有没有；
  迁移只负责把它写进去。而迁移是 seed（可重复执行），删词条时**必须同批改 `ConditionSQL` 的
  计数门槛**，否则下次启动会被重新插回去（见 AGENTS.md 的 122/104 教训）。
