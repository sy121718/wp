# 模块改造任务书 · order（全仓逐模块改造第一份）

> 方法：`public/docs/模块改造通用规范.md`（下称"规范"）。本文件只填 order 的事实、批次与断言。
> 分支 `refactor/order-taskbook`，工作树 `.worktrees/order-refactor`，基线 `4a6b079c`（main）。
> 编辑：唯一编辑代理 `product-editor`（只编辑）；验证：主代理（唯一）。

## 0 逐模块排队（依据：越界密度，非文件数）

| 序 | 模块 | 主要症状（画像） | 备注 |
|---|---|---|---|
| 1 | **order** | `PageResp` 57 命中、模块内渲染出口 48 处、手写 `Offset(` 14 处、service 大文件 | 本文件 |
| 2 | mail | `PageResp` 69、渲染出口 59、`gin.H` 15、Offset 10 | 与 order 同型 |
| 3 | page | `PageResp` 56、渲染出口 39 | 与发布管线交织，需独立工作树 |
| 4 | inventory | 渲染出口 25、Offset 4、`gin.H` 10 | 成本/库存口径有 DEC |
| 5 | user / workbench / ai | `gin.H` 7/14/29、渲染出口 29/28/55 | ai 的 `gin.H` 最多 |
| 6 | content / comment / analytics / masterdata / media | `gin.H` 9/1/3/7/0，渲染出口 58/4/13/14/6 | 体量小，可合并两模块一批 |
| 7 | navigation / block / project / presentation / publication | 渲染出口 40/23/27/0/0，无 `PageResp` | 前三个是"页面出口形状"，后两个已随发布恢复改完 |

order 之后的模块各自另写任务书，模板与本文件同构（§1 事实 → §3 层归属表 → §4 批次 → §5 DEC）。

## 1 order 现状证据（本轮实测）

| 维度 | 事实 |
|---|---|
| 规模 | 83 个非测试 go 文件；service 单文件最大 `order.go` 79.4K、`return.go` 50.5K、`order_overview.go` 26.5K、`coupon.go` 25.4K |
| 页面响应类型 | `dto` 内 57 处 `PageResp`/`ListResp` 命中，如 `order_list_page_resp.go`(9.8K)、`order_sales_page_resp.go`、`order_shipment_page_resp.go` |
| 展示成品串 | `order_customer_cohort_resp.go:11/16-17/29-30`（MonthLabel/RetentionLabel/CohortLabel）、`order_customer_growth_resp.go:24-26`、`order_customer_resp.go:26`、`order_customer_rfm_resp.go:14` |
| 模块内渲染出口 | 48 处 `pageview.`/`adminui.` 引用；`gin.H` 7 处 |
| 页面路由 | `order_page_router.go` 9 个模板常量（:36-38/:69/:72/:75/:78/:81/:84），`adminui.PageSlot`+`PageAuthz`（:97-98/:115-116） |
| 反射挂分页 | `adminui.AttachPaginationView`（`order_list_page.go:170-178`、`return_page.go:191-197`）；`order_list_page_resp.go:81` 的 `Pagination` 靠反射填 |
| 手写分页 | model 层 `Offset(` 14 处：`coupon_model.go:238/:406`、`order_model.go:429/:520`、`return_model.go:252`、`order_customer_segment_model.go:123`、`order_customer_rfm_model.go:207/:260`；service 另有 `utils.NormalizeLimitOffset`（`order_customer.go:184/:235/:317`）；MCP 另有 `clampOffset`（`inbound/mcp/order_tools.go:180`） |
| 错误键 | `enums/order_enums.go` 内 157 处 `Err*` |
| 内部错误外泄 | `adminui.PageInternalText`（`return_page.go:347`）把内部文案写进页面 |
| 测试 | `public/test/order/feature` 20+ 用例（`order_coupon_write_test.go` 34.0K、`order_coupon_json_write_chain_test.go` 19.3K、`order_concurrency_test.go` 13.4K、`order_error_leak_test.go` 13.1K） |

已知与 order 相关但**不在本任务范围**：`scripts/check-write-fields.sh` 对
`order/order_ship_form.html` 的 `lines` 字段扫描口径（main 基线红，另案）。

## 2 目标与非目标

**目标**：页面用现成数据对象 + 统一渲染器；JSON 用业务 DTO；dto 只留语言无关事实；
分页语义一套；内部错误不外泄。

**非目标**：不改订单金额/成本/优惠计算算法；不改数据库结构（除分页索引有 EXPLAIN 证据另议）；
不重构 service 之间的调用关系（除跨模块直表/直 dto）；不顺手改 mail/page 等其它模块。

## 3 层归属表（order · 四列固定形状）

| 改动域 | 层 | 本批允许 | 本批移除的越界职责 |
|---|---|---|---|
| 列表/详情读取 | model | 本模块表 SQL、批量读取、窄投影、唯一尾键 | 手写 `Offset(` 各自为政、整行读后只用两列 |
| 分页语义 | service + `pkg/pagination` | count 与 list 同过滤、饱和夹页、`id ASC` 尾键 | 三套页码语义（model Offset / NormalizeLimitOffset / clampOffset） |
| 页面响应对象 | dto | 业务事实字段（金额分、时间戳、枚举 key） | `*PageResp`、`Label` 成品串、反射挂 `Pagination` |
| 页面出口 | inbound/http | `binding.*` → 调 service → 成功 `view.RenderTemplate` / 失败 JSON | 模板常量散落 router、`adminui.PageSlot`/`PageAuthz`/`AttachPaginationView`/`PageInternalText` |
| 写响应 | inbound/http | JSON `{code,message,data}` | 页面 POST 响应、`HX-Redirect` 之外的后端导航 |
| 错误声明 | enums + service | 声明处 key+兜底+状态码；service 交事实 | service 拼句、界面 key 混进错误域 |
| 模板 | templates | 结构/文案/选中态/表单初值 | 金额求和、状态机第二份实现 |
| 测试 | `public/test/order/**` | 真实迁移库、断言等式、页面真实片段 | 依赖反射分页、断言 `*PageResp` 形状 |

