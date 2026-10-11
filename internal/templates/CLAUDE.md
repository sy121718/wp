# internal/templates 约定

后台页面与片段的 Jet 模板。渲染入口是 `NewJetHTMLRender`（内嵌文件系统，生产不依赖工作目录）。

## 可选数据键必须用 isset 判断

```jet
{{if isset(.Blueprints)}} … {{end}}   {* 正确 *}
{{if .Blueprints}} … {{end}}          {* 数据里没有这个键时整页渲染中断 *}
{{if len(.Blueprints) > 0}} … {{end}} {* 同上 *}
```

Jet 的 `if` 要求 bool，而缺失的 map 键求值成 nil —— 类型不符会让渲染在那一行中断。

**中断的真实后果**（2026-09 实测）：渲染器先渲到 buffer（`jet_render.go` 的 `jetInstance.Render`），
`Execute` 失败走 `renderError` → `http.Error(500, "页面暂时无法显示，请稍后重试")`，**buffer 里的半截内容被
丢弃**。于是整页看到通用错误页 + 日志有 `scene=template`；**htmx 片段则因 2.0.4 默认 `responseHandling`
把 5xx 判成 `swap:false` —— 片段根本不换，用户在浏览器里看不到任何反应**（点了保存没动静，比白页更难查）。

> 旧说法「HTTP 200 + 那一行之后的 HTML 整块消失」是渲染器**加缓冲区之前**的行为，仓库里还有若干处沿用它，
> 不要再据此推理「反正状态码是 200、能看出来」。

因此：只有**保证存在**的键才直接参与判断；可选键（新增的、只由部分渲染路径提供的）一律用 `isset` 包裹。
`admin/pages.html` 的蓝图下拉踩过一次（直接渲染模板的单测不带该键）。

### 缺键的两种后果：取决于键被用在哪里（2026-09 实测）

上面说的是**用在 `if` 条件 / 点号输出**时的后果（中断 → 500）。但同一个缺失的键，换成
**chain 索引 + 函数调用**就完全不是这个结局：

| 用法 | 缺键后果 |
|---|---|
| `{{if .MissingKey}}`、`{{.missing_field}}`（点号） | 渲染中断 → 500（见上节） |
| `{{tr := .["t"]}}` 之后 `{{tr("k","兜底")}}`（chain 索引 + 调用） | **静默**：不报错、不 500、无日志，`tr(...)` 输出**空串** |

判据（`jet_missing_t_varform_test.go` 与 `i18n_jet_test.go` 的 `TestJetTranslateFuncPath` 共同固化）：

```jet
{{tr := .["t"]}}V[{{tr("k", "兜底")}}]   {* data 缺 t 时渲染成 "V[]"，err == nil *}
```

直调形态 `{{ .["t"]("k","兜底") }}` 同样静默 —— 两种形态都**不会**给你 500 或日志。

这条比 500 难查：**页面结构完整、状态码 200、日志干净，只有文案整片空白**（`t` 缺失时所有取词
一起变空）。`shell.Prepare` 无条件注入 `t`，所以漏 `t` 只可能出现在**不走 Prepare 的渲染路径**与
**测试里手工构造的 data** —— 写这类测试/片段渲染时先把 `t` 与 `lang` 带上。

### `t` 与 `tf` 必须成对（缺 `tf` 是 500，缺 `t` 是静默）

`tf(key, fallback, args)` 是「取词 + 填 `%s` 占位符」那一个（签名
`func(key, fallback string, args []string) string`），给带计数的整句用（「已保存 %s 条译文。」）。
两个都经 `data` 注入 —— `adminui.Prepare`（整页）与 `adminui.PrepareBare`（同一份模板不带页壳渲染）
都注入，**语言是请求级的，所以不能挂 `set.AddGlobal`**（并发下 -race 报 DATA RACE）。

两种缺法的后果**不一样，别记混**：

| 缺 | 后果 |
|---|---|
| 模板调了 `tf(...)` 而 data 里没有 `tf` | `identifier "tf" not available in current … or parent scope` → 渲染中断 → **500** |
| 模板用 `{{tr := .["t"]}}` 而 data 里没有 `t` | **静默空串**（见上表） |

