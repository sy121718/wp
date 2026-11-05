# internal/templates 约定

**本目录是前端层**：结构、样式、文案取值、显示形态只写在这里。后台页面、工作台外壳、访客账号页、公共 partial、静态资产（CSS/JS/vendor）都在本目录。
架构判据、响应协议、取词规则的唯一来源是 [AGENTS.md](../../AGENTS.md)；本文件只写模板侧的落地契约。

- **访客账号页刻意不引后台主题与控件基座**，要求无脚本可用。
- **一份源两个出口**：静态资产运行时经 `/static` 提供、构建期部分内联进产物，不维持两套源。
- **通用 UI 形态只有一份实现**：空态、徽标、批量条、日期筛选、抽屉、下拉一律走 partial 或 `static/js/ui/`，不在每页各写一次。
- **公共 partial 实体属主在 `pkg/view/ui/templates`**：`admin/partials/{sidebar,nav-nodes,badge,bulk_bar,csrf_field,date_filter,empty_state,pagination,toolbar_create,write_success}.html` 与根 `partials/ui_scripts.html`。应用侧不保留副本；两条源按同一逻辑键挂进一个复合 FS（先 pkg 后应用），同名即装配期报错。业务专用片段（media_field、rich_editor、stale_pages_drawer、locale_rows、media_gallery、ai_fab）仍在本目录。

## 取词
- 页面与页面内部的 `include` 直接调用 `t(key, fallback)`、`tf`（字符串参数）与 `tn`（命名参数）；无需从业务 data 读取翻译函数。
- 请求级接线为 `internal/text.AttachScope` → 携带 `RenderScope` 的响应 writer → Jet 执行期 `VarMap`（引擎在 `pkg/view/jet`）；直接 `c.HTML` 与共享页面渲染调用使用同一接线。语言与翻译闭包按请求隔离，不放进共享 Jet Set。
- `internal/text` 只组装语言与翻译作用域，不读模型、不带业务语义。JSON 错误消息仍归 `pkg/response` 出口；具体判据见根规则。
- 界面 key 直接写在模板里，例如 `{{t("admin.orders.title", "订单管理")}}`，不从模块传翻译函数或预先翻译的句子。
- `include` 的点上下文可为 struct；字段从点上下文读取，`t` / `tf` / `tn` 与 `lang` 从执行期变量读取，嵌套 include 同样可用。
- 可选 map 键先做存在性判断；struct 字段按真实类型读取。

## 缺键与 isset
- 行元素是 struct：字段一定存在，isset 恒真，直接读。
- 数据根是 map 且键可选：读值前用 isset。模板执行失败返回 `TemplateRenderError`；未写出时由失败中间件按路由声明输出 500，HTML 使用独立错误模板，数据 API 使用 JSON。错误模板二次失败不递归；已写出的响应不再重写。
- 错误分支与空态也必须给齐模板必需键（空列表而不是缺失）。

## 判断与计算
- **归前端**：结构、样式、显示格式、徽标分档、空态、状态文案分支、选中态、分页按钮与链接窗口、时间/金额的显示形态。模板用 eq/ne/isset/len 自己判。
- **归后端**：业务规则、数据库分页筛选 / count / limit / offset、金额求和、库存合计、聚合口径。这些**不许**在模板做（`template_logic_test.go` 守门）。
- **`pkg/money` / `pkg/pagination` 按函数分工**：模板侧调用金额显示与页码窗口链接（`money(...)`、`pager(...)`、`pageNumbers(...)`，封装在 `funcs.go` / `pager.go`，底层是 `money.*` 与 `pagination.Window`/`Derive`/`PageBase`/`QueryURL`）。同包的 `pagination.ClampPage`/`Slice` 是 service/model 的数据库分页工具，不属模板职责。
- range 调函数用 `{{range _, r := .Rows}}` 形式。

## 片段与复用
- 页面与局部响应复用 `include` / `import` / 参数化 `block` + `yield`；Controller 选真实模板，Jet 定义结构与 HTMX。不要为同一业务区域另写 JS 展示，不造自定义标签解析器或完整 Attribute Bag。
- include 传参作为局部点上下文；请求级取词变量不依赖该值的 map / struct 形状，无需为取词展开或复制业务数据。
- 表格数据行的 td 数必须与表头列数一致（colspan 同）；空态表头常驻、空态整行进 tbody（`check-empty-state-table-head.sh`）。

## 写操作
普通界面提交只有一个执行入口：**HTMX 4**；标准表单编码，响应由 Jet 生成 HTML。`static/js/ui/write-base.js` 负责 CSRF、响应校验、失败区与生命周期，不自行 fetch 普通表单，不再默认加载 form-json 或解释 JSON UI。

