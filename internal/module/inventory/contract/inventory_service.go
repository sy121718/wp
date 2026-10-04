// Package inventorycontract inventory 模块对外契约（issue #15）。
package inventorycontract

import (
	"context"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
)

// InventoryService 仓库与库存管理契约。
//
// 边界：本模块只管**库存真源**（inventory_stocks）、仓库实体（inventory_warehouses）
// 与围绕真源的变动能力（流水 / 变动原因字典 / 物料清单 / 缓存同步与对账）。
//
//	· 商品模块只保留 product_variants.stock_total 这个**列表展示用冗余缓存**；
//	· 一切影响可用量的判断（扣减、超卖校验、可售数量）只能读本模块真源并加行锁，
//	  绝不读那个缓存 —— 缓存滞后会直接变成超卖；
//	· 缓存只被「同步 / 对账」，同步发生在库存事务提交之后（跨模块写不进同一事务）；
//	· 采购 / 订单 / 客户各由自己的模块负责，本模块不反向依赖它们。
//
// 依赖方向：inventory → product（本模块实现 product 契约定义的变体库存端口，
// 由顶层装配注入商品模块），商品模块不认识仓库模块的实现。
type InventoryService interface {
	// —— 仓库（验收 1：可建仓库并设置一个默认仓）——
	// CreateWarehouse 建仓；工程内第一个仓自动成为默认仓（「必须有一个默认仓」），
	// 显式 IsDefault 则切换默认仓（同工程唯一）。
	CreateWarehouse(ctx context.Context, req *inventorydto.CreateWarehouseReq) (res *inventorydto.WarehouseResp, err error)
	// UpdateWarehouse 改仓（改名 / 改短码 / 排序 / 状态 / 切换默认仓）。
	// 默认仓不能停用，也不能取消默认（先指定另一个默认仓）。
	UpdateWarehouse(ctx context.Context, req *inventorydto.UpdateWarehouseReq) (res *inventorydto.WarehouseResp, err error)
	GetWarehouse(ctx context.Context, req *inventorydto.GetWarehouseReq) (res *inventorydto.WarehouseResp, err error)
	// ListWarehouses 某工程的仓库列表（默认仓在最前）。
	ListWarehouses(ctx context.Context, req *inventorydto.ListWarehouseReq) (list []*inventorydto.WarehouseResp, err error)
	// DeleteWarehouse 删仓：默认仓、仓内仍有非零库存时一律拒绝。
	DeleteWarehouse(ctx context.Context, req *inventorydto.DeleteWarehouseReq) (err error)

	// —— 库存记录（验收 2/3：SKU × 仓库 一行，新建变体自动生成初始 0 的记录）——
	// EnsureStock 幂等地确保某 SKU 在某仓有一条库存记录（初始 0）；
	// WarehouseID 为空时兜底到该工程的默认仓（未指定仓库时的兜底）。
	EnsureStock(ctx context.Context, req *inventorydto.EnsureStockReq) (res *inventorydto.StockResp, err error)
	// GetStock 单条库存记录（按 id，或按 变体 × 仓库）。
	//
	// 库存读模型自带**仓库侧成本**（StockResp.CostPrice，(仓库, SKU) 的当前值，
	// 迁移 244）：NULL = 尚未核算，0 是合法的显式成本 —— 后台读成本不必另开入口，
	// 也就不会出现「详情与列表各查一份、两份对不上」。
	GetStock(ctx context.Context, req *inventorydto.GetStockReq) (res *inventorydto.StockResp, err error)
	// ListStocksBySKU 某 SKU 在各仓的库存（验收 3/4：同一 SKU 可在多个仓各有一行）。
	// 每行各带**该仓**的成本 —— 多仓同 SKU 的成本本来就是各自独立的（(仓库, SKU) 维度）。
	ListStocksBySKU(ctx context.Context, req *inventorydto.ListStockBySKUReq) (list []*inventorydto.StockResp, err error)
	// ListStocks 库存记录列表（后台核对用，按工程 / 仓 / 商品 / 变体 / SKU / 外部编码过滤 + 分页）。
	ListStocks(ctx context.Context, req *inventorydto.ListStockReq) (list []*inventorydto.StockResp, err error)

	// —— 无限库存与分仓聚合（迁移 261，库存域第一批）——

	// WarehouseStocksByProducts 一次取回若干商品在**各仓**的库存行。
	//
	// **冻结签名（2026-09-19，商品侧依赖；字段名与类型不再变）**：
	//
	//	入参：ctx / projectID（工程作用域，必填）/ productIDs
	//	返回：[]inventorydto.ProductWarehouseStock，元素为
	//	      ProductID, WarehouseID, WarehouseCode, WarehouseName,
	//	      SKUCode, TrackQuantity, Quantity, CostPrice(*float64)
	//
	// 语义：一行 = 一个 (product_id, warehouse_id, sku_code)，**不做聚合** ——
	// 「跨仓求和」与「按归属仓取一条」各调用方口径不同，聚合留在调用方。
	// TrackQuantity 必须与 Quantity 一起读：quantity = 0 有两义（跟踪且卖光 /
	// 不跟踪无限），只看数量会把无限看成没货。一次查询覆盖全部商品的全部仓，
	// 调用方不得逐商品循环调用（那会把一次批量读放大成 N 次往返）。
	//
	// 只读、无副作用；缺工程时由 rls 直接报错，绝不退化成「不限工程」
	//（后者在非超级角色下是静默空集）。
	WarehouseStocksByProducts(ctx context.Context, projectID string, productIDs []string) (out []inventorydto.ProductWarehouseStock, err error)

	// UpdateStockTracking 库存页行内编辑：切换某 (仓库, 变体) 库存行的跟踪开关并写入数量。
	//
	// quantity 只走**变动契约**（adjust，原因 = 手工调整）：数量的任何变化都留下流水；
	// 不跟踪（无限）的行不允许带非 0 数量（ErrStockUntrackedQuantity）；
	// 切成不跟踪会先把数量清成 0 再关开关（顺序不能反，DDL 的 CHECK 不允许
	// 「不跟踪却带数字」）。数量与开关都没变时是空转，不写库也不写流水。
	UpdateStockTracking(ctx context.Context, req *inventorydto.UpdateStockTrackingReq) (res *inventorydto.StockResp, err error)

	// —— 仓库 SKU 与外部编码映射（迁移 251 / docs/14 §9.3）——
	//
	// 属性属于**商品**，仓库侧只回答「这条货在这个仓叫什么」。映射是 **N:1**
	//（同一商品的多个变体可共用同一个外码），所以唯一性是 service 弱校验而不是唯一索引：
	// 同一仓内同一外码必须指向同一个 product_id（多口味共用合法，跨商品报
	// ErrExternalSKUProductConflict）。
	//
	// ListWarehouseSKUs 按仓库列出可选的仓库 SKU（分页 + 关键字，命中我们的编码或外部编码）；
	// 新建商品抽屉的「从仓库选」用它列候选，但服务端**不信任**前端提交的编码 ——
	// 落库前一律经 GetWarehouseSKU 在该仓复核一遍。
	ListWarehouseSKUs(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (list []*inventorydto.WarehouseSKUResp, err error)
	// GetWarehouseSKU 「从仓库选」的最小查询：给定仓库 + 仓库 SKU → 那一行
	//（不存在即 ErrWarehouseSKUNotFound，不返回空行）。
	GetWarehouseSKU(ctx context.Context, req *inventorydto.GetWarehouseSKUReq) (res *inventorydto.WarehouseSKUResp, err error)
	// BindExternalSKU 绑定 / 更新某 (仓库, 变体) 库存行的外部编码（空串 = 清空，
	// 该仓改回用我们自己的 SKU；非空时做 N:1 弱校验）。
	BindExternalSKU(ctx context.Context, req *inventorydto.BindExternalSKUReq) (res *inventorydto.StockResp, err error)

	// —— 库存变动与流水（issue #16）——
	// ChangeStock 按 SKU 增减库存（验收 1/2/3/4）：真源行锁内判定可用量，
	// 多行按 (变体, 仓库) 标识升序加锁（无死锁），每次变动写带原因与来源引用的流水。
	ChangeStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error)
	// DeductStock 按 SKU 扣减（验收 1/5）：不足即整体拒绝；ExpandBOM 为真时
	// 按物料清单展开成多个子项 SKU 一起扣减。
	DeductStock(ctx context.Context, req *inventorydto.DeductStockReq) (res *inventorydto.StockChangeResp, err error)
	// ChangeStockTx / DeductStockTx —— **事务透传版**：与 ChangeStock / DeductStock 逐字
	// 同一条路径（同一套加锁顺序、同一份流水构造），但在**调用方的事务**里执行。
	//
	// 专供同库跨模块的调用方（订单建单扣减 / 取消归还库存 / 退货入库）把库存变动纳入
	// 自己那个事务：一次用户可感知的写操作里有多处持久化写入时必须同事务，任一步失败
	// 整体回滚 —— 而不是「先提交 A、再动库存、失败再补偿」（补偿只留给跨库 / 外部系统，
	// 见 AGENTS.md「写操作的事务与回滚」）。
	//
	// 两条硬约定：
	//   · Tx 版本**不自己开事务** —— tx 非 nil，由调用方负责提交 / 回滚；
	//   · 错误**原样返回**（不吞、不映射、不降级），调用方据此回滚整个事务。
	//
	// 非 Tx 版本自己开一个事务并委托给它们，行为不变。
	ChangeStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ChangeStockReq) (err error)
	DeductStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.DeductStockReq) (err error)
	// ListMovements 库存流水列表（方向 / 数量 / 原因 / 来源引用都可过滤）。
	ListMovements(ctx context.Context, req *inventorydto.ListMovementReq) (list []*inventorydto.MovementResp, err error)
	// CountMovements 流水总条数：与 ListMovements **逐字同一份过滤条件**
	//（同一个 model 过滤函数，见 inventorymodel.applyMovementFilter），
	// 供后台列表页的服务端分页算总页数。
	//
	// req 里的 Page / Size 对计数无意义（被忽略）：总数与「当前在第几页」无关，
	// 与 CountProducts 同一形状 —— 复用同一个请求类型，两处口径才不会分叉。
	CountMovements(ctx context.Context, req *inventorydto.ListMovementReq) (n int64, err error)

	// —— 变动原因字典（验收 4）——
	ListReasons(ctx context.Context, req *inventorydto.ListReasonReq) (list []*inventorydto.ReasonResp, err error)
	CreateReason(ctx context.Context, req *inventorydto.CreateReasonReq) (res *inventorydto.ReasonResp, err error)
	UpdateReason(ctx context.Context, req *inventorydto.UpdateReasonReq) (res *inventorydto.ReasonResp, err error)

	// —— 物料清单（验收 5）——
	// SetBOM 全量替换某个父 SKU 的清单（空 items 即清空）。
	SetBOM(ctx context.Context, req *inventorydto.SetBOMReq) (res *inventorydto.BOMResp, err error)
	GetBOM(ctx context.Context, req *inventorydto.GetBOMReq) (res *inventorydto.BOMResp, err error)

	// —— 货源（issue #17）——
	// 一张表承载**所有进货来源**：外部供应商、集团内关联公司、自家工厂用 type 区分，
	// related_party 标记关联交易（报表按「类型 × 关联方」取数），config 承载异构对接扩展信息。
	// CreateSource 新建货源（验收 1/2）：类型区分内外部、可标记关联方、可配对接扩展信息；
	// 内部货源恒为关联方，结算价只属于内部货源。
	CreateSource(ctx context.Context, req *inventorydto.CreateSourceReq) (res *inventorydto.SourceResp, err error)
	// UpdateSource 改货源（改名 / 改码 / 改类型 / 改关联方 / 改结算价 / 停启用 / 改对接配置）。
	UpdateSource(ctx context.Context, req *inventorydto.UpdateSourceReq) (res *inventorydto.SourceResp, err error)
	GetSource(ctx context.Context, req *inventorydto.GetSourceReq) (res *inventorydto.SourceResp, err error)
	// ListSources 货源列表（验收 4）：类型 / 关联方 / 状态 / 关键词都是可组合的筛选维度。
	ListSources(ctx context.Context, req *inventorydto.ListSourceReq) (list []*inventorydto.SourceResp, err error)
	// CountSources 货源总条数：与 ListSources 同一份过滤条件（含「默认只列启用中」这条
	// 由 IncludeDisabled 决定的默认档），供后台分页算总页数。
	CountSources(ctx context.Context, req *inventorydto.ListSourceReq) (n int64, err error)
	DeleteSource(ctx context.Context, req *inventorydto.DeleteSourceReq) (err error)
	// SourceSummary 按「类型 × 关联方」分组统计（验收 4「关联方标志可用于报表区分」的数据出口）。
	SourceSummary(ctx context.Context, req *inventorydto.SourceSummaryReq) (res *inventorydto.SourceSummaryResp, err error)

	// —— 采购单与入库（issue #18）——
	// 采购单：单头（来源 = #17 的货源 + 收货仓）+ 结构化行（SKU × 数量 × 单价），
	// 行的「已入库数量」可原子递增；采购单状态由「已入库数量 与 采购数量」推导。
	// CreatePurchaseOrder 新建采购单（验收 1）：单头 + 结构化行，来源必须是启用中的货源。
	CreatePurchaseOrder(ctx context.Context, req *inventorydto.CreatePurchaseOrderReq) (res *inventorydto.PurchaseOrderResp, err error)
	// UpdatePurchaseOrder 改单头（来源 / 收货仓 / 备注 / 预计到货），行全量替换只允许在未入库时。
	UpdatePurchaseOrder(ctx context.Context, req *inventorydto.UpdatePurchaseOrderReq) (res *inventorydto.PurchaseOrderResp, err error)
	GetPurchaseOrder(ctx context.Context, req *inventorydto.GetPurchaseOrderReq) (res *inventorydto.PurchaseOrderResp, err error)
	// ListPurchaseOrders 采购单列表（状态 / 货源 / 关键词可组合筛选）。
	ListPurchaseOrders(ctx context.Context, req *inventorydto.ListPurchaseOrderReq) (list []*inventorydto.PurchaseOrderResp, err error)
	// CountPurchaseOrders 采购单总张数：与 ListPurchaseOrders 同一份过滤条件，
	// 供后台分页算总页数。
	CountPurchaseOrders(ctx context.Context, req *inventorydto.ListPurchaseOrderReq) (n int64, err error)

	// RegisterReceipt 登记采购收货（验收 1/2/3/4）：按行累加已入库数量（原子递增、超收拒绝、
	// 幂等键防重放），随后经 ChangeStockTx 增加库存并写流水（原因 = 采购入库、来源 = 采购单），
	// 并把本次到货价（缺省沿用采购行单价）写为该 (仓库, SKU) 的**当前成本价**。
	//
	// **上述全部写入是同一个事务**：锁采购单头 → 行已入库数量原子递增 → 建入库单与入库行
	// → 库存变动（ChangeStockTx 透传句柄）→ 单据状态与批次号写回 → 采购单状态重算。
	// 任一步失败整体回滚 —— 不会留下「记了账没动库存」（跨模块的 DB 补偿已删）。
	// 唯一的事务外步骤是商品侧 product_variants.cost_price 的兼容回写（那是商品模块的写，
	// 端口没有 Tx 形态）：失败只记在入库单行上，并可由幂等重放补做（见 applyReceiptCosts）。
	RegisterReceipt(ctx context.Context, req *inventorydto.RegisterReceiptReq) (res *inventorydto.ReceiptResp, err error)
	// RegisterProductionInbound 自家工厂生产入库（验收 5）：无采购单、来源必须是内部货源、
	// 成本价手工填写；库存变动同样走 ChangeStock（原因 = 生产入库），
	// 手工成本作为**显式成本**落到 (仓库, SKU) 的当前值上（与采购收货同一条路径）。
	RegisterProductionInbound(ctx context.Context, req *inventorydto.ProductionInboundReq) (res *inventorydto.ReceiptResp, err error)
	// ListPurchaseHistory 某 SKU 的进货历史（验收 6）：入库单行 + 单价快照 + 来源 + 收货仓。
	ListPurchaseHistory(ctx context.Context, req *inventorydto.ListPurchaseHistoryReq) (list []*inventorydto.PurchaseHistoryResp, err error)

	// —— 商品侧缓存同步与对账（验收 6/7）——
	// SyncStockCache 显式同步（真源汇总 → 商品侧展示缓存，带时间戳）。
	// ReconcileStockCache 真源与缓存逐变体对账；Repair 为真时按真源修复。

	// —— 成本快照口径收口（2026-09-19 商品域评审，docs/14 §1.3 / §4.2 / §4.3 / §9.3）——
	//
	// ResolveVariantWarehouseCosts 解析若干行的「归属仓 + 该 (仓库, SKU) 的当前成本」：
	// 归属仓 = 行上显式 WarehouseID，为空按本模块既有的归属仓解析规则（默认仓）解析
	//（与扣减同一个入口）；成本未核算（NULL）时 CostPrice 为 nil —— 绝不用 0 冒充。
	// 订单行的成本快照经它取数（变体级 product_variants.cost_price 不再是这条链路的来源）。
	ResolveVariantWarehouseCosts(ctx context.Context, projectID string, refs []inventorydto.VariantWarehouseCostRef) (out []inventorydto.VariantWarehouseCost, err error)

	// VariantHasStockMovement 该变体是否**有过任何库存流水**（变体删除守卫，docs/14 §8.2）。
	//
	// 订单一旦建单就一定会产生扣减流水，所以「有流水」等价于「被订单用过」；
	// 历史单据按 variant_id 追溯，有流水即**不允许硬删**。与「非零库存」互补：
	// 卖出后补货清零的变体库存为 0 却仍被用过，两个守卫都要。
	// projectID 必填：流水表带工程策略，缺作用域时计数恒 0（守卫静默失效）。
	VariantHasStockMovement(ctx context.Context, projectID, variantID string) (exists bool, err error)
}

