# 交互改动的真实浏览器实测报告（2026-09-19）

> 内部过程文档（非对外使用）

> 范围：确认批 `ad55acb8` / `4e65df46` / `a2ed4bc9` / `980edda6` 的交互改动在**真实浏览器 + 真实输入**下的行为。
> 本轮**不改任何产品代码**，只出证据。问题按 **阻塞 / 一般 / 观感** 三档标注。

---

## 0. 结论速览

| 档位 | 数量 | 条目 |
|---|---|---|
| 通过 | 12 | 见 §4 矩阵中"通过"行 |
| 失败 | 2（本批） | F1 悬浮面板「悬停后点击即关」；F2 Esc 关掉整个抽屉（预览自身 Esc 形同虚设） |
| 阻塞（**非本批引入**） | 1 | B1 `workbench/layout.html` 的 `{{if .isTemplate}}` 缺键导致 **页面/块编辑器整页渲染中断**（HTTP 200 + 截断），直接封死场景 3、场景 4 的后半段 |
| 未验证 | 4 | 见 §6（含原因与阻断点） |
| 已撤回 | 1 | 曾判 375 下工作台横向溢出 426px，**强制出帧复测后撤回**（见 §5.3） |

---

## 1. 环境

| 项 | 值 |
|---|---|
| 被测实例 | 自建，**端口 8081**，工作目录 `/home/sky/project/go/wp/tmp/head-run`（`config.yaml` 由工作区配置复制，仅改端口与日志目录；`public/` 与 `internal/` 用符号链接指回仓库） |
| 构建 | `go build -o tmp/gowp-dev-head ./cmd`，HEAD = `ad55acb8544acde3c6deca861224803c6b8fa361` |
| 用户进程 | 8080 上的 `tmp/gowp-dev`（19:58 构建，pid 2444550）**全程未碰**，仅用于"回归归属"对照（见 B1） |
| 浏览器 | ego 任务空间 `default`（Chrome，dpr 1.75），未强杀、任务结束未关闭空间 |
| 登录 | 浏览器：`GET /admin/dev-login?to=...`（debug 路由）；curl 对照：同入口取 cookie |
| 测试数据（我改动的库数据，均已列出） | `navigations 6e053460`：`panel_width` auto→**full**、`panel_block_id` → `15dab1bd`(页眉1)；`blocks` 新建 `03d6ca0f`(页眉菜单·home)；`content_templates` 新建 `adf26704`(页眉测试模板, entity_type=header，经 `POST /api/contenttemplate/create` 造数) |

### 1.1 视口控制：本机真实可设范围（实测，非推测）

命令一律是 `Emulation.setDeviceMetricsOverride`（`deviceScaleFactor:1.75`），读数是**页面内** `window.innerWidth`：

| 请求宽度 | 实测 innerWidth | 说明 |
|---|---|---|
| 1440 | 1440 | 稳定 |
| 768 | 768 | 稳定（偶发被钳到 736，重设一次即恢复） |
| 375 | **375** | **可设到**，但需要"重设 + 强制出帧"才稳定；未出帧时读到过 500 / 736（钳制） |

> 任务书提示"本机最小布局宽度约 500px、375 可能被钳制"。实测结论：**375 可以设到**，钳制是**读数时机**问题（无渲染帧时读到旧值）。诚实记录：早期几次未出帧的读数确实是 500/736，报告里凡是进入矩阵的数字都是"重设视图 → `ego_screenshot` 强制出帧 → 同一次求值内读 innerWidth"得到的。

### 1.2 两个环境陷阱（会影响一切结论，务必知道）

1. **多 page target 时 `ego_cdp` 与 `ego_js` 可能落在不同标签页**。本次中途出现过一个 `chrome://newtab` target：`Emulation.setDeviceMetricsOverride` 落在 newtab，`ego_js` 读的却是 gowp 页——表现为"设了 375 却读到 1440"。发现后 `Target.closeTarget` 关掉多余标签、确认 `Target.getTargets` 只剩一个 page 才继续。
2. **无渲染帧时 `getComputedStyle` / `getBoundingClientRect` 会返回旧值**（AGENTS.md 已写明）。实例：抽屉明明已打开（`body.drawer-open`），未出帧时读到 `transform: matrix(1,0,0,1,560,0)`（屏幕外）、按钮 `x=1465`；`ego_screenshot` 出一帧后立刻变成 `matrix(1,0,0,1,0,0)`、`x=905`。**本报告所有矩阵数据都遵循"先截图出帧再读"**。