实测（2026-10，本仓）：`admin/page/pages.html` 只声明了 `{{tr := .["t"]}}`，新加的提示槽却调了
`tf(...)` —— 于是「列表取数失败」那条降级提示一分页就 500，而这条路径平时不走，
单元测试喂的数据恰好都不触发它。**加 `{{tf := .["tf"]}}` 与调用必须在同一次改动里**。

### 槽位收**事实**，不收成品串

取词解在渲染期：数据槽给 `adminui.Msg`（`{Key, Fallback, Args}`，模板 `tr` / `tf` 取词），
**不要在 Go 里翻好再塞进程串**。成品串把那一句锁死在一种语言，而它在英文界面上仍然是中文
（不报错、门禁也不红）。槽位判空统一写 `{{if x.Key != "" || x.Fallback != ""}}` ——
**不要写 `{{if x}}`**：结构体恒为真，空事实会渲染成一个空徽标。

同一处还有两件事一起改：
- 模板里新出现的取词（不论 `tr` 还是 `tf`）要同时声明对应变量；
- 直接渲染模板的测试（`internal/templates`、`public/test/**/feature`）喂的数据要跟着换形状 ——
  给空串（旧形态）会在 `.Key` 上炸，给缺键会被 `isset` 守卫吞掉（那时断言会变成「提示没出现」）。

### 行数据是 `gin.H` 时，缺键当**参数**用会中断渲染

同一条「缺键」在不同位置后果不同（2026-10，product 批实测）：

| 用法 | 缺键时 |
|---|---|
| `{{x.Foo}}` 直接输出 | **静默空串**（不报错） |
| `{{tr(x.FooKey, x.FooFallback)}}` 当**函数实参** | `call expression: argument for position 0 in func(string, string) string is not a valid value` → 渲染中断 → **500** |

所以「行数据只有 `Label`、模板改读 `LabelKey` 之后忘了改夹具」这类错会在**渲染期**炸而不是编译期，
症状是整块片段 500（htmx 对 5xx 不 swap，用户点了没反应）。两类做法：

- 生产路径：把键**给齐**（Go 侧装配恒给 `XxxKey` + `XxxFallback`）——这是正解，模板不写守卫；
- 过渡/兼容老形状：模板写 `{{if isset(x.LabelKey)}}{{tr(x.LabelKey, x.LabelFallback)}}{{else}}{{x.Label}}{{end}}`
  （`isset` 对 map 缺键返回 false；结构体字段恒存在，**不需要**这层守卫）。
  · **`isset` 问的是「键在不在」，不是「值空不空」** —— 数据行（分类名 / 品牌名 / 商品名这类**没有词条**
  的行）带上 `"LabelKey": ""` 就等于给定了这个键，`isset` 为真 → 走取词分支 → `tr("", "")` →
  **空白 option**：200、结构完整、日志干净。实测 `TestTaxonomyAdminPages`（`public/test/product/feature`）
  报「商品编辑页缺少 \"山野\"」就是这个原因。判据：**数据行整键省略，不给空串**
  （`productPickOptions` / `bundleSourceProductOptions` + `internal/module/product/inbound/http/product_dropdown_option_shape_test.go`，
  该测试对「数据行带上键」与「空项没给非空 Key」两个方向都判失败）。

判据在测试侧：`internal/templates` 的夹具按**新形状**手写（`internal/templates/product_attribute_form_fail_render_test.go`
是样板：`SubmitErr` 给 `{Key, Fallback, Args}`、片段 data 带 `t` 与 `tf`）。

## 错误分支也必须给齐模板必需键

**取数失败的分支不是「少渲染一块」，而是会把整页渲染打断**（后果见上节）。实测：mail 三个页面的错误分支
只设了 `Err`，没给 `title` / `Accounts` / `AutoTotal`，于是「取数失败」在运营眼里就是一条通用错误提示 ——
那段本该说清失败原因的具体文案一个字都到不了页面。

两条做法（择一，别每处手工补）：

