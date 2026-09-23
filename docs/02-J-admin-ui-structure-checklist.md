# 后台 UI 结构布局检测清单（子代理专用）

> 用途：对 `/admin` 下每个页面做**结构性**检测，产出可验证的问题清单。
> 判据源自 `admin-ui-logic` 技能 + `docs/02-H-admin-page-shell.md` + `docs/02-I-admin-model.md`。
>
> **只报结构布局类问题**（信息架构 / 区块归属 / 列表形态 / 布局效率 / 控件选型）。
> 纯视觉（颜色、字号、间距的观感）不在本清单范围 —— 那属于 `ui-refit`。
>
> 每条问题必须带**可复现证据**：DOM 查询结果、文件行号、实测数字。**没证据 = 不许写。**

## A. 一页多职能（最高优先级）

| 检测 | 怎么测 | 判据 |
|---|---|---|
| A1 页面有几个 `.card` / `<section>`？ | `document.querySelectorAll('.card').length` | 单职能 ≤ 2；≥ 3 要逐个问「它服务本页主语吗」 |
| A2 是否有**只读**的监控/统计区块占据主区？ | 找无 `<form>`、无按钮、无链接的 `.card` | 只读面板不该占列表上方的主位（`blocks.html` 的「待重建影响面」是样本） |
| A3 区块的主语与本页主语是否一致？ | 读每个卡的标题 + 内容，写出「它的主语」 | 主语不同的区块应换页（`admin-ui-logic` §1） |
| A4 有几个职能需要折叠才能装下？ | `document.querySelectorAll('.section-fold').length` | 一页需要折叠才能容纳 4 件事 → 说明它是 4 页 |

## B. 并列表格 / 分段列表

| 检测 | 怎么测 | 判据 |
|---|---|---|
| B1 一页有几张 `<table class="data-table">`？ | `document.querySelectorAll('table.data-table').length` | **同一实体**被拆成多张表 = 结构问题（应按某个字段分档，用列 + 徽章表达） |
| B2 多张表的**列头是否一致**？ | 逐表取 `thead th` 文本数组，比对 | 同实体不同列头 → 用户在三段间要重新适应列布局 |
| B3 全选框是否有嵌套条件？ | `grep -n 'data-check-all'` 看上下文 | 出现三层 `{{if}}` 嵌套（为「哪段有数据哪段才渲染」）→ 是「多张表」这个设计逼出来的，属设计缺陷信号 |
| B4 是否缺筛选 / 搜索 / 分页？ | `grep -c 'filter-bar'`、`pagination`、`name="keyword"` | 列表页应有筛选栏 + 服务端分页（`02-I` §4.2） |

## C. 列语义

| 检测 | 怎么测 | 判据 |
|---|---|---|
| C1 一列是否塞了两个概念？ | 读列名 + 单元格内容的实际含义 | 违反 `admin-ui-logic` §4「一列一个概念」 |
| C2 同页是否同词异义？ | 收集页面上重复出现的关键词（列名 / 卡标题 / 按钮），看是否指不同东西 | 如 `blocks` 页「影响面」既指引用数、又指待重建页面清单 |
| C3 是否有单元格输出「句子」而非值？ | 找含 `·`、`/`、`未指定`、`N 个` 的单元格 | 违反 §4「单元格是值不是句子」 |
| C4 统计是否混进列表正文？ | 看列表标题旁是否有 `合计 N 条`、`手工 N · 自动 N` | 违反 §4「统计不进列表正文」 |

## D. 列表形态与操作列

| 检测 | 怎么测 | 判据 |
|---|---|---|
| D1 是否为表格 + 末列 `.col-actions`？ | `document.querySelectorAll('.col-actions').length` | 展开式（`<details>`）列表用于结构统一的写场景 = 违规 |
| D2 行内操作按钮文案是否带实体名？ | 读 `.col-actions` 内按钮文本 | 应写「删除」而非「删除仓库」（行已指明是谁） |
| D3 危险操作是否有二次确认？ | `grep -n 'data-confirm'` | 删除类必须有 `data-confirm` + `data-confirm-danger` |
| D4 是否支持批量？ | `data-check-all` + `.bulk-bar` | 行数 > 5 的列表应有批量选择（`02-I` §4.2） |
| D5 每行是否有独立 `<form>`？ | `document.querySelectorAll('td form').length` + `template[id^=tpl-]` 数量 | 「行内表单不嵌套」——应改 `form="row-form-ID"` + 表格外隐藏表单（`menus.html` 的 118 模板是反例样本） |