---

## 2. 被测场景与结论

| # | 场景 | 结论 |
|---|---|---|
| 1 | 导航菜单编辑器面板操作（panelWidth auto/full、hover 预览） | **基本通过**，2 处交互缺陷（F1/F2） |
| 2 | `?menu=` 自动打开抽屉 | **通过**（4/4 次全新加载） |
| 3 | 检查器就地建菜单 | **未验证**（被 B1 封死） |
| 4 | 块编辑后 returnUrl 回跳 | **前半通过、后半未验证**（被 B1 封死） |
| 5 | 结构模板后台管理 + 可视化编辑（无样例实体模式） | **通过** |

---

## 3. 失败项与最小复现

### F1（一般）有 hover 能力的设备上，"悬停已打开"时点一下按钮 = 关掉（鼠标与合成触摸都中）

- 现象：鼠标移到「预览面板」按钮上，浮层自动打开；紧接着**点击该按钮**，浮层立刻关闭（要再点一次才打开）。触屏合成点击同样落在"已开→点即关"。
- 根因（读码 + 事件日志一致）：`admin.js` 对 hover 设备绑了 `mouseover → setOpen(true)`，而 `click` 分支是 `willOpen = !is-open` 的**纯开关**；鼠标点按钮必然先触发 `mouseover`（浮层已开），随后 `click` 就把同一浮层关掉。
- 最小复现（1440）：
  1. `/admin/navigations?kind=header&menu=<id>&project=<pid>`（抽屉自动打开，菜单项已挂面板块）
  2. `ego_hover('#btn-preview')` → 读 `pop.hidden === false`（打开）
  3. `ego_click('#btn-preview')` → 读 `pop.hidden === true`（被关掉）；再点一次又打开
  - 事件日志（capture 阶段监听）：
    - hover：`["focusin@form-input","mouseover@btn-preview",...]` → `pop:false（可见）`
    - click#1：`["focusin@btn-preview","click@btn-preview"]`（**没有新的 mouseover**）→ `pop:true（隐藏）`
- 期望 vs 实际：期望"点一下要么无变化要么保持打开"；实际"点一下反而关掉"。

### F2（一般）预览浮层打开时按 Esc：整个抽屉被关掉，预览自身的 Esc 行为不可达

- 现象：预览浮层打开时按 Esc → 抽屉关闭、`data-drawer-body` 被清空（`innerHTML.length === 0`）、预览 DOM 一并消失、焦点丢回 body。`admin.js` 里"Esc 关闭预览并把焦点还给触发按钮"这段在真实环境**观测不到**。
- 根因：`ui/drawer.js:86` 的 document 级 `keydown`（`Escape → closeDrawer()`）先跑并且不看浮层状态，抽屉被关后浮层节点已随 `body.innerHTML=''` 消失，`admin.js` 的 Esc 分支此时 `querySelector('[data-panel-preview].is-open')` 为 null 直接 return。
- 最小复现（1440）：
  1. 打开抽屉 → Tab 到「预览面板」（10 次 Tab）→ Enter 打开浮层（`pop.hidden===false`）
  2. 按 Esc
  3. 实际：`drawerHidden=true`、`bodyLen=0`、`#pop-preview` 元素不存在、`activeElement=body`
  4. 期望（按代码注释）：只关浮层、焦点回到「预览面板」按钮

### F3（观感）`SaveBlockContent` 出 500 时不落任何日志

- 我用合成 payload 直接 POST `/admin/blocks/save-content` 得到 `HTTP 500 {"code":500,"message":"系统内部错误，请稍后重试"}`，而 `tmp/head-run/.../logs/*` 里**没有对应错误记录**（`block_page_handle.go:339-344` 只在 `blocks.Update` 失败时回一句归口文案，不记 `err`）。
- 影响：这类 500 只能靠猜（我这次就无法判断是"文档校验不过"还是别的）。建议补一条结构化日志（原文进日志、文案出响应，符合既有三件套约定）。
- 说明：我的 payload 是**合成**的（`{"root":[],"settings":{}}`），不排除是我给的文档不合法；本条只针对"没有日志"这一事实。