- 错误分支复用**同一个装配助手**（如 `mailPageErrData(...)`）：助手给齐该页模板需要的全部键
  （标题、空列表、筛选回显、分页数），调用点只传错误；
- 或就地给每个必需键补零值，并在旁边注明「错误分支也要给，否则整页断」。

回归守卫：`public/test/mail/feature/mail_error_leak_test.go` 那类用例断言「错误路径响应里有 `</html>`」
+「归口文案出现」—— 新增页面时照抄这两条断言。

## CSRF token（本文件只记 fragment 特例）

后台页面 / 访客页面的取值链、两种合法写法与禁令见 `AGENTS.md` §「模板渲染」，本文件不重复。

**fragment 模板例外**：`fragments/*.jet` 的 data 是 **struct**（字段 `CSRFToken`），
`{{ .["csrf_token"] }}` 会报 `can't use csrf_token as field name in struct type`，只能写 `{{ .CSRFToken }}`。
`user_account_sessions.jet` 的 `{{csrfToken := .CSRFToken}}` 是片段版写法。
**同样的限制适用于任何以 struct 为渲染 data 的模板**（见下文 i18n 一节）。

改了模板记得跑 `go test ./internal/templates/...` 确认页面仍能渲染。

## 键名与数据准备

`admin/layout.html` 按 `{{.title}}` / `{{.menu}}` 取小写键，各页面的列表数据用大写（`{{.Pages}}` /
`{{.Projects}}`）—— 新增页面时对齐既有页面的写法，不要自创一种。
页面数据由各 handle 的 `templateMap()` 组装（gin.H），**模板只做渲染、不做查询**。

## 片段复用：include 与 import + yield

列表页的公共结构（批量条、分页、工具条）走**片段**，不要复制 HTML。两种机制分工：

- `include` —— 无参数、或参数就是最终值时的直接插入；
- `import` + `yield` —— 需要**调用点插入自己的内容**时（批量条要包住本页的按钮与字段）。
  片段放 `admin/partials/`（**只有跨模块共用的**；模块专属片段跟模块走），样板见
  `admin/partials/bulk_bar.html`：`bulkBarOpen(count)` 与 `bulkBarClose()` 成对，中间可 yield 任意个
  `bulkBtn(...)`。

五条实测约束（踩过，别重试）：

1. `{{import ...}}` 必须写在 `{{extends "layout.html"}}` **之后**、任何 `{{block}}` **之前**；
2. **`.["t"](...)` 不能出现在「赋值右侧 / 单目表达式」里** —— Jet 报
   `unexpected node type .["t"] in unary expression`。这包括 yield 参数，也包括
   `{{x := .["t"]("k", "兜底")}}`；一律先 `{{tr := .["t"]}}` 声明成变量再用 `tr(...)`。
   **取词留在调用点，片段只收成品文案** —— 否则 `admin_group_f_i18n_test.go` 的 key 判据会把词条算成孤儿；
3. yield 有两种形态且配平要求**相反**：`{{yield b(args)}}` 不需要 `end`，`{{yield b(args) content}}` 必须配
   `end`。配平判据（`admin_template_integrity_test.go`）已同时识别两者；
4. 片段只收「结构 + 由调用点传进来的成品文案」，不假定调用点字段与按钮的数量（0~N 个）；
5. **`include` / `import` / `extends` 的路径都相对「当前模板文件所在目录」解析**，不是相对模板根 ——
   三种引用形态的写法见下文「引用路径怎么写」。

## 非基座增强脚本必须自己挂 `htmx:afterSwap`

增强脚本（如 `product-create-form.js`）若**不走 `WBUI.register`**，htmx 把片段 swap 进来后**不会**重新初始化
它 —— `WBUI.scan` 只跑 `WBUI.controls` 里登记过的控件。症状是「直接打开页面时正常、被 swap 后静默失效」，
而且**没有任何测试会红**（藏在 JS 里、不编译、不被断言覆盖）。

做法：自己挂 `htmx:afterSwap`，挂法照 `ui/index.js`（在 `document` 上听、以 `e.target` 为范围；
注意 htmx 对 `outerHTML` 替换派发 afterSwap 时目标可能**已脱离文档**，要 `closest` → 目标内查找 →
document 兜底）。

