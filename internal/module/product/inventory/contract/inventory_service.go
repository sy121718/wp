// Package inventorycontract inventory 模块对外契约（issue #15）。
package inventorycontract

import (
	"context"

	inventorydto "go_wp/internal/module/product/inventory/dto"
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
	GetStock(ctx context.Context, req *inventorydto.GetStockReq) (res *inventorydto.StockResp, err error)
	// ListStocksBySKU 某 SKU 在各仓的库存（验收 3/4：同一 SKU 可在多个仓各有一行）。
	ListStocksBySKU(ctx context.Context, req *inventorydto.ListStockBySKUReq) (list []*inventorydto.StockResp, err error)
	// ListStocks 库存记录列表（后台核对用，按工程 / 仓 / 商品 / 变体 / SKU 过滤 + 分页）。
	ListStocks(ctx context.Context, req *inventorydto.ListStockReq) (list []*inventorydto.StockResp, err error)

	// —— 库存变动与流水（issue #16）——
	// ChangeStock 按 SKU 增减库存（验收 1/2/3/4）：真源行锁内判定可用量，
	// 多行按 (变体, 仓库) 标识升序加锁（无死锁），每次变动写带原因与来源引用的流水。
	ChangeStock(ctx context.Context, req *inventorydto.ChangeStockReq) (res *inventorydto.StockChangeResp, err error)
	// DeductStock 按 SKU 扣减（验收 1/5）：不足即整体拒绝；ExpandBOM 为真时
	// 按物料清单展开成多个子项 SKU 一起扣减。
	DeductStock(ctx context.Context, req *inventorydto.DeductStockReq) (res *inventorydto.StockChangeResp, err error)
	// ListMovements 库存流水列表（方向 / 数量 / 原因 / 来源引用都可过滤）。
	ListMovements(ctx context.Context, req *inventorydto.ListMovementReq) (list []*inventorydto.MovementResp, err error)

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

	// RegisterReceipt 登记采购收货（验收 1/2/3/4）：按行累加已入库数量（原子递增、超收拒绝、
	// 幂等键防重放），随后经 ChangeStock 增加库存并写流水（原因 = 采购入库、来源 = 采购单），
	// 并以采购单价更新 SKU 成本价；采购单状态在这条链路上重算。
	RegisterReceipt(ctx context.Context, req *inventorydto.RegisterReceiptReq) (res *inventorydto.ReceiptResp, err error)
	// RegisterProductionInbound 自家工厂生产入库（验收 5）：无采购单、来源必须是内部货源、
	// 成本价手工填写；库存变动同样走 ChangeStock（原因 = 生产入库）。
	RegisterProductionInbound(ctx context.Context, req *inventorydto.ProductionInboundReq) (res *inventorydto.ReceiptResp, err error)
	// ListPurchaseHistory 某 SKU 的进货历史（验收 6）：入库单行 + 单价快照 + 来源 + 收货仓。
	ListPurchaseHistory(ctx context.Context, req *inventorydto.ListPurchaseHistoryReq) (list []*inventorydto.PurchaseHistoryResp, err error)

	// —— 商品侧缓存同步与对账（验收 6/7）——
	// SyncStockCache 显式同步（真源汇总 → 商品侧展示缓存，带时间戳）。
	SyncStockCache(ctx context.Context, req *inventorydto.SyncStockCacheReq) (res *inventorydto.SyncStockCacheResp, err error)
	// ReconcileStockCache 真源与缓存逐变体对账；Repair 为真时按真源修复。
	ReconcileStockCache(ctx context.Context, req *inventorydto.ReconcileStockCacheReq) (res *inventorydto.ReconcileStockCacheResp, err error)
}