---

## 4. 通过项（附证据）

### 4.1 场景 1/2：导航菜单编辑器（`/admin/navigations`）

| 项 | 输入方式 | 证据 |
|---|---|---|
| panelWidth 选 `full` | **键盘**：点开自绘下拉 → `ArrowDown` → `Enter` | 原生 `select.value`：`"auto"` → `"full"`；触发器文本"宽度：跟随内容"→"宽度：通栏"；菜单收起、焦点回触发器 |
| 保存面板（PRG） | **鼠标点击** `保存面板` | `POST /admin/navigations/panel` → `303` → `/admin/navigations?kind=header&menu=6e053460-…&project=…` |
| 值真的落库 | — | `select panel_width from navigations where id='6e053460…'` → `full`（此前 `auto`，`update_time` 20:18:32） |
| `?menu=` 自动开抽屉 | 全新加载 ×4 | 4/4：`drawerHidden=false`、标题 `home`、`panelWidth="full"`、`[data-drawer-auto-done]="1"` |
| 面板块下拉选「页眉1」 | **鼠标点击**（触发器 + 选项） | 原生 `select[name=panelBlockId].value='15dab1bd-…'`；保存后库里 `panel_block_id` 同步 |
| hover 打开预览 | **鼠标移入** | `pop.hidden=false`、`aria-expanded=true`、iframe `src` 首次写入、`contentDocument.body.innerHTML.length=24005` |
| mouseout 关闭 | **鼠标移出** | `pop.hidden=true`、`aria=false`、iframe `src` **保留**（符合"只首次写 src"的设计） |
| 点击浮层外部关闭 | **鼠标点击**抽屉标题 | `pop.hidden=true` 且 `drawerHidden=false`（只关浮层，抽屉保留） |
| 键盘打开预览 | **键盘**：Tab×10 到按钮 + `Enter` | 事件 `keydown[Enter] → click`；`pop.hidden=false`、iframe src 写入 |
| 横向滚轮 | **真实 wheel**（`Input.dispatchMouseEvent type=mouseWheel`） | `.table-wrap` `scrollLeft`：0 → **148.57**（deltaX=+200）→ 0（deltaX=−200）。纵向 wheel 无效是预期：外壳无纵向溢出（`docH == vh == 900`） |

### 4.2 场景 5：结构模板（`/workbench?entityType=header`，无样例实体）

| 项 | 证据 |
|---|---|
| 列表页结构模板行 | 新建 header 模板后列表出现：`页眉测试模板 / 页眉（结构） / v1 / 设为生效 / 无引用 / 2026-09-19 20:27 / 可视化编辑`；article 模板仍显示"无可视化编辑入口"（该工程 articles 表 0 行，无样例实体，属既有正确行为） |
| 编辑入口不带 entityId | `/workbench?entityType=header&projectId=…&template=adf26704-…`（对照 product 模板：`…&entityId=b92e6ce0-…`） |
| 结构模板徽标 | `#wb-template-mode` 文本"结构模板"，`offsetParent` 非空，1440/768/375 三档均可见 |
| 无实体预览真的出产物 | 画布 iframe `src=/workbench/template/preview?editor=1&entityType=header&projectId=…&template=…`（**无 entityId**），插值后 `<header class="sky-c-hdr-section sky-section" …><h2 class="sky-c-hdr-heading" …>页眉结构模板</h2></header>`，`body.innerHTML.length=24189` |
| 可视化编辑回路 | 结构树点「标题」→ 检查器出现表单（`textarea[data-wb-path=text]`）；`ego_fill` 输入"页眉测试模板-已改" + Tab → 画布 iframe 文本变为"页眉测试模板-已改"（`len=24192`），即真编辑链路在无实体模式下可用 |

### 4.3 视口 × 输入矩阵（全部"先出帧再读"）

导航菜单页（`/admin/navigations`，抽屉已自动打开 + 悬浮预览已打开）：

