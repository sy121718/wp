# page 模块改造执行记录

> 任务书：本记录对应 `public/docs/模块改造通用规范.md` 的第三轮应用（order → mail → page）。
> 分支 `refactor/page-taskbook`，工作树 `.worktrees/page-refactor`，基线 main = e7f8b549。
> 编辑：product-editor（只编辑）；验证、门禁、提交：主代理。

## 1 现状（本轮实测，基线行号）

| 维度 | 事实 |
|---|---|
| 页面出口 | `page_router.go` 5 处：/pages（PagesListPageTemplate:141）、POST /workbench/history（BareSlot PageHistoryListTemplate:158，片段）、/pages/translations（:188）、/page-translation-misses（:202）、GET /api/page/redirect（RedirectPageTemplate:217，路由挂在 /api 组但渲染整页） |
| 反射分页 | 0 处（page 模块没有 AttachPaginationView） |
| gin.H | 3 处，全部是 JSON 出口（`pages_handle.go:955`、`page_translations_handle.go:411/:463`），不留页 |
| 页面视图在 dto | `page_redirect_page_resp.go`、`page_langs_resp.go`、`page_state_resp.go`、`page_view_resp.go` 四份 |
| 分页 offset | 1 处 `Offset(keep-1)`（`page_model.go:763`，保留最近 N 个版本，不是分页），不属收敛范围 |
| 错误域 | 30 个 Err* 常量与 errFallbacks 30 条逐条对应，service 内界面 key 0 处 |

## 2 批次与提交

| 批次 | 提交 | 内容 |
|---|---|---|
| M0 | 1faad3c5 | 五条页面出口改 `pageview.UseRenderer`（历史片段用 `adminui.RenderBare`）；六个 handler 出口改 `RenderTemplate`；PageSlot 下限守卫 48→44 |
| M1 | 53240d0e | 四份 dto 页面视图整体搬进 `inbound/http/*_view_handle.go`（`page_redirect_page_view_handle.go`、`page_view_handle.go`、`page_state_view_handle.go`），`page_view_resp.go` 清空后删除 |
| M2 | 核查结项 | 30 个 Err* 与 errFallbacks 逐条对应（missing=0），service 界面 key 0 处 |

## 3 判据

1. **片段出口与整页出口用不同渲染器**：`POST /workbench/history` 原挂 `adminui.BareSlot`，
   迁移必须用 `pageview.UseRenderer(adminui.RenderBare)`；换成 RenderPage 会给修订历史套上页壳。
2. **路由所在分组不代表出口形态**：`GET /api/page/redirect` 挂在 /api 组（为了让 Session/CSRF/Casbin
   三层链与接口一致），但它渲染整页 `admin/page/page_redirects.html`，所以是 RenderPage。
3. **`c.HTML` 不都要迁**：`page_translations_handle.go:475` 的 `PageTranslationErrorsTemplate`
   是 htmx 就地替换的逐行错误片段，不是页面出口，保持原样（迁它会把片段变成整页）。
4. **搬视图类型时不能只搬一半**：`StaleItemResp` 被同文件 `StaleOverviewResp` 引用，只搬前者会在
   `dto ↔ inbound/http` 之间形成 import 环；两者一起搬（消费者全是页面出口）即无环。
5. **哪些类型必须留 dto**：带 json tag 且经 `pagecontract.*` 别名跨模块暴露的（`PageLangStateResp`）、
   请求类型、接口投影（`RedirectListResp`/`RedirectProjectOptionResp`/`RedirectItemResp`）。
6. **测试里的 `adminui.PageSlot` 不用改**：`PageSlot` 与 `UseRenderer` 都注入渲染器，
   `RenderTemplate` 只覆盖模板名，两种装配兼容（page 的 8 个测试文件因此零改动）。

## 4 未做

- 3 处 JSON 出口的 `gin.H`：属响应形状，不是页面视图，不在本任务范围。
- `page_model.go:763` 的 `Offset(keep-1)` 是「保留最近 N 个修订」的业务语义，不是分页 offset，
  不换成 `utils.PageOffset`。
- 3 项 DEC 仍待决（见任务书 §5），未擅自决定。

## 5 验收

```bash
go build ./...
go test ./internal/module/page/... ./internal/architecture/ ./public/test/page/... ./public/test/security/...
bash scripts/check-all.sh          # 29/30，唯一红 check-write-fields.sh 属 main 基线
```