## E. 抽屉与控件

| 检测 | 怎么测 | 判据 |
|---|---|---|
| E1 抽屉模板是否随页面全量下发？ | `document.querySelectorAll('template[id^=tpl-]').length` + 累计 `content` 节点数 + `htmlKB` | 每行一份模板 → 体积随行数线性膨胀（`menus.html` 4603 节点） |
| E2 下拉选项是否同层级？ | 读 `<select>` 的 option 列表 | 选项混了不同维度（位置/用途/版式/机制）→ 应分组或换控件（§8） |
| E3 描述类字段是否用了单行 input？ | `grep 'type="text" name="(description|body|content)'` | 违反 §8 硬规则 |
| E4 SEO/meta 字段是否用了富文本？ | 找 `seoDescription` 附近的 `trix-editor` | 违反 §8 硬规则（meta 必须纯文本） |
| E5 单选项控件是否在无选项时渲染？ | 看工程选择器 `<select name="project">` | 单工程时应条件渲染（`len(.Projects) > 1`） |

## F. 说明文字与布局效率

| 检测 | 怎么测 | 判据 |
|---|---|---|
| F1 正文说明文字占比 | `[...document.querySelectorAll('.hint,.text-mute,.form-hint,.page-sub')].reduce((s,e)=>s+e.getBoundingClientRect().height,0)/innerHeight` | > 0.066（60px/908px）即违规（§2.1 硬规则）。**选择器需补充**：`.perm-intro,.perm-hint` 等同义类名（`role_permissions.html:24/54` 就漏检了），建议用 `[class*="-intro"],[class*="-hint"]` 兜一层 |
| F2 主行动是否与筛选同行？ | 看 `.page-head .page-actions` 是否含主按钮 | 若「新建 X」单独占一张卡 → 违规（§2.2 硬规则） |
| F3 首屏是否有数据行？ | 表格首行底边 ≤ 视口高 | 首屏 0 行数据 = 违规 |
| F4 页面高度 | `document.documentElement.scrollHeight/innerHeight` | 单职能 ≤ 1 屏；多职能 ≤ 2 屏 |

## G. 空态

| 检测 | 怎么测 | 判据 |
|---|---|---|
| G1 空态是否含主行动？ | 空态区块内是否有按钮/链接 | 应「标题 + 一句话 + 主按钮」（§7） |
| G2 空态文案与 UI 是否一致？ | **读文案，然后验证文案提到的控件真的存在** | `inventory.html` 的空态说「用上面的表单入库」但页面无入库表单——这是同类缺陷的样本，每页都要查 |

## H. 检测方法论的坑（本轮实测踩过，必读）

**H1. 统计表单字段必须解析 `<form>` 边界，不能只看 action 列表。**
误判实例：`product_edit.html` 的 `/admin/products/update` 表单从 **60 行开到 197 行**，
「属性引用」（120）/「分类与品牌」（140）/「手工标签」（176）三段**都在这个表单内**。
若按「页面出现过哪些 `action=`」统计，会得出「三段只有标题没有表单」的错误结论
（两个独立代理都踩了，报告里报成 P0「全站无属性/分类/品牌入口」）。

判定方法：用行号确定每个 `<form>` 的起止区间，再判断目标字段落在哪个区间内：
```bash
grep -nE '<form|</form>' internal/templates/admin/<file>.html
```

**H2. 统计字段必须跟进 `{{include}}` 引入的片段。**
误判实例：`products_new.html` 全文只有 48 行、行内看不到几个 `name=`，
但第 21 行 `{{include "partials/product_create_form.html"}}` 引入了 148 行的共享表单
（注释明确写着「与列表页抽屉共用同一份建表单片段」）。
只看调用页会得出「只有基础字段」的错误结论。