| 视口(请求/实测) | `scrollWidth === clientWidth` | 抽屉右边界 | 预览浮层右边界 / 宽度 | 浮层越界 | iframe 内容 |
|---|---|---|---|---|---|
| 1440 / 1440 | 1440 === 1440 ✅ | 1440 | 1440 / 559 | 否 | 24005 B |
| 768 / 768 | 768 === 768 ✅ | 768 | 768 / 559 | 否 | 24005 B |
| 375 / 375 | 375 === 375 ✅ | 375 | 375 / 345 | 否 | 24005 B |

结构模板工作台页（`/workbench?template=…&entityType=header`）：

| 视口(请求/实测) | `scrollWidth === clientWidth` | 越界元素 | 结构模板徽标 |
|---|---|---|---|
| 1440 / 1440 | 1440 === 1440 ✅ | 无 | 可见 |
| 768 / 768 | 768 === 768 ✅ | 无 | 可见 |
| 375 / 375 | 375 === 375 ✅ | 无 | 可见 |

输入覆盖统计：**点击**（抽屉行、下拉、保存、外部区域）、**拖拽**（未涉及——本轮被测控件无拖拽语义）、**滚轮/触摸板**（真实 mouseWheel ×3）、**触屏**（`Input.dispatchTouchEvent` touchStart/touchEnd ×2）、**键盘**（Tab/Enter/Esc/ArrowDown/ArrowUp）。

---

## 5. 阻塞项：B1（**不是本批引入**，但直接封死场景 3 与场景 4）

### 5.1 现象

`GET /workbench?id=<pageId>`（页面模式）与 `GET /workbench?block=<blockId>`（块模式）都是 **HTTP 200 + Content-Length 2642/2654**，HTML 在 `<span class="wb-status" id="wb-status">就绪</span>` 之后**戛然而止**：没有画布、没有 `#wb-save-draft`、`document.querySelectorAll('script').length === 0`。同一模板的**模板模式**（`?template=…&entityType=header`）却完整返回 **213774** 字节。

### 5.2 根因（已用最小程序证明）

`internal/templates/workbench/layout.html:53` 是 `{{if .isTemplate}}`，而页面模式（`workbench_handle.go:119` 一带）与块模式（`:204` 一带）的数据里**只有 `isBlock`、没有 `isTemplate`**（模板模式两键都给）。

Jet 在 key 缺失时的行为（用仓库内 jet v6 写的最小程序实测，程序放在 gitignore 的 `tmp/jettest/`）：

```
缺 isTemplate 键（页面/块模式的数据形状） -> out="HEAD" err=Jet Runtime Error ("/t":1): there is no field or method 'isTemplate' in jet.VarMap (.isTemplate)
isTemplate=false（含键）              -> out="HEADTTAIL" err=<nil>
缺 isTemplate 键且 isBlock=true（块模式） -> out="HEAD" err=Jet Runtime Error ("/t":1): there is no field or method 'isTemplate' in jet.VarMap (.isTemplate)
```

即：**缺键 → 运行时报错 → 已写出的前半截 HTML 留在响应里、状态码仍是 200**，与 `internal/templates/CLAUDE.md` 描述的现象逐字吻合。

### 5.3 归属：本批之前的存量缺陷（两处独立证据）

- `{{if .isTemplate}}` 来自 **c79623d8**（`git log -S'{{if .isTemplate}}'`），`ad55acb8^` 的 layout 与 handle 已经就是这个形状（模板模式之外的两种模式同样没有 `isTemplate` 键）。
- **用户自己的 8080 旧二进制（19:58 构建，不含本批）复现同样结果**：`curl /workbench?id=0ff14014-…` → `code:200 size:2642`，截断点完全一致。

> 本批唯一改到 `layout.html` 的是 `ad55acb8` 新增的"结构模板"徽标块（在 `isTemplate` 分支**内部**），与页面/块模式的截断无关。

### 5.4 对本轮任务的影响

- **场景 3（检查器就地建菜单）**：页面模式工作台整页截断 → 画布/检查器/结构树全都不存在 → 完全无法验证。
- **场景 4（块编辑 returnUrl 回跳）**：走通了前半段（见 §4 之外的下方证据），后半段（块保存 → 303 → 回跳）因为块编辑器同样截断而无法在浏览器里完成。

