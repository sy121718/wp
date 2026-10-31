# user 模块改造执行记录

> 第五轮应用 `public/docs/模块改造通用规范.md`（order → mail → page → inventory → user）。
> 分支 `refactor/user-taskbook`，工作树 `.worktrees/user-refactor`，基线 main = 90c88560。
> 编辑：product-editor（只编辑）；验证、门禁、提交：主代理。

## 1 现状（本轮实测）

| 维度 | 事实 |
|---|---|
| 页面出口 | `user_page_router.go` 5 处 PageSlot：/admin/customers(:73)、/overview(:79)、/rfm(:84)、/cohort(:89)、/detail(:94)；无 BareSlot |
| 反射分页 | 2 处（`user_page.go:662` 客户列表、`:1408` RFM 页），path 传空串、Base 已由 `reqparams.FilterBaseURL` 拼好 |
| 页面视图在 dto | `dto/user_customer_page_resp.go`（9 个类型，含 Text/Title/Label/Pagination） |
| gin.H | 0 处 |
| 分页 offset | 2 处 `(page - 1) * limit`（`user_page.go:642/:1387`）；service 侧另有 `utils.NormalizeLimitOffset`（外部传值的归一入口，保留） |
| 错误域 | service 内界面 key 0 处 |

## 2 批次与提交

| 批次 | 提交 | 内容 |
|---|---|---|
| M0 | 71f6b3e5 | 五条页面出口改 `pageview.UseRenderer(adminui.RenderPage)`；六个出口改 `RenderTemplate`；两处反射分页改直接赋 `view.Pagination`；PageSlot 下限守卫 38→33 |
| M1 | bc48c1b3 | `dto/user_customer_page_resp.go` → `inbound/http/user_customer_view_handle.go`（9 个类型）；删 `presentation_owner_test.go` 的 4 处登记 |
| M3 | a061c9ab | 两处 `(page - 1) * limit` → `utils.PageOffset` |
| M2 | 核查结项 | service 内界面 key 0 处 |

## 3 判据

1. **反射分页 → 直接赋值是等价变换，前提是 path 传空**：`AttachPaginationView` 只在 `path != ""` 时
   用 `filterBaseURL` 覆盖 Base（`internal/adminui/page_pagination.go:77-79`）。user 两处都传空串、
   Base 已在 `pagination.Page` 里拼好，所以逐字照抄即等价；这与 mail 的联系人页（path 非空、必须显式拼 Base）相反。
2. **接口 DTO 与页面视图的分界看 json tag 与跨模块暴露**：`CustomerCounters`（带 json tag，
   service 与 mcp 都在用）留 dto；`CustomersListPageResp` 等 9 个纯视图类型搬进 inbound/http。
3. **搬走文件必须删门禁登记**：`presentation_owner_test.go` 的 `user_customer_page_resp.go: 4` 条目
   不删就会报「登记 4 处，实测 0 处」。
4. **adminui 不能无条件删**：搬完页面视图后 `adminui.Msg`/`ErrFact`/`PageErrorFact`/`MsgText`/
   `BulkIDsFacingText` 与 router 的 `PageAuthz` 仍在用，删 import 会编译失败。
5. **同一个模块里两套分页语义要分清**：`NormalizeLimitOffset`（外部传 limit/offset 的接口归一）
   与 `PageOffset`（页码→偏移）是两件事，不要合并成一套。

## 4 未做

- `customer_admin_resp.go` 只含接口 DTO（`CustomerCounters` 等），不需要搬。
- 3 项 DEC 仍待决（客户页 DisplayName/LabelKey 作为模板契约键的判定已登记在
  `presentation_owner_test.go` 的注释里，本轮未动）。

## 5 验收

```bash
go build ./...
go test ./internal/module/user/... ./internal/architecture/ ./public/test/user/...
bash scripts/check-all.sh          # 29/30，唯一红 check-write-fields.sh 属 main 基线
```
