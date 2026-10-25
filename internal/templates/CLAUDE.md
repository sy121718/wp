# internal/templates 约定

**本目录是前端层**：结构、样式、文案取值、显示形态只写在这里。后端不出 HTML、不算显示。

## 取词
- 页面与片段都能拿到注入的 t（func(key, fallback)）与 tf（带 %s 参数），由渲染出口注入。
- 写法：先取变量 `{{tr := .["t"]}}` 再用 tr(...)；或直接 chain 索引调用，两者等价。
- 禁止 `{{.csrf_token}}`（缺键中断整页）：存在性用 `{{if .["DevLogin"]}}` 这种 chain 写法。
- 片段数据是 struct 时不能索引取词，只能读字段；需要取词时由外层注入 t。

## 缺键与 isset
- 行元素是 struct：字段一定存在，isset 恒真，直接读。
- 数据根是 map 且键可选：读值前用 isset，否则缺键那一行中断渲染（HTTP 仍 200，后面整块消失）。
- 错误分支与空态也必须给齐模板必需键（空列表而不是缺失）。

## 判断与计算
- 徽标分档、空态、状态文案分支、选中态：模板用 eq/ne/isset/len 自己判。
- 分页窗口、页码显示：用总数/页码/每页条数自己算。
- 金额求和、库存合计这类业务口径不许在模板做（template_logic_test.go 守门）。
- range 调函数用 `{{range _, r := .Rows}}` 形式。

## 片段与复用
- include 传参只传一个值：传 struct 时片段拿不到 t，需在页面内联或由外层注入。
- import + yield 用于同文件块复用。
- 表格数据行的 td 数必须与表头列数一致（colspan 同）；空态表头常驻、空态整行进 tbody（check-empty-state-table-head.sh）。

## 写操作
写基座只有一个：**`static/js/ui/write-base.js`**（旧的 json_write / htmx_form_body /
write_feedback 三件已删除）。契约见该文件头，要点：

- **必填三个**：`data-write`（create|update|delete|bulk|custom）、`data-write-url`（必须 `/api/` 开头）、
  `data-write-status`（同表单内或宿主内有对应元素）。
- **可选六个**：`-nav`（成功后跳转，含 `{id}`）、`-reload`（裸属性，成功刷新）、`-close`（关抽屉选择器）、
  `-confirm`、`-extra`（bulk 点名要发的字段）、`-outcome`（仅 bulk）。
- **声明可以写在 form 或提交按钮上**，两者都有时按钮优先。这是「一个表单并排几个动作」的落点
  （批量启停 / 批量删除端点不同），**不是历史残留**。
- 失败不重渲染片段：4xx + JSON 由基座 toast；用户已填内容留在 DOM。

### 有意保留的整形属性（**不是残留，别删**）

| 属性 | 为什么删不掉 |
|---|---|
| `data-write-json` | HTML 表单没有嵌套语法：`inner.days=30` 绑不出 `*Inner`（实测）。行模板 → JSON 数组/对象 |
| `data-write-json-single` | 声明哪些 name 发单对象而非数组 |
| `data-write-json-wrap` | 富文本给裸 HTML，库里是 jsonb `{html: …}` |
| `data-write-lines` | 多行文本 → `[]string` |
| `data-write-keep-empty` | 空串有语义的字段（解绑）不能被删 |
| `data-write-ids-scope` | ids 在**另一个容器**里（抽屉内容被克隆到 document 级） |
| `data-write-host` | 一批隐藏表单**共用一个结论区**（放 `closest('[data-write-host]')` 里） |
| `data-write-rows` / `-object` / `-builder` | **存量兼容层**：10 处模板的行由服务端片段渲染，改成 `<template data-row>` 要连服务端一起改 |

### 已彻底删除的（写上去不报错，只是不生效）

`data-write-api`（→ `-url`）、`-mode`（→ `data-write`）、`-number` / `-float` / `-bool` / `-array`
（formdata 的 `"3"` 服务端直接绑进 int，前端不做类型转换）、`-skip-empty`（基座永远删空值）、
`-flash` / `-flash-target`（→ `-reload`，结论经 sessionStorage 跨刷新）、`data-close-on-ok`（→ `-close`）、
`data-reload-on-ok`（→ `-reload`）、`data-write-html`（→ `-json-wrap`）。

`scripts/check-write-attributes.sh` 守这张表：写表单上出现已删属性即变红。

- 非基座增强脚本不再自挂 `htmx:after:swap`：控件用 `WBUI.scan` + `MutationObserver`
  （`ui/index.js`），需要 htmx 认领新 DOM 时广播 `wb:rescan`（`ui/htmx-bridge.js` 翻译）。
  控件**不得**直接调 `htmx.*` —— `scripts/check-controls-no-htmx.sh` 守这条。