场景 4 前半段证据：抽屉里点「新建面板块并编辑」→ 服务端 303 →
`/workbench?block=03d6ca0f-57e4-4f8b-a78b-8f6bb049d02d&returnUrl=%2Fadmin%2Fnavigations%3Fkind%3Dheader%26menu%3D6e053460-…%26project%3D52935790-…`
（blocks 表新增 `03d6ca0f / 页眉菜单·home`），浏览器确实落在该 URL；随后因 B1，页面只有顶栏骨架、无 `存草稿` 按钮，链路到此为止。

### 5.5 修复方向（供主代理决策，本轮未改代码）

两条都对：
1. `layout.html:53` 改成 `{{if isset(.isTemplate)}}{{if .isTemplate}}`（或在模板里统一用 `{{if not .isBlock}}` 之类不依赖缺键的判断）；
2. 或者在 `workbench_handle.go` 的页面/块两个数据里显式补 `"isTemplate": false`。
推荐 1（与 `internal/templates/CLAUDE.md`「可选键一律 isset」的既有约定一致）；补键只是把这个坑留给下一个新增分支的人。
另外建议顺带断言：`internal/templates` 下补一条"任意 workbench 模式渲染出的 HTML 必须含 `</html>`"的守卫（与 mail 的错误分支守卫同形），否则这类截断永远只能靠肉眼发现。

---

## 6. 未验证项与原因（诚实清单）

| 项 | 原因 |
|---|---|
| 场景 3 全部 | B1：页面模式工作台整页截断，检查器根本不存在 |
| 场景 4 的"保存 → 303 回跳" | B1：块模式工作台整页截断，`存草稿` 按钮不渲染；我又直接 `POST /admin/blocks/save-content`（合法/非法/空 returnUrl 三种）全部拿到 `500 系统内部错误`，服务端未记日志，无法判断是我的合成文档不合法还是别的 |
| **真机触屏（`hover: none`）路径** | 本机 `Emulation.setTouchEmulationEnabled(true)` 后 `matchMedia('(hover: hover)')` 仍为 `true`、`navigator.maxTouchPoints` 仍为 `0`——**无法伪装成无 hover 设备**。因此只能观测到"合成触摸点击在 hover 设备上的表现"（= F1 的路径），"触屏设备上点击能正常打开"这一条**没有证据**，不做通过判定 |
| 拖拽输入 | 本轮 5 个场景的被测控件里没有拖拽语义（抽屉/下拉/浮层/结构树都是点选），未构造无意义的拖拽 |
| 结构模板的"设为生效 / 删除保护 / 影响面反查" | 属 4e65df46 的服务端行为，本轮任务是交互实测；未做（需要构造被引用页面/实例） |
| 375 的"完全无钳制" | 可设到 375（多次实测），但偶发需要重设一次；未定位钳制的确切触发条件 |

---

## 7. 我做过但需要主代理知晓的副作用

1. 库里新增/修改了测试数据（§1 已列）：`navigations.panel_width/panel_block_id`、新建 block `03d6ca0f`、新建 header content_template `adf26704`（未"设为生效"）。需要回滚的话按这三条删除/还原即可。
2. `tmp/head-run/`（隔离运行目录：config.yaml + public/internal 符号链接）与 `tmp/gowp-dev-head`（二进制）留在 gitignore 的 `tmp/` 下；`tmp/jettest/main.go` 是本次的 Jet 最小复现程序，同属 `tmp/`。
3. 浏览器任务空间 `default` **保持打开**（保留登录态），未强杀浏览器。
4. 全程未执行 `git add/commit`，未改任何产品代码。

---

## 8. 已撤回的结论（记录过程，避免被后来者当成结论）

一度判定"375 下工作台页存在横向溢出（`scrollWidth=426 > clientWidth=375`，越界元素是 `#wb-immersive 聚焦模式`）"。**撤回**：该读数是切换视口后**未强制出帧**取得的旧值；补 `ego_screenshot` 出帧后同一页面在 375 下 `scrollWidth === clientWidth === 375`、越界元素为空（§4.3 表）。同一页面在页面模式（截断状态下）与模板模式两种状态下复测均为无溢出。
