# 模块改造任务书 · mail（第二份，复用 order 的批次结构）

> 方法：`public/docs/模块改造通用规范.md`。范本与踩坑：`public/docs/order模块改造执行记录.md`。
> 建议分支 `refactor/mail-taskbook`，工作树 `.worktrees/mail-refactor`，基线 = order 分支合并后的 main。
> 编辑：`product-editor` 只编辑；验证与提交：主代理。

## 1 现状证据（本轮实测，行号以 main=f87b458e 为准）

| 维度 | 事实 |
|---|---|
| 规模 | 42 个非测试 go 文件；`inbound/http/mail_page.go` 79.7K 是单文件最大控制器；`mail_err.go` 19.2K、`mail_contact_write.go` 18.1K、`mail_handle.go` 12.7K、`mail_page_router.go` 11.4K |
| 页面路由 | `mail_page_router.go` 内 **10 处** `adminui.PageSlot(...)`（:53/:62/:68/:81/:101/:110/:122/:127/:131/:138），无 BareSlot；:31 注释仍写「迁移到 PageSlot 后」 |
| 反射挂分页 | 6 处 `adminui.AttachPaginationView`（`mail_page.go:498/:560/:678/:811/:1052/:1323`），:1320 注释明确「靠反射」 |
| 页面视图在 dto | `dto/mail_page_resp.go`（含 `NewCampaignsPageError`、`TriggerLabel:54`、`StepLabel:91`、`NextLabel:92`）、`dto/mail_campaign_resp.go`、`dto/mail_automation_resp.go`、`dto/mail_contact_resp.go` 四份含页面响应类型 |
| 分页语义 | model 层 10+ 处 `Offset(`，且**两种形状并存**：`(current - 1) * limit`（`mail_account_model.go:241/:341`）与外部传入 `offset`（`:448/:534`、`mail_automation_model.go:153/:262/:345`、`mail_marketing_model.go:493/:659/:857`）；尾键多为 `id DESC`，`mail_account_model.go:241` 是 `is_default DESC, id ASC` |
| gin.H | 3 处（非测试） |
| 展示成品串 | `dto/mail_option_resp.go:12` 的 `Label`（选项名，属业务值，保留）；`mail_page_resp.go` 的 `TriggerLabel/StepLabel/NextLabel`（页面视图，随视图搬包） |
| 测试 | `mail_page_router_test.go`、`mail_pagination_test.go`、`mail_contact_write_test.go`(22K)、`mail_contact_borrow_permission{, _db}_test.go`；`public/test/mail/feature` 有 `mail_contact_crud_test.go`、`mail_split_render_guard_test.go` |

## 2 目标与非目标

**目标**：10 条页面路由改为 handler 声明模板 + 统一渲染器；6 处反射分页改为直接字段；
页面视图类型搬出 dto；`Offset(` 的两种形状收敛到 `pkg/utils.PageOffset`（order 已提供）。

**非目标**：不改发信队列/模板编译/营销发送算法；不改 SMTP 与回调安全策略；
不动 `mail_termplate` 等历史命名；不改其它模块。

## 3 层归属表

| 改动域 | 层 | 本批允许 | 本批移除的越界职责 |
|---|---|---|---|
| 页面出口 | inbound/http | `binding` → service → `view.RenderTemplate` / 失败 JSON | 模板名在 router 的 PageSlot 里声明 |
| 分页事实 | inbound/http + pkg/utils | 直接赋 `view.Pagination`、`utils.PageOffset` | 反射挂载、两种 offset 形状 |
| 页面视图类型 | inbound/http | `mail_*_view_handle.go` 内定义（<域>_handle.go 合规命名） | 页面响应类型留在 dto |
| 模型查询 | model | 本模块表 SQL、唯一尾键 | 手写 `(current-1)*limit` |
| 模板 | templates | 结构/文案/选中态 | 业务计算 |

## 4 批次

### M0-1 页面路由（分三小步，每步 3–4 条）
- `adminui.PageSlot(Tpl)` → `pageview.UseRenderer(adminui.RenderPage)`；handler 末尾
  `pageview.Render(c, view)` → `pageview.RenderTemplate(c, Tpl, view)`。
- 覆盖 10 条：accounts、templates、template-editor、contacts、campaigns、campaign、
  automation、automation-runs、automation-run、automation-canvas。
- **断言**：每条页面 GET 成功返回 200 + 真实 HTML 片段；失败为 `{code,message}`；
  `mail_page_router_test.go` 与 `mail_pagination_test.go` 全绿。

### M0-2 反射分页 → 直接字段
- 6 处 `AttachPaginationView` 改 `view.Pagination = pagination.Page{...}`（取值与 Base 逐字照抄）。
- **断言**：分页链接基址与参数不变（`mail_pagination_test.go` 钉住）。

### M1 页面视图搬出 dto
- `mail_page_resp.go` / `mail_campaign_resp.go` / `mail_automation_resp.go` / `mail_contact_resp.go`
  里的页面响应类型迁到 `inbound/http/*_view_handle.go`；业务 DTO 留 dto。
- **断言**：模板字段名不变；`go build` 与 feature 绿；dto 目录只剩业务类型。

### M3 offset 收敛
- model 层 `(current - 1) * limit` 改由 service 计算并传 offset（或统一用 `utils.PageOffset`），
  不用第二套语义；唯一尾键保持。
- **断言**：越界页、空集、排序并列三例；SQL 条数不增。

### 后续
- 错误与文案域（`mail_err.go` 19.2K）：按规范 §6 分批。
- `mail_page.go` 79.7K 拆分属独立任务（按域拆 `*_page.go`），不在本任务范围。

## 5 待决 DEC（登记 `public/docs/mail业务裁决-DEC.md`）

1. 联系人列表 borrow permission（`mail_contact_borrow_permission*`）的可见范围口径是否随分页改动。
2. 活动/自动化的时区与窗口口径是否与列表分页共用过滤常量。
3. `mail_option_resp.go:12` 的 `Label` 保留为业务值（与 order 的 `Title` 同类判定）。

## 6 验证矩阵

每批：`go build ./...` + `go test ./internal/module/mail/... ./public/test/mail/...` +
`bash scripts/check-all.sh` + `git diff --check`。
合 main 前：internal+pkg 与 public 全量各一次。

## 7 禁止项

- 不改发信算法、SMTP/回调安全策略、模板编译产物。
- 不为门禁加豁免、不删用例、不放宽断言。
- 不顺手改 order/page 等其它模块，也不改 `check-write-fields.sh` 的 main 基线红。
- 不新增第二份前端词表；页面文案只走模板 t/tf 与 enums 兜底。