判定方法：脚本统计前先解出所有 include/import 的片段，或用
`{{include}}` 的 grep 结果人工确认。

**H3. 全选框作用域取 `closest('form')`，不是「一张表一个」。**
误判实例（**本条是父 agent 的判断错误，被实测推翻**）：
`blocks.html` 有三段 `<table>` 却只有一个 `data-check-all`，一度被判为「段间漏选」。
实测**不成立** —— `admin.js:522` 的 `scopeOf(el)` 取 `el.closest('form')`，
而三段表**同处一个 `<form>`**（`blocks.html:143` 的 form 包住整个 `.list-card`），
所以全选联动正常（实测勾选 3 行 → 「已选 3 项」）。

判定方法：先确认 `data-check-all` 与 `data-check-item` 是否在同一 `<form>` 内：
```bash
grep -nE '<form|</form>|data-check-all|data-check-item' internal/templates/admin/<file>.html
```
**教训**：从源码结构推断运行时行为必须用浏览器实测复核 —— 这一条正是「核对了自己写下的配置、
没核对系统实际做的事」的又一例（`AGENTS.md` 已记录过同类教训）。

**H4. `hintReal` 必须排除表格内单元格。**
（已在 §F1 说明，此处重申因为它是本轮最大的一类假阳性。）
`products` 53/53、`articles` 50/50、`menus` 117/117、`dashboard` 8/8 的 `.text-mute` 全在 `<td>` 内，
修正口径后**全站真实正文说明为 0**。

**H5. 页面必须带必需的 query 参数才能测。**
`page_translations` 需 `?pageId=`、`role_permissions` 需 `?role_id=`、`mail_campaign` 需 `?id=`、
`product_detail_template` 需 `?product=` —— 裸访问会 302，测出来的「空白页」不是缺陷。

**H6. 读数前必须断言页面身份。**
浏览器单例 + 并行任务会互相抢占页面，且**任一任务执行 `/admin/dev-login` 会轮换会话、
踢掉其它任务的登录态**（本轮实测：一个批量脚本 38 页全部落到 `/admin/login`）。
每次读数前断言 `location.pathname`，不一致就重登重取。
**并行扫描的正确做法是独立 Chrome 实例**（puppeteer-core + 独立 `user-data-dir`），
而不是共用 ego 浏览器的不同 task space。

**H7. `hintReal` 的扩展选择器会命中布局壳的侧栏元素。**
若用 `[class*="-hint"]` 兜底同义类名，会命中 `partials/sidebar.html:49` 的 `.pin-hint`
（「点击菜单后收起」，高 **116px**）—— 管理域每页都因此假报 `hintReal=0.127`（超阈值 1.9 倍）。
排除侧栏后 6 页真实值为 **0**。与 §H4（表格单元格假阳性）同源：**度量必须排除「不属于页面正文」的容器**。
排除列表应为：`table / .help-pop / template / aside / [data-sidebar]`。

**H8. 客户端过滤 + 批量选择必须实测两个方向。**
`admin.js` 在「筛掉行时撤销勾选」（`applyFilter`）这一方向是**对的**（有注释），
但「全选不尊重可见性」（全选循环不过滤 `hidden`）就在**同一段代码**里。
只读源码会得出「已处理」的错误结论。判据：任何「客户端过滤 + 批量选择」组合都要分别实测
**「先过滤再全选」**与**「先全选再过滤」**两个方向。
实测反例（`/admin/menus`）：过滤「商品」→ 可见 17/117 行 → 点全选 → 实际勾中 **117 项**
（**100 项是隐藏行**）→ 批量删除表单会提交 **117 个 id**。**静默越界删除。**

**H9. 服务端筛选可能是「死控件」。**
模板有筛选输入框 + 「筛选」按钮、`value="{{...}}"` 回显位、注释还写着「服务端筛选：条件进 SQL」，
但 handler **从不读 query** —— UI 完整、按钮可点、**无任何报错**，输入后页面重载、结果一条没变、
输入框还清空。用户会以为「没有这条记录」。
判据：**每个筛选栏都要实测「输入一个不可能命中的值 → 行数是否变化 + 输入是否回显」**。
实测命中的三处：`administrators`（name/email）、`roles`（keyword）、`datarules`（domain）；
正确样本只有 `permissions`（`?code=` 回显且 20 行→0 行）。