- 表单直接声明本站 `hx-post` / `hx-put` / `hx-patch` / `hx-delete`、成功目标与交换方式；不强制 `/api/` 前缀、`hx-ext="form-json"` 或 `hx-swap="none"`。上传明确 `hx-encoding="multipart/form-data"`，不手动设置 boundary。
- 确认用 `hx-confirm`；防重复用 `hx-sync="this:drop"`。独立端点按钮必须 `type="button"`，用 `hx-sync="closest form:drop"`，避免同时触发原生 submit。
- Jet 明确声明发起表单/宿主的错误区；失败 HTML 只进入该区，不替换已编辑表单，不丢文件、富文本、焦点与未保存行。不同表单错误不能串。
- 成功导航、刷新与关闭只由发起模板显式声明；创建后的界面链接由 Jet 根据实体事实生成，不依赖 JSON `data.id`，Controller 不拼 URL。失败、重定向、非法类型与迟到响应不得执行成功动作。
- 复杂编辑器只提供真实结构化业务对象，明确 JSON 输入，仍由 HTMX 发送、Jet 输出；不提供普通 UI 的 JSON 结果处理器或 JSON→DOM 注册表。
- 后台 CSRF 从表单/meta 取，访客从明确访客会话取并消费轮换；不能仅按 URL 前缀混用身份。共享基座在交换前拒绝后端导航头。

### 字段与编码

- 普通字段使用真实控件，名称与 Gin 原生 form tag 一致；集合按真实字段重复提交。Gin 不自动理解任意嵌套对象/明细下标，复杂结构用明确 JSON 请求。
- 明确 checkbox 未选、集合全取消、空数字与 0、缺失与清空的业务语义；同名 hidden + checkbox 会受 Gin 首值绑定影响，不能未经验证使用。
- ID 保持字符串，不经 JavaScript Number；金额不在 JS 做业务浮点运算。不全局删空值，不把结构 stringify 后藏普通字段，不引入通用表单 Schema。
- 旧 `data-write-url` / `-confirm` / `-extra` / `-outcome`、`-json*` / `-lines` / `-keep-empty` / `-ids-scope` / `-rows` / `-object`、`-number` / `-float` / `-bool` / `-array` / `-skip-empty`、`-api` / `-mode` / `-html` / `-flash*`、`data-close-on-ok` / `data-reload-on-ok` 均已删除；不提供旧执行器或别名回退。

`scripts/check-write-attributes.sh` 扫描所有模板标签：旧属性、未知带值/裸属性、带值行为开关及写 form 缺少唯一 HTMX 写方法、非空 target/swap 声明均失败；`data-write` 仅为认领标记，不按值选择协议。反向核对基座属性的真实消费点，`data-write-result` 是 Jet 目标钩子，不冒充基座执行属性。字段门禁按真实 handler、显式 binder 与 METHOD+PATH 对账 form/multipart 的精确 name；未知协议、动态路径与 JSON 对象提供器字段明确报未检查，不保留豁免或折叠下标。未迁页面触发门禁是待实施结果，不放宽判据。

- 非基座增强脚本不再自挂 `htmx:after:swap`：控件用 `WBUI.scan` + `MutationObserver`（`ui/index.js`），需要 htmx 认领新 DOM 时广播 `wb:rescan`（`ui/htmx-bridge.js` 翻译）。控件**不得**直接调 `htmx.*` —— `scripts/check-controls-no-htmx.sh` 守这条。

## htmx 属性（vendored 4.0.0，以 bundle 为准）
- **提交期间禁用写 `hx-disable="findAll button:not(:disabled)"`**，只匹配本来可用的按钮；v4 收尾会直接设 disabled=false，不保留原禁用态。旧 `hx-disabled-elt` 在 v4 中不存在且不报错；用真实浏览器验证禁用与 finally 恢复，不能仅看属性字符串。`hx-sync` 默认 queue first，防重复应显式 drop；不存在 `hx-timeout`，超时由真实 request/config 能力控制。
- 事件名 v4 一律冒号分段：`htmx:after:swap` / `htmx:before:request` / `htmx:response:error`。v2 的驼峰名（`htmx:afterSwap`）不再派发，写了就是死监听。
- 只能写 bundle 或实际启用扩展里真实消费的属性与元素：`scripts/check-htmx-api.sh` 核对 v2 已删属性、驼峰事件、旧扩展 API 及模板每个 `hx-*` 的来源。退役 form-json 不再作为默认依赖或属性来源；当前没有额外 hx-* 扩展属性登记，不能从 registerExtension 推定 core 支持 hx-ext。CSS 排除只有 `hx-progress` / `hx-progress-slide`，不放行任意扩展属性。
- 局部回写两种写法**都合法**：`<hx-partial hx-target="#id">` 与 `<template hx type="partial" hx-target="#id">`。前者不是原生元素，而是 bundle 里 `#Ie` 的通用改写：`<hx-<名>>` → `<template hx type="<名>">`，两者等价（实测：能更新目标、不留残留、含嵌套子元素也正常）。
- 推而广之：`hx-<名>` 一律会被改写成 `template hx type=<名>` —— 写成 `<hx-xxx>` 当普通标签用不会报错也不会渲染。
- 本仓 HTMX 4 不会仅因 HTTP >=400 自动停止 swap；共享基座必须在交换前校验状态与 HTML 类型，把失败限制到声明的错误区。路由协议不由 `HX-Request` 或 `Accept` 决定。