## 4 批次（每批独立可验收，先删旧形状再建新形状）

### B0 页面出口收口（最小风险，先做）
- 9 个模板常量从 `order_page_router.go` 移入各自 handler 的常量或 handler 声明；router 只装渲染器。
- `adminui.PageSlot`/`PageAuthz` → 与商品同形：handler 内 `view.UseRenderer` + 成功渲染/失败 JSON。
- 删 `adminui.AttachPaginationView` 反射挂 `Pagination`：分页事实在 handler 组装成现成对象的一部分。
- 删 `adminui.PageInternalText`：内部错误只进日志与错误码。
- **断言**：`order_list_page.go` 三个 handler 的成功响应为 200 + 真实 HTML 片段、失败为
  `{code,message}` 且不含内部原文；`gin.H` 计数不增；`check-page-view-dto.sh` 与
  `controller_shape_test.go` 登记同步下调。

### B1 页面响应对象去 `*PageResp`
- `order_list_page_resp.go`/`order_sales_page_resp.go`/`order_shipment_page_resp.go` 等改成
  「现成数据对象」组合（列表项 + 分页 + 筛选初值各一个对象），handler 只组装不派生。
- **断言**：模板不再读 `Page`/`PageSize`/`Total` 的反射字段；JSON 出口形状不变（`order.*` MCP/API 兼容）。

### B2 dto 展示字段清零
- `Label`/`RateLabel`/`AmountLabel` 这类成品串从前端派生；dto 保留原始值与单位。
- **断言**：`order_customer_*_test.go` 断言原始值等式（不是文案）；文案断言移到模板测试或 JS 测试。

### B3 分页语义归一
- 14 处 model `Offset(` 收敛到 `pkg/pagination`：count/list 同过滤 + 唯一尾键 `id ASC` + 饱和夹页。
- 三套语义（model / `NormalizeLimitOffset` / `clampOffset`）合并为一条，MCP 复用同一函数。
- **断言**：越界页、MaxInt、空集、排序并列四类用例；SQL 预算（每列表页 SQL 条数）不增。

### B4 错误与文案域
- 157 处 `Err*` 按声明处收敛；service 内界面 key 字面量清零；明细键若需迁移按 seed 规则（复制旧值 + 保旧键 + 中英兜底）。
- **断言**：`order_error_leak_test.go` 全绿且断言「不含内部原文」；`text_domain_test.go` 无新增豁免。

## 5 待决口径（登记 `public/docs/order业务裁决-DEC.md`，实施中遇到就停在该点，其余照做）

1. **金额展示精度**：前端按分展示还是按元（影响 `*AmountLabel` 移除后的派生位置）。
2. **访客订单查询是否需要分页上限**（与商品 DEC-05 同类问题：无上限时深页代价）。
3. **客户 RFM/cohort 的窗口口径**是否与列表分页共用同一时间过滤常量（决定 B3 能否合并过滤下推）。
4. **`order_attr_resp.go:89` 的 `Title`** 判定为业务属性名（保留）；若前端已本地化则该判定要重评。

## 6 验证矩阵

| 阶段 | 命令 | 归属 |
|---|---|---|
| 每批 | `go build ./...` | 主代理 |
| 每批 | `go test ./internal/module/order/... ./public/test/order/...` | 主代理 |
| 每批 | `bash scripts/check-all.sh` | 主代理 |
| 每批 | `git diff --check`、`code-review-graph update` | 主代理 |
| 合 main 前 | `go test ./internal/... ./pkg/...`、`go test ./public/...` 各一次 | 主代理 |

## 7 禁止项

- 不改订单金额/成本/优惠算法与税费口径。
- 不为通过门禁加豁免、不删用例、不放宽断言、不手抄建表 SQL。
- 不在 order 之外顺手改其它模块（含 `check-write-fields.sh` 的 main 基线红）。
- 不引入第二份前端词表（文案只来自模板 t/tf 与 enums 兜底）。
- 编辑代理不跑测试/构建/门禁，不提交、不合并。

## 8 执行进度

| 批次 | 状态 | 提交 |
|---|---|---|
| B0-1 / B0-2a / B0-2b / B0-3a（页面出口 + 反射分页） | 已完成并验收 | c60cabae / 33b0b711 / 3e76e16a / d29e0833 |
| B1-1（页面视图搬出 dto） | 已完成并验收 | 2361535d |
| B3-1（分页 offset 口径收敛到 pkg/utils.PageOffset） | 已完成并验收 | 52e95cd8 |
| B4（错误与文案域） | **核查后结项**：59 个 Err* 常量与 errFallbacks 逐条对应（脚本核查 missing=0），service 内界面 key 字面量 0 处 | — |
| B2（dto 展示成品串） | 暂缓：40+ 字段同时被 JSON 出口（MCP/访客 API）消费，改形状须与前端同批 | — |
| DEC 4 项 | 待决，未擅自决定 | — |

全量验收：`go test ./internal/... ./pkg/...` 203 包 ok、`go test ./public/...` 66 包 ok（/tmp/order-final-int.log、/tmp/order-final-pub.log）。

## 9 派发方式

小步 brief，一次一批：给「批次号 + 允许的文件清单 + 断言要求 + 禁止项」，不含"顺便"。
每批完成后主代理跑验证矩阵，绿灯才派下一批；红则先在 main 复跑同一条用例做归属判断。
