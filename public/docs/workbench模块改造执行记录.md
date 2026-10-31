# workbench 模块改造执行记录

> 第六轮应用 `public/docs/模块改造通用规范.md`（order → mail → page → inventory → user → **workbench**）。
> 分支 `refactor/workbench-taskbook`，工作树 `.worktrees/workbench-refactor`，基线 main = 21cdf3f1。
> 编辑：product-editor（只编辑）；验证、门禁、提交：主代理。

## 1 现状（实测）

| 维度 | 事实 |
|---|---|
| 页面出口 | `inbound/http/router.go` 4 条：`/admin` PageSlot(DashboardPageTemplate):184、`/workbench` PageSlot(CanvasLayoutTemplate):186、`/workbench/inspector` BareSlot:191、`/workbench/outline` BareSlot:193 |
| handler 内渲染 | 8 处 `pageview.Render` + 1 处 `c.HTML(status,"workbench/error",…)` |
| router 含业务 | `Dashboard`(router.go:100-139) 在 router 里读 projects、记日志、`collectOverview`、解析区间 |
| dto | `canvas_page_resp.go`/`inspector_fragment_resp.go` 已强类型视图（`view:"…"` 小写模板键）；无 Pagination/HTTPStatus |
| 分页 | 0 处 `(page-1)*limit`、0 AttachPaginationView（本模块无列表分页） |
| gin.H | 6 处，全是 JSON 出口 / `<script type="application/json">` 元数据 |
| service 拼 HTML | `RenderOutlineHTML` 等 22 行（结构树 / 检查器正文）—— DEC-W1 |
| 错误域 | 35 个 `Err*` 与 `errFallbacks` 逐条对应（missing=0、orphan=0）；service 界面 key 0 处 |

## 2 批次与提交

| 批次 | 提交 | 内容 |
|---|---|---|
| M0 | 73db762c | `/workbench` 改 `UseRenderer(RenderPage)`、两条片段改 `UseRenderer(RenderBare)`；canvas_handle.go:118/:197/:313 与 canvas_preview_handle.go:259 → `RenderTemplate(c, CanvasLayoutTemplate, …)`；inspector_handle.go:34/:53 → InspectorPanelTemplate、:74 → OutlineTreeTemplate；PageSlot 下限 33→32 |
| M1 | 91597fc3 | `Dashboard`+`projectIDs` 搬进 workbench_page.go（:232/:281），三处 → `RenderTemplate(c, DashboardPageTemplate, …)`；`router.go → workbench_router.go`；删 `pendingControllerFiles` 登记、同步 `pendingControllerMethods` key；归口文案改 `response.InternalText`；下限 32→31 |
| M2 | (见 git log) | `dto/canvas_page_resp.go → inbound/http/canvas_view_handle.go`（CanvasPageResp、DashboardPageResp）、`dto/inspector_fragment_resp.go → inbound/http/inspector_view_handle.go`（InspectorPanelResp、OutlineTreeResp、WorkbenchErrorPageResp）；dto 只留请求绑定类型 |
| M3 | 结项 | 0 处 offset，无需改动 |
| M4 | 结项 | 35 个 Err 与兜底全对应、service 界面 key 0 处 |

## 3 判据

1. **片段必须 RenderBare**：`/workbench/inspector`、`/workbench/outline` 是 htmx 局部替换，换成 RenderPage 会把面板套上整页壳。
2. **router 只装配**：`Dashboard` 在 router 里读 service、拼区间、记日志属越界；搬进 `workbench_page.go` 后才能把
   `router.go` 改名成 `workbench_router.go`（门禁只认 `<模块>_router.go` / `<域>_handle.go`），改名后必须同步删
   `pendingControllerFiles` 登记、否则反向判据报「登记了却已合规」。
3. **控制器不 import adminui**（module/CLAUDE.md）：归口文案用 `response.InternalText(c)`（`adminui.PageInternalText`
   就是它的薄壳）；只有 router 允许 import adminui（`RenderPage`/`RenderBare` 在这）。
4. **失败页保留 `c.HTML`**：`workbench/error` 是带 4xx/5xx 状态码的独立整页外壳，`pageview.RenderTemplate` 没有状态码入口（DEC-W2）。
5. **视图类型看消费者**：五个类型的消费者全在 inbound/http（页面出口），搬走无环；`DashboardPageResp.Overview` 是 `any`，
   不需要反向引用 dto。请求绑定类型（`draft_req.go`/`preview_req.go`）留 dto。
6. **画布模板契约是小写键**：`view:"title"`/`view:"jsVer"` 这类 tag 是模板读法，搬类型时不能动。

## 4 待决（未擅改）

- **DEC-W1** service 侧拼 HTML：`RenderOutlineHTML` 等 22 行（注释称"取词在 service、无模板槽位"）。搬进模板是形状
  变更，需与前端同批。
- **DEC-W2** 失败页状态码：可选让 `WorkbenchErrorPageResp` 实现 `PageStatus() int`（pkg/view/result.go:15 支持 0 视为 200）。
- **DEC-W3** canvas meta 的 gin.H 是 JSON 出口，非模板视图，不动。

## 5 验收

```bash
go build ./...
go test ./internal/module/workbench/... ./internal/architecture/ ./public/test/workbench/...
go test ./internal/... ./pkg/...     # /tmp/wb-final-int.log
go test ./public/...                 # /tmp/wb-final-pub.log
bash scripts/check-all.sh            # 29/30，唯一红 check-write-fields.sh 属 main 基线
```