**幂等标记必须落在「内容节点」上，不能落在容器上**：容器（`<form>`）会被 htmx 复用、只换里面的内容，
容器级的 `dataset` 标记会让第二次 swap 进来的新控件被当成「已增强」而跳过。

## 写表单失败时原地留住输入（htmx 路径）

**用户的输入比错误文案贵**：一个业务错误把整屏已填内容清掉，是最容易被容忍、也最伤的缺陷。
表单保留原生 `method` / `action`（无 JS 时照旧能提交），另加 `hx-post`（htmx 优先拦住 submit），
服务端按 `HX-Request` 分档：

| 请求 | 失败 | 成功 |
|---|---|---|
| htmx（`HX-Request: true`） | **200 + 片段自身**（错误槽 + 回填后的表单） | `HX-Redirect`（**不能是 302**：XHR 会自己跟随，最终响应里读不到 `Location`，整页 HTML 会被塞进片段的位置） |
| 原生 | `302 + ?err=` 回**本页** | `302` 到目标页 |

四条实测约束：

1. **成功路径也必须分档** —— 写表单的分档不是「失败才分」，成功那一支漏了照样坏页面；
2. **回填键名要带前缀**（`FormEcho` / `FormEchoChecked` / `FormEchoMulti`）：片段与页面共用同一份渲染 data，
   通用键名会撞上页面已有键 —— 商品列表页的 `Form` 是批量改价的**结构体**，撞名时片段里的 `{{.Form.name}}`
   命中结构体、渲染直接失败；
3. **多选按「值」命中**（遍历本次提交的同名多值逐个比对），不能用「该字段提交过」判 —— 后者会把整组选项
   一次勾满；
4. **错误槽放在 `<form>` 之外、host 之内** —— 放进 form 里会被下一次提交整块换掉。

片段自带 host（`<div data-product-create-host>`），表单用 `hx-target="closest [data-product-create-host]"`
+ `hx-swap="outerHTML"`，失败时服务端渲染的就是**这个片段自身**（一个真源，两条路径）。

**字段清单与模板必须正反双向断言**：模板读的每个回填字段都必须在 handler 的清单里，反向也要钉。
漏列不是「页面截断」，而是渲染失败 → 500 → htmx 不 swap（用户点了保存**什么都看不到**）。

样板：`admin/product/product_create_form.html` + `internal/module/product/inbound/http/` 的
`productCreateFail` / `productCreateFormFields`；判据与浏览器实测见 `docs/02-T-write-fail-echo-batch1.md`。

## Jet 模板内可以做简单计算

Jet v6 支持**三元表达式与算术**，所以「折叠卡 summary 带计数」这类需求**不需要在 handler 里暴露新键**：

```jet
{{kinds := (len(.ArtifactPatrol.OrphanSchemas) > 0 ? 1 : 0) + (len(.ArtifactPatrol.MissingSchemas) > 0 ? 1 : 0) + …}}
```

两个实测结论：

- **Jet v6.3.2 不支持 `??`（空值合并）** —— 缺键兜底要写
  `{{x := "默认"}}{{if isset(.X)}}{{x = .X}}{{end}}`（对已声明的模板内变量赋值有效，能影响外层作用域）；
- 类名语义：`.page-sub` **只在 `.page-head .page-sub` 下有样式**；
  `.card-body`（**不带 `card`**）是「无边框无背景、但保留 `.card-body > h2` 小节标题样式」的写法 ——
  卡里分小节时用它，避免「卡中卡」。

## `range` 里调全局函数必须用 `{{range _, r := …}}` 形式

`{{range .Items}}…{{end}}` 会把**点号**指向当前项，于是循环体里任何 `.["x"]` 都是在**当前项上找字段**，
而不是在根数据上找键。症状取决于它找的是什么：

```jet
{{range .Overview.Presets}}
  {{ .["t"](r.LabelKey, r.Label) }}   {* ✗ Jet Runtime Error *)
{{end}}
{* can't use t as field name in struct type templates.tmplRangePreset *}
```

