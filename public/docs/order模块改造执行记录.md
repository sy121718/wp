# order 模块改造执行记录（第一批 · 样本模块）

> 分支 `refactor/order-taskbook`，工作树 `.worktrees/order-refactor`，基线 `4a6b079c`。
> 方法：`public/docs/模块改造通用规范.md`；任务书：`public/docs/模块改造任务书-order.md`。
> 分工：`product-editor` 子代理只编辑；主代理唯一验证并提交。

## 1 已交付批次

| 批次 | 提交 | 内容 | 验收 |
|---|---|---|---|
| B0-1 | `c60cabae` | GET /orders 改用 `pageview.UseRenderer(adminui.RenderPage)` + handler 声明模板 | build 0 / order+architecture 341 / order feature 113 / 门禁 29-30 |
| B0-2a | `33b0b711` | /orders/overview、/orders/pending-ship、/order-carriers 同上；/orders/ship-form 是**片段**，改 `RenderBare` | 同上 |
| B0-2b | `3e76e16a` | /orders/new 与 POST /orders/create 同批、/returns、/coupons、/coupons/edit-form（片段） | 同上 |
| B0-3a | `d29e0833` | 三处 `adminui.AttachPaginationView` 反射挂载改为直接赋 `view.Pagination`；删对应 UI 线豁免登记 | 同上 |
| B1-1 | `2361535d` | 三份纯页面视图文件从 `dto/` 搬到 `inbound/http/`（`order_view_handle.go` / `sales_view_handle.go` / `shipment_view_handle.go`），package 改 `orderhttp` | 同上 |
| B3-1 | `52e95cd8` | `pkg/utils.PageOffset` 新增（饱和乘法），四页 offset 归一；MCP 删三个本地 clamp | 同上 + `pkg/utils` 12 例 |

## 2 判据记录（踩过的坑）

1. **片段不是整页**：`adminui.BareSlot` 的出口是 `RenderBare`（只注入取词，不套页壳）；
   迁到 `adminui.RenderPage` 会给抽屉套上页壳。判据：看原 slot 是 PageSlot 还是 BareSlot。
2. **`PageInternalText` 不是泄漏**：它返回 `response.InternalText` 的受控归口文案
   （`internal/adminui/errors.go:95-104`），用于页面降级显示。任务书 B0 原文「删 PageInternalText」
   措辞错误，正确口径是「保留，只用于降级文本」。
3. **门禁登记两张表别删错**：`internal/architecture/controller_shape_test.go` 里
   `pendingControllerFiles`（:56，文件名豁免）与 `pendingControllerUIImport`（:513，UI 线 import 豁免）
   都有同一文件条目。文件不再 import adminui 时删的是 **UI 线**那条；
   删文件名那条会让 `TestControllerFileNames` 立刻红。
4. **控制器文件命名**：只允许 `<模块>_router.go` / `<域>_handle.go`。新增视图文件用
   `order_view_handle.go` 这类名字才不需要新增豁免。
5. **未使用 import 会直接编译失败**（Go 语义），不是"删了会断"——删 `adminui` import
   必须与该文件最后一个 `adminui.` 调用同时消失。
6. **`pkg/utils` 已有`NormalizePaging` 饱和写法**：新增 `PageOffset` 复用它同一口径，
   不新造第二套夹页语义。

## 3 暂缓与待办

- **B2（dto 展示成品串 → 前端派生）暂缓**：`order_customer_cohort_resp.go` 的
  `MonthLabel/RetentionLabel/CohortLabel`、`order_customer_growth_resp.go` 的
  `RepurchaseRateLabel`、`order_customer_rfm_resp.go` 的 `MonetaryLabel` 等 40+ 字段
  同时被 JSON 出口（MCP / 访客 API）与页面消费，改成原始值需同步改前端 JS 与契约。
  模板侧已有 `money()` / `percent()`（`internal/templates/funcs.go:122`）可直接承接，
  但契约变更要单独一批并与前端协同。
- **B4（错误与文案域）待办**：`enums/order_enums.go` 157 处 `Err*` 未按声明处收敛；
  `service` 内界面 key 字面量 0 处（画像已核）。分批做，按规范 §6 的 seed 迁移规则。
- **DEC 待决 4 项**（见任务书 §5）：金额展示精度、访客订单分页上限、RFM/cohort 窗口口径是否与
  列表共用过滤常量、`order_attr_resp.go:89` 的 `Title` 判定。均未擅自决定。
- **`scripts/check-write-fields.sh`** 对 `order/order_ship_form.html` 的 `lines` 字段扫描
  仍红：main 基线自带（`019357d1` 与 `4a6b079c` 同样红），本分支未改。

## 4 验证命令与结果

```
go build ./...                                                    exit 0
go test ./pkg/utils/ ./internal/module/order/... ./internal/architecture/ ./public/test/order/...   exit 0
bash scripts/check-all.sh                                         29/30（唯一红为上述 main 基线项）
```

日志：`/tmp/order-b02a-gates.log`、`/tmp/order-b03a-gates.log`、`/tmp/order-b11-gates.log`、
`/tmp/order-b31-gates.log`、`/tmp/order-b31-tests.log`。