// === 跨模块形状：调用方要传进来、要收回去的类型 ===

// 跨模块调用方使用的**形状重导出**（与 order 契约同一手法）：调用方只依赖 contract，
// 不直接 import inventory/dto。
//
// 为什么是重导出而不是另造一组「契约自有入参类型」：本模块的 dto 与对外契约形状是同一件事
// （仓库 / 仓库 SKU 的查询入参与库存投影就是它对外的语义），form 标签只影响 HTTP 绑定，
// 不改变语义。另造一组形状意味着两份必须逐字段保持等价的定义 —— 那是把「一处改、调用方编译错」
// 换成「一处改、另一处静默分叉」：耦合没有减少，出错面反而变大。
//
// 边界：本模块内部（service / inbound）继续用 inventorydto 作为实现形状；重导出只服务于
// 跨模块调用方（当前是 product：商品列表的库存投影、新建商品的「从仓库选」、变体成本快照）。
type (
	// 商品列表的库存投影（一行一个仓，跨仓求和在调用方做）。
	ProductWarehouseStock = inventorydto.ProductWarehouseStock
	// 成本快照的一行入参：哪个变体、在哪个仓（空 = 按归属仓解析）。
	VariantWarehouseCostRef = inventorydto.VariantWarehouseCostRef
	// 「从仓库选」：候选项查询入参与该行的只读投影。
	ListWarehouseSKUReq = inventorydto.ListWarehouseSKUReq
	GetWarehouseSKUReq  = inventorydto.GetWarehouseSKUReq
	WarehouseSKUResp    = inventorydto.WarehouseSKUResp
	// 仓库列表（工程维度）。
	ListWarehouseReq = inventorydto.ListWarehouseReq
)