报错文本说的是「t 不是结构体字段」，而 `t` 明明是 layout 注入的全局函数 —— 这句话很容易被读成
「t 没注入」或「模板注册有问题」，从而去查 `shell.Prepare` 那条链，方向就错了：根因是 `range` 换了点号。

**两种修法，选一种并保持一致**：

```jet
{{range _, r := .Overview.Presets}}      {* ✅ 点号仍指根数据；用 r.X 取项 *)
  <a href="{{r.URL}}">{{ .["t"](r.LabelKey, r.Label) }}</a>
{{end}}

{{range $ := .Overview.Presets}}         {* 也可以，但仓库里没有先例 *)
```

**这个写法是既有约定，不是新发明** —— `internal/templates/admin/dashboard.html` 的筛选条预设按钮
（`.Overview.Presets`）与趋势图粒度按钮（`.Overview.GranularityPresets`）都写 `{{range _, x := …}}`。
新写 range 时照抄这个形状；已经写了 `{{range .Foo}}` 的，只要循环体里出现 `.["t"]`、`.["csrf_token"]`
或其它**根级键**，就必须改。

## 表格列数：数据行的 `td` 数必须与表头列数一致

表头写了 N 列、数据行只写 N-1 个 `<td>` 时，浏览器**不报错**，而是把缺的那列补在**行尾** → 整表**左移一列**
（第一列显示的是第二列的值），模板层没有异常、纯计数断言也抓不到（`<table>`/`<tr>` 数都对）。

实测踩过：`page_translations.html` 合并组表后表头 6 列、数据行只给了 5 个 `td`。**这类缺陷只有渲染后逐行数
字段才能发现** —— 合并表格时要写一条「**每行 `td` 数 == 表头列数**」的渲染断言，别只断言表的数量。

配套：空态行与「分组行 / 复用行」这类跨列行要用 `colspan` **精确等于列数**。

## 同文件复用块要包起来；`.help-pop` 不能放进 overflow 容器

**① Jet v6：同文件里 `{{block}}` 定义会立即渲染一次。** 想在**同一个文件内**定义一份可复用的块再多次使用，
直接写 `{{block x()}}…{{end}}` 会在定义处就渲染一遍，页面上凭空多出一块。两个可行写法：

- 用 `{{if false}}…{{end}}` 包住定义（解析期注册、执行期跳过）—— `analytics.html` 的面板级空态用这条，
  守卫是 `admin_empty_actions_test.go` 的「恰好 5 处 `.empty-actions`」；
- 或把块挪进**独立片段文件**再 `{{import}}` —— `partials/bulk_bar.html` 用这条。

另：块参数**必须命名传递**，位置参数会被渲染测试当场打回（实测踩过，2026-09 第二次）。

**② `.help-pop`（悬浮说明）不能放在 `.table-scroll` 内的 `<td>` 里。** 那个容器是 `overflow: auto`，
会把 `<td>` 内绝对定位的悬浮层裁掉（表头 `th` 在容器顶部、向下展开才有空间）。实测：`customers.html` 把
三句说明从行内 `colspan` 提示行移到**状态列表头的 `.help`** 才显示正常 —— 顺带解决了「同一句在每一行重复」。

**③ 说明文字一律走 `.help-pop`，不用原生 `title`（2026-10 实测，第二次犯）。**
口径 / 领域说明只有一个通行形态：

```html
<span class="help">
  <button type="button" class="help-btn" aria-label="{{ tr("admin.common.help.label", "查看说明") }}" aria-expanded="false">?</button>
  <span class="help-pop" role="tooltip">{{ tr("…hint", "一句话说明") }}</span>
</span>
```

`admin.js` 已把 hover / 键盘聚焦（`:focus-within`）/ Esc / 点外部关闭 / **贴右边缘自动左翻** 全做完了，
写模板时不需要任何 JS。

原生 `title` 的问题不是「不好看」而是**位置不可控**：实测悬浮时它盖住相邻卡片，看上去就是
「说明文字直接显示在卡面上」——被当成「把注释写进了前端」。它还不受 `z-index`、看不见键盘焦点、
触屏不出现。

