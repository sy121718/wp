# content 模块改造执行记录

> 第七轮应用 `public/docs/模块改造通用规范.md`（order → mail → page → inventory → user → workbench → **content**）。
> 分支 `refactor/content-taskbook`，工作树 `.worktrees/content-refactor`，基线 main = 7d307fb4。
> 编辑：product-editor（只编辑）；验证、门禁、提交：主代理。

## 1 现状（实测）

| 维度 | 事实 |
|---|---|
| 页面出口 | `content_page_router.go` 9 处 Slot（4 PageSlot + 5 BareSlot）+ `/articles/edit` **没有渲染装配** |
| handler 内渲染 | `content_page.go` 19 处 `pageview.Render`（整页 8 + 片段 11） |
| 分页 | 1 处 `adminui.AttachPaginationView(data, "/admin/articles", {keyword}, pagination.Page{…})`；2 处 `(page-1)*limit` |
| dto | 4 个文件混装页面视图与接口 DTO：`content_fragment_resp.go`、`content_article_edit_resp.go`、`content_option_resp.go`、`content_resp.go` |
| service 界面 key | 0 处 |
| 错误域 | `content_enums.go` 7 个 Err + `errFallbacks`（10 条）全对应；`content_admin_enums.go` 25 个 key 走**控制器侧兜底表**（`articleFacingMessages` 等）—— DEC-C1 |

## 2 批次与提交

| 批次 | 提交 | 内容 |
|---|---|---|
| C0-1 | 05906acc | 4 条整页路由改 `UseRenderer(RenderPage)`；**新增常量 `ArticleEditPageTemplate`** 并给 `/articles/edit` 补渲染装配（原先无渲染器 → 500「视图出口未装配」）；列表页分页改直接赋 `pagination.Page` 且显式 `Base`；8 处整页出口改 `RenderTemplate`；PageSlot 下限 31→27 |
| C0-2 | d06e199b | 5 条片段路由改 `UseRenderer(RenderBare)`；11 处片段出口改 `RenderTemplate`（`content_page.go` 内 `pageview.Render(` 归零） |
| C1 | (见 git log) | 16 个页面视图类型搬进 `inbound/http/`（`content_view_handle.go`、`article_edit_view_handle.go`、`content_view_options_handle.go`、`content_preview_view_handle.go`）；dto 只留 `ContentResp`（跨模块）、`ArticleStaleImpactResp`（0 消费者）与请求类型 |
| C3 | (见 git log) | 2 处 `(page-1)*limit` → `utils.PageOffset` |
| C4 | 核查结项 | service 界面 key 0 处；`content_enums.go` 的 Err 与兜底全对应（缺 0） |

## 3 判据

1. **没挂渲染器的页面路由是隐性 500**：`/articles/edit` 只有 `PageAuthz` + handler，`pageview.Render` 在
   `slotOf` 里拿不到 renderer 就报「视图出口未装配」。迁移时不能只换已有的 Slot，要顺带扫「有 Render 无装配」的路由。
2. **分页直接赋值必须补 Base**：`AttachPaginationView` 在 path 非空时用 `filterBaseURL` **覆盖** `p.Base`
   （internal/adminui/page_pagination.go:77-79）；直接赋值不补 Base 会让页码链接没有 href。
3. **片段换 RenderBare**：5 条 htmx 片段（stale drawer / score / seo drawer / preview-frame / import preview）用
   RenderPage 会被套上整页壳。
4. **混装 dto 只搬页面视图**：`content_option_resp.go` 里选项视图与 `ContentResp`（json tag + contract 跨模块）混放，
   整份搬会造 dto↔http 环；按类型切分，原文件保留剩余类型。
5. **0 消费者的 `ArticleStaleImpactResp` 不动**：删类型属清理，不在本轮范围，只点名。

## 4 待决

- **DEC-C1 兜底表位置**：`content_admin_enums.go` 的 25 个界面/错误 key 的**中文兜底在控制器侧**
  （`articleFacingMessages`/`articlePublishFacingMessages`），与「兜底跟声明处走（enums 的 errFallbacks）」
  规范有漂移；搬兜底会牵动这些 key 的取词路径与 i18n 测试，需单独立项。
- **DEC-C2 `ArticleStaleImpactResp` 无消费者**（0 处引用）：删除或改为内部形状待定。
- **DEC-C3 预览帧直出 HTML**：`ArticlePreviewFrame` 的替代页正文仍由 Go 直出 HTML（取词只能在控制器发生），
  与「后端无 HTML 字面量」门禁的既有豁免一致，未动。

## 5 验收

```bash
go build ./...
go test ./internal/module/content/... ./internal/architecture/ ./public/test/content/...
go test ./internal/... ./pkg/...     # /tmp/ct-final-int.log
go test ./public/...                 # /tmp/ct-final-pub.log
bash scripts/check-all.sh            # 29/30，唯一红 check-write-fields.sh 属 main 基线
```