**H10. 「同一函数里修一半」是常见遗漏形态。**
`admin_pages_handle.go` 的 `DatarulesEditPage`：**缺 id** 分支已修（303 + `?err=`），
但**同一函数**的「id 存在但查不到」分支仍是 `404 + 裸文本`。
判据：改一个失败出口时，**把同一函数里所有失败分支列出来逐个过**，不要只改报告点名的那一个。

**H11. 折叠/展开类判据不要写死起始值 —— 先求最小值。**
权限树实测顶层是 `data-depth="1"`（分布 1:8 / 2:43 / 3:65），**不是 0**。
写死 `depth !== 0` 时折叠**静默不生效**（`hiddenRows:0`，没有任何报错、没有日志），
页高停在 4.15 屏却看不出原因。
判据：**先求 rows 里的最小 depth 再据此折叠**，不要假定起始值。
推广：任何「按层级/序号字段分组」的逻辑，都先求实际取值范围，不假定从 0 或 1 开始。

**H12. 验证「失败出口」前，先证明请求真的到达了 handler。**
401（未登录）/ 403（无权限）/ 404（路由不存在）/ CSRF 失败都发生在**中间件或路由层**，
与 handler 的失败出口无关。用 `redirect:'manual'` 时拿到的 `opaqueredirect`（status 0、body 空）
也容易被误读成「响应不对」。
实测踩过的坑：某代理测 `departments.html` 时读顶层 DOM 得到 `csrfLen=0` 并上报为「缺 CSRF 隐藏域」，
实际抽屉表单克隆后有 `csrfLen:64`（表单在 `<template>` 里，未展开时不在文档树）。
判据：**断言失败时先区分「中间件层拒绝」与「handler 出口形态」**，并确认你读到的是目标页面。

**H13. `location` 断言成立 ≠ 页面存在：必须同时断言「这是一张 HTML 文档」。**

2026-09 实测踩过：`/admin/dashboard` 是 **404**（真实路由是 `/admin`，见
`internal/module/workbench/inbound/http/router.go:220`），但浏览器把 `application/json`
的 404 响应也包成一张文档，`location.pathname` 仍是 `/admin/dashboard` —— 于是逐页验证台
判定「到位」，三条读数 `tables=0`、`h1=null`、`emptyStates=0` 被当成「这个页面没有表格」的
证据用了下去（恰好与「空态吃掉表头」的读数形状一致，差点被并进结论）。

判据：**取 `document.contentType`，不是 `text/html` 就判「不可信」**。
`h1 === null` 同样是一个可疑信号 —— 后台页壳恒有标题，读到 null 时先怀疑「这不是页面」，
而不是「这页没有标题」。

由此得出一条更一般的纪律：**同一批读数全部来自同一个错误前提时，它们互相印证也没有意义**。
三条读数都指向「没有表格」并不构成证据链，除非先证明「这确实是一张页面」。

```
## <模板名> (<URL>)
度量：screens=<n> hintRatio=<n> cards=<n> tables=<n> rows=<n> visRows=<n> forms=<n> templates=<n>(<节点>) htmlKB=<n> checkAll=<n> colActions=<n> filterBar=<n> pageHead=<n>
结构：<页面做什么的一句话职能>
[A1/P0] <问题一句话>
  证据：<DOM 查询 / 行号 / 数字>
  影响：<用户会怎么做错>
  修法：<具体到类名或结构>
（无问题写：PASS — 检测了 A1-A4, B1-B4, C1-C4, D1-D5, E1-E5, F1-F4, G1-G2）
```

**要求**：
- 检测项要**逐项过**，PASS 时列出实际检测过的项，不许笼统写「无问题」；
- 读数前校验 `location.href` 与目标一致（浏览器单例，可能被其他任务抢占）；
- 数字必须真实实测；行号必须真实存在；**严禁编造**；
- 发现的问题若已在 `docs/02-I-admin-model.md` §5.2 记录，标「已在模型文档记录」并补充新细节。
