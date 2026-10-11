# 后台响应形态：什么时候出 HTML，什么时候出数据

判据只有一条 —— **这个响应是不是「导航目标」**。

| # | 场景 | 形态 | 现状 |
|---|---|---|---|
| 1 | **导航目标**：点菜单/链接、刷新、直输 URL、中键新标签、禁用 JS | 整页 HTML（SSR：Jet 渲染 + `adminui.Prepare`） | 已如此 |
| 2 | **页内片段**：`hx-get`/`hx-post` 要替换某块 DOM | 片段 HTML，前端 swap（htmx 模型就是「服务端出片段、客户端替换」） | 已如此 |
| 3 | **动作回执**：提交成功/失败/提示 | **数据**：htmx → `200 + HX-Trigger` 事件；纯 fetch → `{ok,msg,redirect}`。由一层前端统一渲染提示框 | **尚未全量完成（架构债）**：JSON 写已落地，仍有 33 处 `shell.RenderJump` 用整页 HTML 说一句话，另有 28 处 `c.Redirect(http.StatusFound, ...)`（后者含登录/入口跳转，见下表口径） |
| 4 | **入口被拒**：页面 GET 鉴权失败 | 整页 + 真实状态码：未登录 302 `/admin/login`，无权限 403 + 提示页；htmx 走 `HX-Redirect` | 已落地（`adminui.PageAuthz` → `adminui.Reject`；task-92 批 4a 自 `internal/shell` 迁入，路由侧唯一约定写法随之改名） |

## 计数口径与时点（第 3 格那两个数怎么来的）

**改这两个数时，连口径与时点一起改** —— 本文件以前的漂移就起于「只留一个数字、不留下怎么数的」。

| 数 | 口径 | 复算命令 |
|---|---|---|
| 33 | `shell.RenderJump` 调用点，**与 `scripts/check-shell-migration.sh` 判据 2 同一口径**：只数 `internal/module/*/inbound/http/*.go` 的**非测试**文件、跳过整行注释（注释里的调用点不计） | `rg -n '\bshell\.RenderJump\(' internal/module/*/inbound/http/*.go --glob '!*_test.go'` 列定位证据；对源文本剔整行 `//` 后按该正则统计出现次数（一行多处须分别计） |
| 28 | `c.Redirect(http.StatusFound, ...)`，**全仓非测试 `*.go`（`internal` / `pkg` / `cmd`）**、跳过整行注释（`internal/shell/` 自身 0 处，排不排除都一样） | `rg -n 'c\.Redirect\(http\.StatusFound' --glob '*.go' --glob '!*_test.go' internal pkg cmd` 列定位证据；对源文本剔整行 `//` 后按该正则统计出现次数 |

时点：**2026-10-10 12:08:15 +08:00（task-124，当前共享工作树静态复算）**。计数单位是匹配出现次数，不是行数；与门禁一样只剔行首可选空白后的 `//` 整行注释，不额外剔块注释或行尾注释。两点必须知道：

1. 这两个数**随迁移下降**，写在文档里的一定是当时的快照；引用时先说时点，别把它当不变量。
2. 28 处里**有相当一部分不是动作回执**，而是入口/登录跳转（`internal/middleware/builtin/auth.go:129`（`loginPagePath`）、`internal/adminui/reject.go:83`（`adminLoginPath`）、`internal/module/user/inbound/http/user_page.go` 的 9 处 `/user/login` 字面量）。所以「25 处回列表页」这类**分类数不可复算**：机械可数的只有总数，回执 / 入口的划分要逐个读调用点。**不要把人工分类的结果写成计数**。

## 为什么 1 / 2 / 4 不能改成数据

- 这三格的请求由**浏览器发起导航**（地址栏、链接、刷新、后退），响应必须自带内容 —— 回 JSON 就是一坨报文。
- 页面被拒（第 4 格）尤其不能回 JSON：直接输 URL 的人要看得懂「没权限、去找谁」，监控要能从状态码看出失败。
- 换掉这套就得回到客户端路由（SPA），即本项目已删除的 Vue 目录那套。

## 第 3 格为什么应该是数据

- 调用方已经在页面上，客户端有能力渲染提示；服务端渲染一张整页提示页（含外壳、侧栏、导航）只为传一句话，是纯浪费，且强制一次整页重绘。
- 落地前提两条：
  1. 一层前端提示层（一个 JS + `HX-Trigger` 事件约定，或 fetch 拦截）；
  2. 无 JS 降级 —— 无 JS 时 htmx 本就不工作，因此不额外损失，但表单的 POST 回跳语义要跟着改（PRG → 回执）。
- 剩余复判面：33 处 `shell.RenderJump` + 28 处 `c.Redirect(http.StatusFound, ...)`（口径与时点见上「计数口径与时点」）逐个重判。

## 分层约定（与响应形态配套）