**例外（只有一类）**：**纯图标按钮的无障碍名字**（关闭 / 切换主题 / 语言 / 站点，以及柱图的
按月 tooltip）继续用 `title` —— 它们没有可见文字，改成浮层反而要点一次才读得到。

**④ 卡片口径改浮层时必须同时放开 `.card` 的 `overflow`，且必须用双类。**
`.card` 基座带 `overflow: hidden`（`ui.css:551`），浮层是从标签往下展开的绝对定位元素，
不放开会**被卡片边界切掉一截**（实测「销售额 ÷ 订单数」右半句没了）。写成单类 `.sales-card`
与 `.card` **同层**（都 (0,1,0)），胜负就落到「`ui.css` 在 `theme.css` 之后加载」这个与规则无关的
事实上（`layout.html:25-27`），一重排就静默失效 —— 正是 `ui_base_layering_contract_test.go` 记录的那类
缺陷。正确写法是 `.card.sales-card { overflow: visible; }`（(0,2,0)）。

闸门：`bash scripts/check-help-popover.sh`（模板里的卡片 / 统计卡上出现 `title` 即失败，
白名单见该脚本的 `ALLOW`）。

### 改 UI 之前先过设计技能（流程硬要求）

新增后台页面、或改动既有页面的外观之前，**先加载技能**：`frontend-design`（方向与手艺）
+ `ui-refit`（改造协议：先审计 → 声明 preserve list → 按切片改 → 收尾带校验器输出）。
产品 UI / dashboard / admin 走 Emil 品味 + craft-floor，**不要**套 taste-skill 的营销页规则。
先审计再动手，别先改样式再回头找问题。

## admin/ 的目录结构：按后端模块分组

`admin/` 下按后端模块分子目录：

- 根只留 `layout.html` / `login.html` / `dashboard.html` 三个壳页面；
- `partials/` 只放**跨模块 / 通用**片段（bulk_bar / pagination / toolbar_create / rich_editor / sidebar /
  nav-nodes / media_field / locale_rows）—— 判据是**被几个模块引用**，不是名字像谁；
- 其余按模块：`system/`（**= 后端 admin 模块**）、`product/ inventory/ order/ content/ page/ project/
  navigation/ mail/ analytics/ block/ contenttemplate/ plugin/ media/ masterdata/ user/`；
- **模块专属片段跟模块走**（如 `product/product_create_form.html`）。

### 引用路径怎么写

`include` / `import` / **`extends`** 都相对「当前文件所在目录」解析：

| 谁引用谁 | 写法 |
|---|---|
| 根下页面 → 共享片段 | `partials/x.html` |
| 子目录页面 → 共享片段 | `../partials/x.html` |
| 子目录页面 → 同模块片段 | `x.html`（裸名） |
| 任何子目录页面 → layout | `../layout.html` |

### 改文件名 / 搬文件时的硬要求

**先 `rg` 出这个文件名的全部出现形态** —— 最容易漏的**八种**（全都**不会**在编译期报错）：
① 相对同目录的**裸名** include；② `{{import}}`；③ **不带 `.html` 后缀**的模板名
（`c.HTML(…, "admin/settings", …)`）；④ **`{{extends}}`**（漏了 = 整页 500）；⑤ 测试里的**文件系统路径**
（`os.ReadFile("admin/x.html")`、`filepath.Glob("admin/*.html")`、基线表的 key）；⑥ 正则字符类写窄
（`[a-z_]` 会漏掉含数字的 `i18n`）；⑦ **引用落点写错**（不是「没改到」）—— `fragments/` 在**模板根**下，
从 `admin/<模块>/` 出发要写 `../../fragments/x.html`（实测写成 `../fragments/` 少一层，content 包 4 条渲染
测试当场红）；⑧ **验证范围不足** —— 只跑 `./internal/templates/...` 会漏掉模块包里的硬编码路径
（`internal/module/**/inbound/http/*_test.go` 实测红了 4 条），只扫「能直接打开的 URL」会漏掉带参数的编辑页：
搬完模板至少跑 `go test ./internal/...`。

