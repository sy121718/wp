# 后台响应形态：什么时候出 HTML，什么时候出数据

判据只有一条 —— **这个响应是不是「导航目标」**。

| # | 场景 | 形态 | 现状 |
|---|---|---|---|
| 1 | **导航目标**：点菜单/链接、刷新、直输 URL、中键新标签、禁用 JS | 整页 HTML（SSR：Jet 渲染 + `shell.Prepare`） | 已如此 |
| 2 | **页内片段**：`hx-get`/`hx-post` 要替换某块 DOM | 片段 HTML，前端 swap（htmx 模型就是「服务端出片段、客户端替换」） | 已如此 |
| 3 | **动作回执**：提交成功/失败/提示 | **数据**：htmx → `200 + HX-Trigger` 事件；纯 fetch → `{ok,msg,redirect}`。由一层前端统一渲染提示框 | **未做**：现状 43 处 `shell.RenderJump` 用整页 HTML 说一句话，25 处 302 回跳 |
| 4 | **入口被拒**：页面 GET 鉴权失败 | 整页 + 真实状态码：未登录 302 `/admin/login`，无权限 403 + 提示页；htmx 走 `HX-Redirect` | 已落地（`shell.PageAuthz` → `shell.RejectPageRequest`） |

## 为什么 1 / 2 / 4 不能改成数据

- 这三格的请求由**浏览器发起导航**（地址栏、链接、刷新、后退），响应必须自带内容 —— 回 JSON 就是一坨报文。
- 页面被拒（第 4 格）尤其不能回 JSON：直接输 URL 的人要看得懂「没权限、去找谁」，监控要能从状态码看出失败。
- 换掉这套就得回到客户端路由（SPA），即本项目已删除的 Vue 目录那套。

## 第 3 格为什么应该是数据

- 调用方已经在页面上，客户端有能力渲染提示；服务端渲染一张整页提示页（含外壳、侧栏、导航）只为传一句话，是纯浪费，且强制一次整页重绘。
- 落地前提两条：
  1. 一层前端提示层（一个 JS + `HX-Trigger` 事件约定，或 fetch 拦截）；
  2. 无 JS 降级 —— 无 JS 时 htmx 本就不工作，因此不额外损失，但表单的 POST 回跳语义要跟着改（PRG → 回执）。
- 改动面：43 处 `shell.RenderJump` + 25 处 `c.Redirect(http.StatusFound, ...)` 逐个重判。

## 分层约定（与响应形态配套）

| 职责 | 归属 |
|---|---|
| 请求管道判定（鉴权、拒绝标记） | `internal/middleware/builtin`（如 `PageCasbinMiddleware`） |
| 页面表现（整页/片段渲染、提示出口、外壳数据） | `internal/shell`（如 `PageAuthz` / `RejectPageRequest` / `RenderJump` / `PageError`） |

中间件不认识 i18n、不认识模板；表现层不做鉴权判定。