| 职责 | 归属（**现状**） |
|---|---|
| 请求管道判定（鉴权、拒绝标记） | `internal/middleware/builtin` —— `PageCasbinMiddleware(As)` 只回答「能不能过」，出口以函数值注入（`PageRejectFunc = func(c, status, key)`）；本层不认 HTML、不认 i18n |
| 页面被拒的**出口**（302 `/admin/login` / 403 整页提示 / htmx `HX-Redirect`） | `internal/adminui` 的 `Reject`。它是叶包：本仓内只依赖 `internal/middleware/builtin` / `internal/templates` / `pkg/response` / admin 模块的 `dto` / `internal/builder/core` / `pkg/pathkit` / `pkg/safepath` / `pkg/logger`（外加 gin 与标准库；清单可复算：`go list -f '{{join .Imports "\n"}}' ./internal/adminui`），**不 import `internal/shell`**（反向即成导入环）；壳键由调用方经 `ShellInjector`（签名与 `adminui.Prepare` 逐字一致）注入 |
| 壳键装配、页面表现（整页/片段渲染、提示出口、外壳数据） | 装配归 `internal/adminui`（`adminui.Prepare`，task-92 批 2a 自 shell 迁入）；页面错误出口与受控文本也归 `internal/adminui`（`PageErrorBadRequest` / `AdminWriteFailedText` / `PageInternalText` / `PageErrorInternal`，批 2b-1 与 task-114 迁入）；**页面鉴权判定与拒绝出口也已离开壳层**（`adminui.PageAuthz` / `PageAuthzAs` / `Reject`，批 4a 迁入 —— 20 个模块 router 文件的 **73 处**调用点同批改名）；`internal/shell` 的 `Prepare` 转发已删除（order 五个生产文件六处调用已直引 `adminui.Prepare`）；剩下的过渡面是 `RenderJump` / `Jump` / `TranslateFor` / `BackPath` / `BulkIDs` 一族等，逐 scope 计数在 `scripts/shell-migration-allow.txt`（口径见 `scripts/check-shell-migration.sh` 文件头） |

中间件不认识 i18n、不认识模板；表现层不做鉴权判定。

**这是迁移过渡形态，不是终局** —— 终局归属写在 `internal/shell/shell_migration_test.go` 的迁移计划表里（判定 → 中间件侧；仍在壳层的 `RenderJump` / `Jump` → admin 侧，即今天的 `internal/adminui`；`Prepare`、页面错误与鉴权出口已完成迁出，导出计划仅登记仍存符号），`internal/shell` 最后整体删除。**别把「出口已迁到 `adminui`」读成「shell 已经删掉了」**：上「计数口径与时点」里的数只说页面写出口还没迁完。实现已迁的部分：`Prepare`（实现在批 2a 迁入 `internal/adminui/prepare.go:53`；2026-10-10 最后六处 order 生产调用改为直引，`shell.Prepare` 转发与奇偶校验已删除，装配契约和真实渲染测试保留）；`PageErrorBadRequest` / `AdminWriteFailedText` / `PageInternalText` / `PageError`（批 2b-1、task-114）；`PageAuthz` / `PageAuthzAs` / `RejectPageRequest`（批 4a，本文件第 4 格与上「分层约定」表同批改）。仍在壳层的是 `RenderJump` / `Jump` / `TranslateFor` / `BackPath` / `BulkIDs` 一族 —— 逐条归属与删除条件都在迁移计划表里。

页面鉴权入口统一写作 `adminui.PageAuthz(As)`。`scripts/check-page-get-authz.sh` 扫描实际 Go 注册行，先剔除行注释与块注释，再提取 `(页面, obj, act)`；改入口写法必须同步更新扫描器。判据逐菜单比较该菜单绑定的码集合，每个实际 obj/act 都须属于本菜单，其他菜单的绑定码不能作为回退。门禁只读查询菜单及权限绑定，连接强制 `default_transaction_read_only=on`，不执行 Go 路由装配（装配会同步权限和策略）。路由存在性依据已审计的 `internal/routers/testdata/routes.snapshot`，因此本门禁不证明快照与当前运行时一致；快照更新及其运行时验证是独立流程，不能当作只读操作。对象分三类：普通 `/api/*` 对象须在快照中；仅声明无路由的 `GET /api/i18n/list`（`i18n:view`）和 `POST /api/seo/audit`（`seo:audit`）显式登记；存量页面对象 `GET /admin/orders/overview`（`order:overview`）单独登记且须在快照中。后两类还核对指定注册文件中的 `permission.Declare` 与权限常量值，删除或注释掉声明均报错；静态检查不替代运行时可达性验证。登记项失效也报错。只读查询成功后执行正常态及负向夹具自检，覆盖跨菜单借码、act 错误、无路由未登记、登记删除、声明删除、注释伪注册。汇总分别打印绑定行数、唯一菜单 path 数和实际鉴权三元组数。