改完必须：**反查残留** + 走一遍**真实 HTTP 路径**（编译通过 ≠ 引用正确）。
门禁里的 glob 要**递归**：`filepath.Glob("admin/*.html")` 在分目录后只匹配到 3 个壳页面 ——
门禁会静默缩水、仍然绿，但不再守任何东西。测试请用 `adminTemplateFiles` / `adminTemplatePath` /
`adminTemplateSource` 按 basename 解析，不要硬编码路径。

**落点有专门的守卫**：`internal/templates/admin_template_resolve_test.go` 的
`TestAdminTemplateReferencesResolve` 静态检查 admin/ 下每个模板的 `extends` / `import` / `include` 落点是否
存在（并带坏样本自检防它退化成空转）。加模板或搬目录后先跑它。

## i18n key 扫描：三种取词形态都要扫

算「模板在用、但 `sys_i18n` 里没有」的 key 时，**只扫一种调用形式会少一半** —— 本轮实测：只认
`{{ .["t"]("key", "兜底") }}` 得到 128 条，补上复用形态后是 **183 条**（+43%）。

```jet
{{ .["t"]("admin.x.y", "兜底") }}                  {* ① 直调 *}
{{tr := .["t"]}} … {{tr("admin.x.y", "兜底")}}      {* ② 复用 —— 模板的事实主流 *}
{{gfTr := .["t"]}} … {{gfTr("admin.x.y", "兜底")}}  {* ③ 任意局部变量名 *}
```

- **②③ 才是主流**：Jet 里 `.["t"](…)` 不能出现在赋值右侧与 yield 参数里（见「片段复用」节），所以凡是
  range / 循环内取词都得先声明变量再调用 —— 变量名由作者定，**按变量名列举的白名单一定会漏**。
  判据按**形状**给最稳：点分 key 后面紧跟逗号与第二个字符串参数，
  `\(\s*"[a-z][a-zA-Z0-9_.]*\.[a-zA-Z0-9_.]+"\s*,\s*"`
- **Go 侧完全不在模板面上**：`shell.TranslateFor(c)` 拿到的 `tr("admin.x", "中文")` 要单独扫
  `internal/module/**/*.go`（本轮 3 条 key 只在这里找得到）
- **真源是数据库的 `sys_i18n`**（主键 `(item_key, lang)`），不是迁移文件 —— 判定要看库里有没有。
  而迁移是 seed（可重复执行），删词条时**必须同批改 `ConditionSQL` 的计数门槛**，否则下次启动会被插回去
  （见 `AGENTS.md` §「数据库」的 122/104 教训）
- **struct 作渲染 data 的模板不能写 `{{ .["t"](…) }}`**：Jet 的 chain 索引在 struct 上会运行时报错
  `can't use t as field name in struct type` —— 与上文 CSRF 的 fragment 特例**同源**，但适用范围更大：
  **任何**以 struct 为渲染 data 的模板都如此（实测 `admin/product/product_attribute_rows.html` 曾收到
  `attrRowsCtx{GroupID, Rows}`）。**做法：把片段 data 改成 `gin.H` 并走 `adminui.PrepareBare`**
  （它注入 `t` / `tf` / `lang`），取词因此留在渲染期，控制器也不需要任何取词函数 ——
  `product/inbound/http/product_attributes.go` 的 `attrRowsCtxData` 是样板：
  ```go
  func attrRowsCtxData(c *gin.Context, groupID string, rows []attrValueFormRow) gin.H {
      return adminui.PrepareBare(c, gin.H{"GroupID": groupID, "Rows": rows})
  }
  ```
  旧写法（给 struct 挂 `Tr func(key, fallback string) string` 字段、由控制器填 `shell.TranslateFor(c)`）
  是**控制器取词**，已按 `internal/module/CLAUDE.md` §inbound/http 清掉；**不要**再给 struct 加这类字段。
  注意 `gin.H` 缺键的后果：当**函数实参**用会 500（见上文「行数据是 `gin.H` 时」那节），
  所以改形状时同批改 `internal/templates/**` 的夹具。
