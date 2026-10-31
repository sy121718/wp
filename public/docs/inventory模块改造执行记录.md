# inventory 模块改造执行记录

> 第四轮应用 `public/docs/模块改造通用规范.md`（order → mail → page → inventory）。
> 分支 `refactor/inventory-taskbook`，工作树 `.worktrees/inventory-refactor`，基线 main = 661a26ea。
> 编辑：product-editor（只编辑）；验证、门禁、提交：主代理。

## 1 现状（本轮实测）

| 维度 | 事实 |
|---|---|
| 页面出口 | `inventory_page_router.go` 6 处 PageSlot：/inventory(:29)、/inventory/warehouses(:33)、/inventory/reasons(:35)、/inventory/skus(:39)、/inventory/movements(:44)、/inventory/transfers(:94)；无 BareSlot |
| 反射分页 | 0 处 |
| 页面视图在 dto | `inventory_page_resp.go`、`inventory_admin_resp.go` 两份 |
| gin.H 视图 | `inventory_error_handle.go` 的 `type inventoryLoadFailedView gin.H`（失败页整页视图，键是小写 title/menu + 大写数据键） |
| 分页 offset | 4 处在 service 里写 `(page-1)*size`（transfer:734、change:331、stock:290/:1000）；model 只接形参 |
| 错误域 | service 内界面 key 0 处 |

## 2 批次与提交

| 批次 | 提交 | 内容 |
|---|---|---|
| M0 | 7b05c2d7 | 六条页面出口改 `pageview.UseRenderer(adminui.RenderPage)`；六个 handler 出口改 `RenderTemplate`；PageSlot 下限守卫 44→38 |
| M1 | d37db2c5 | 两份 dto 页面视图搬进 `inbound/http/inventory_view_handle.go` / `inventory_admin_view_handle.go`；删 `presentation_owner_test.go` 里已搬走文件的 3 处展示字段登记 |
| M3 | 1c7d31f5 | 四处 `(page-1)*size` → `utils.PageOffset` |
| M2 | 核查结项 | service 内界面 key 字面量 0 处；Err* 与兜底表同源 |

## 3 判据与取舍

1. **`_handle.go` 命名门禁同样约束视图文件**：搬进 inbound/http 的视图文件名必须 `<域>_view_handle.go`
   （`TestControllerFileNames` 只认 `<模块>_router.go` / `<域>_handle.go`）。
2. **接口读模型与页面视图的界线**：`StockSKURowResp` 由 `InventoryService.ListStockSKURows` 返回并经
   `inventorycontract` 别名跨模块暴露，属接口读模型，留 dto；页面视图反向引用 `inventorydto.X`。
   `TransferRowResp`（带 json tag、被 `TransferDetailResp` 内嵌）、`WarehouseThirdPartyResp` 同理。
3. **搬走文件要同步删门禁登记**：`presentation_owner_test.go` 的 `pendingDTOPresentation` 按文件登记
   展示字段处数（`inventory_page_resp.go: 3`），文件搬走后实测 0 处 → 直接删条目（台账只减不增）。
   忘了这步会报「登记 3 处，实测 0 处」。
4. **gin.H 失败页视图暂不动**：`inventoryLoadFailedView` 的键是模板契约，其中 `title`/`menu` 是小写，
   与 `layout.html` 的 `{{if isset(.title)}}` 耦合。改成结构体后 tab 标题会从「库存管理」回落成品牌名，
   属可见行为变化，登记为待决（见 §4），不擅自改。

## 4 待决

1. `inventoryLoadFailedView` 结构体化 + layout 的 tab 标题口径（是否统一成 `.Title`，或让 layout 同时认两种）。
2. 失败页键集是「三个页面的超集」，结构体化会同时影响 inventory.html / 仓库页 / 原因页三张模板的契约。
3. 库存数量 unknown 与 0 的区分展示（沿用商品模块 PRD-017 的 unknown 口径后再统一）。

## 5 验收

```bash
go build ./...
go test ./internal/module/inventory/... ./internal/architecture/ ./public/test/inventory/...
bash scripts/check-all.sh          # 29/30，唯一红 check-write-fields.sh 属 main 基线
```