## 多端（硬规则）
- 宽度写 min(100%, 设计宽度)；绝对值用 min()/clamp() 按视口封顶，不留 right 越界。
- 触屏是独立环境：hover 规则包在 `@media (hover: hover)` 里，触屏等价形态用 CSSBuckets.AddHoverNone。
- 验收三条视口（1440 / 768 / 375）× 四种输入（鼠标 / 滚轮 / 触摸 / 键盘）各一次。

## 令牌（都在 ui.css，**没有** tokens.css）
- 间距 / 圆角 / 阴影 / 时长 / 缓动：`ui.css` 的 `:root`（`--ui-sp-*` / `--ui-r-*` / `--ui-shadow-*` / `--ui-duration-*` / `--ui-ease-*`）。**都带兜底链**（`var(--sp-xs, 6px)`）—— theme.css 缺失时仍有值，所以不要把它们拆到单独文件（拆了就断链）。
- 字号：`--ui-fs-2xs` … `--ui-fs-2xl`（11/12/13/14/15/16/18/22px）。**新规则一律用令牌**，存量硬编码逐条替换需要 Playwright 截图基线，是独立一批。
- 入场缓动两族并存：`spring` / `soft` / `bouncy`（linear() 采样，需 Chrome 113+）与 `circ-out` / `circ-in-out` / `back-out` / `quint-in-out`（cubic-bezier，全平台）。
- 入场动效名一律 `<效果>-in[-<方向>]`（`fade-in-up` / `back-in-top-left`）。`scripts/check-frontend-naming.sh` 守命名 + 反向核对关键帧存在。

## 视觉与动效
- CSS 层次三层：页面底 + 卡片面 + 描边；只靠描边区分会读成灰蒙蒙一片。
- 亮暗两套色值都写在作用域变量组里（--dash-* 之类），只给亮色会让暗色主题变白底黑字。
- 动画/观察者/时间线改动必须读到「值在变化」；无头环境不出渲染帧，先截图强制出帧再断言。
- 读过渡中的计算样式不等于终态：先等 transitionDuration，或改读类名与规则是否在。
- 只活在内存里的类名（展开态/选中态/is-asking）刷新会丢：回填数据的那段代码要把它补上。

## 目录与命名
- `admin/<模块>/`：按后端模块分组；引用路径相对当前文件（Jet 不做模板根解析）。
- 改文件名/搬文件：同步 `*_router.go` 的模板常量与 scripts 里的清单。
- i18n key 扫描三种取词形态都要扫（`t(...)`、`tf(...)`、`tr(...)`）。

## 待实施（前端模板统一封装）

目标：模板写法对齐 Laravel Blade 的易用性 —— **体验目标，不引入 Laravel 依赖、不照搬其全部功能、不再加一层包装链**。以下能力**尚未实现为统一封装**，现状如实记录，按优先级推进：

1. **布局与继承**：统一的页面骨架（layout + section）写法，替代每个页面重复拼壳。
2. **组件复用**：参数化组件的统一调用方式仍待整理；struct 与嵌套 include 的请求级取词作用域已接通，不再属于待实施限制。
3. **全局取词接线已完成**：请求级 `VarMap` 同时覆盖直接 HTML 与共享页面渲染，语言隔离及 struct include 已有定向回归；旧模板中的 data 取词写法随各域迁移清理。
4. **表单与交互**：写基座 `write-base.js` 已统一；表单字段与交互能力的声明式封装仍缺。
5. **分页 / 空态 / 徽标**：已有 partial，需统一调用面，避免每个模块各写一次。

这些都属**共享渲染基础设施**，一次封装、页面直接用；**不要**退化成逐模块传翻译函数或逐模块定义 UI 枚举。