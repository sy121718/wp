# 模块改造任务书 — workbench（第六轮）

> 依 `public/docs/模块改造通用规范.md`（order → mail → page → inventory → user → **workbench**）。
> 工作树 `.worktrees/workbench-refactor`，分支 `refactor/workbench-taskbook`，基线 main = 21cdf3f1。
> 编辑：product-editor（只编辑）；验证、门禁、提交：主代理。

## 1 现状（实测）

| 维度 | 事实 |
|---|---|
| 路由 | `inbound/http/router.go` 230 行；4 个页面出口，2 整页 + 2 片段：`/admin` PageSlot(DashboardPageTemplate):184、`/workbench` PageSlot(CanvasLayoutTemplate):186、`/workbench/inspector` BareSlot(InspectorPanelTemplate):191、`/workbench/outline` BareSlot(OutlineTreeTemplate):193 |
| 模板常量 | 同文件 :222-229（`admin/dashboard`、`workbench/layout`、`fragments/inspector_panel`、`fragments/outline_tree`），另 `workbench/error` 只在 inspector_handle.go:131 硬编码 |
| handler 内渲染 | 8 处 `pageview.Render`：router.go:105/:122/:133（Dashboard 三档）、canvas_handle.go:118/:197/:313、canvas_preview_handle.go:259；inspector_handle.go:34/:53（面板正常/空态）、:74（结构树）；失败页 `c.HTML(status,"workbench/error",…)` inspector_handle.go:131 |
| **router 含业务** | `Dashboard`（router.go:100-139）整段在 router：读 `h.projects.List`、记日志、`collectOverview`、解析区间 —— 违反「router 只装配」；门禁已把它登记进 `pendingControllerFiles`（controller_shape_test.go:96） |
| dto | 已强类型：`dto/canvas_page_resp.go`（CanvasPageResp/DashboardPageResp，带 `view:"…"` 小写模板键）、`dto/inspector_fragment_resp.go`（InspectorPanelResp/OutlineTreeResp/WorkbenchErrorPageResp）；无 Pagination/HTTPStatus 展示字段 |
| 分页 | 0 处 `(page-1)*limit`，0 AttachPaginationView（本模块无列表分页） |
| gin.H | 6 处，全是 JSON 出口或 meta 序列化（workbench_handle.go:87、canvas_handle.go:75/:152/:275、canvas_preview_handle.go:223/:285） |
| service 侧拼 HTML | `service/workbench.go:261 RenderOutlineHTML` 等共 22 行含 HTML 字面量（结构树/检查器面板正文）—— 见 DEC-W1 |
| 错误域 | enums 35 个 `Err*`，5 处 fallback 相关行；service 内界面 key 字面量 0 处 |

## 2 批次

### M0-1 画布整页出口（模板名归路由层）
- `router.go:186` `adminui.PageSlot(CanvasLayoutTemplate)` → `pageview.UseRenderer(adminui.RenderPage)`。
- `canvas_handle.go:118/:197/:313` 与 `canvas_preview_handle.go:259` 的 `pageview.Render(c, &workbenchdto.CanvasPageResp{…})` →
  `pageview.RenderTemplate(c, CanvasLayoutTemplate, &workbenchdto.CanvasPageResp{…})`。
- 断言：`canvas_handle.go`/`canvas_preview_handle.go` 内 `pageview.Render(` 归零；`router.go` 无 PageSlot 命中此路由。

### M0-2 片段出口（BareSlot → UseRenderer(RenderBare)）
- `router.go:191/:193` → `pageview.UseRenderer(adminui.RenderBare)`（**不是** RenderPage：片段换成整页渲染会给面板套页壳）。
- `inspector_handle.go:34/:53` → `RenderTemplate(c, InspectorPanelTemplate, …)`；`:74` → `RenderTemplate(c, OutlineTreeTemplate, …)`。
- 保留不动：`:131` 的 `c.HTML(status,"workbench/error",…)` —— 失败页是**带状态码的独立整页外壳**，`pageview` 只有无状态码入口（要带状态码得让视图实现 `PageStatus()`，属 DEC-W2）。这一条要写进文件注释说明为何留。

### M1 把 Dashboard 从 router 移进控制器文件
- `router.go:96-148`（`Dashboard` + `projectIDs`）整体移到 `workbench_page.go`（同包，import 不变则不动 import）。
- `router.go:184` → `pageview.UseRenderer(adminui.RenderPage)`，处理函数名保持 `h.Dashboard`。
- `workbench_page.go` 三处 `Render` → `RenderTemplate(c, DashboardPageTemplate, …)`。
- 门禁减条：`internal/architecture/controller_shape_test.go:96` 删 `workbench/inbound/http/router.go` 的 `pendingControllerFiles` 条目
  （移走后 router 只剩装配）；若 `TestControllerMethodSize` 因 `SetupWorkbenchRoutes` 行长再红，把该函数的登记保留并在回报里说明。

### M2 视图类型归位（核查，多半结项）
- dto 里的 `CanvasPageResp`/`DashboardPageResp`/`InspectorPanelResp`/`OutlineTreeResp`/`WorkbenchErrorPageResp` **已经是页面视图**，
  且消费者全是页面出口 —— 按规范 M1 的精神应搬进 `inbound/http/`。
- 判据：先 grep 确认无 `response.Success(data)` / mcp / 跨模块别名消费；全为页面出口则 `git mv dto/canvas_page_resp.go →
  inbound/http/canvas_view_handle.go`、`dto/inspector_fragment_resp.go → inbound/http/inspector_view_handle.go`，
  处理消费方前缀；**若发现任一类型被 JSON 出口消费就整份留 dto，并在回报里列出类型名与消费点**。

### M3 offset
无（0 处），结项。

### M4 错误域核查
- 35 个 `Err*` 与兜底表逐条对应（脚本核查 missing=0）；service 内界面 key 0 处。发现缺口只报不改。

## 3 待决（不擅改）

- **DEC-W1 service 侧拼 HTML**：`RenderOutlineHTML` 等 22 行在 service 拼 HTML 串（注释称"取词在 service、无模板槽位"）。
  搬进模板是形状变更，需与前端同批，本轮只登记。
- **DEC-W2 失败页状态码**：`workbench/error` 需要 4xx/5xx 状态码，`pageview.RenderTemplate` 无状态码入口。
  可选：让 `WorkbenchErrorPageResp` 实现 `PageStatus() int`（pkg/view/result.go:15 已支持 0 视为 200）。
- **DEC-W3 canvas meta/JSON**：`canvas_handle.go:152` 等 gin.H 是 `<script type="application/json">` 的元数据，属 JSON 出口，不动。

## 4 验收

```bash
go build ./...
go test ./internal/module/workbench/... ./internal/architecture/ ./public/test/workbench/...
bash scripts/check-all.sh     # 29/30，唯一红 check-write-fields.sh 属 main 基线
```