## htmx 属性（vendored 4.0.0，以 bundle 为准）
- **提交期间禁按钮写 `hx-disable="find button[type=submit]"`**。v2 的 `hx-disabled-elt` 在 v4 里
  **不存在且不报错** —— 实测（真实浏览器 + vendored bundle）：用旧属性时请求期间按钮 `disabled` 恒为
  `false`，换成 `hx-disable` 才是 `true`、settle 后恢复。写错的代价是「防重复提交静默失效」。
- 事件名 v4 一律冒号分段：`htmx:after:swap` / `htmx:before:request` / `htmx:response:error`。
  v2 的驼峰名（`htmx:afterSwap`）不再派发，写了就是死监听。
- 只能写 bundle 里真实存在的属性与元素：`scripts/check-htmx-api.sh` 机械核对四件事 ——
  v2 已删属性、v2 驼峰事件名、v2 扩展 API、以及**模板里的每个 `hx-*` 是否在 bundle 里存在**
  （最后一条是反向核对，拓「以为支持其实不支持」）。排除名单只有 `hx-progress` /
  `hx-progress-slide`（本项目自定义 CSS 类）。
- 局部回写两种写法**都合法**：`<hx-partial hx-target="#id">` 与 `<template hx type="partial" hx-target="#id">`。
  前者不是原生元素，而是 bundle 里 `#Ie` 的通用改写：`<hx-<名>>` → `<template hx type="<名>">`
  （`</hx-…>` → `</template>`），所以两者等价。实测（vendored 4.0.0，真实浏览器）：
  `<hx-partial>` 能更新目标、不留残留、含嵌套子元素也正常。
- 推而广之：`hx-<名>` 一律会被改写成 `template hx type=<名>` —— 所以「写成 `<hx-xxx>` 当普通标签用」
  不会报错也不会渲染，只会被当成一条 partial/swap 指令。

## 多端（硬规则）
- 宽度写 min(100%, 设计宽度)；绝对值用 min()/clamp() 按视口封顶，不留 right 越界。
- 触屏是独立环境：hover 规则包在 @media (hover: hover) 里，触屏等价形态用 CSSBuckets.AddHoverNone。
- 验收三条视口（1440 / 768 / 375）× 四种输入（鼠标 / 滚轮 / 触摸 / 键盘）各一次。

## 令牌（都在 ui.css，**没有** tokens.css）
- 间距 / 圆角 / 阴影 / 时长 / 缓动：`ui.css` 的 `:root`（`--ui-sp-*` / `--ui-r-*` / `--ui-shadow-*` /
  `--ui-duration-*` / `--ui-ease-*`）。**都带兜底链**（`var(--sp-xs, 6px)`）—— theme.css 缺失时仍有值，
  所以不要把它们拆到单独文件（拆了就断链）。
- 字号：`--ui-fs-2xs` … `--ui-fs-2xl`（11/12/13/14/15/16/18/22px，2026-11 补）。
  取值等于补之前各处的硬编码实际值，所以替换零视觉变化。**新规则一律用令牌**，
  存量硬编码逐条替换需要 Playwright 截图基线，是独立一批。
- 入场缓动两族并存：`spring` / `soft` / `bouncy`（linear() 采样，需 Chrome 113+）与
  `circ-out` / `circ-in-out` / `back-out` / `quint-in-out`（cubic-bezier，全平台）。
  要 Apple 式回弹用前者，要老浏览器一致用后者。
- 入场动效名一律 `<效果>-in[-<方向>]`（`fade-in-up` / `back-in-top-left`）。
  `scripts/check-frontend-naming.sh` 守命名 + 反向核对关键帧存在。

## 视觉与动效
- CSS 层次三层：页面底 + 卡片面 + 描边；只靠描边区分会读成灰蒙蒙一片。
- 亮暗两套色值都写在作用域变量组里（--dash-* 之类），只给亮色会让暗色主题变白底黑字。
- 动画/观察者/时间线改动必须读到「值在变化」；无头环境不出渲染帧，先截图强制出帧再断言。
- 读过渡中的计算样式不等于终态：先等 transitionDuration，或改读类名与规则是否在。
- 只活在内存里的类名（展开态/选中态/is-asking）刷新会丢：回填数据的那段代码要把它补上。

## 目录与命名
- admin/<模块>/：按后端模块分组；引用路径相对当前文件（Jet 不做模板根解析）。
- 改文件名/搬文件：同步 *_router.go 的模板常量与 scripts 里的清单。
- i18n key 扫描三种取词形态都要扫（t(...)、tf(...)、tr(...)）。