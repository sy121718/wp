package inventorycontract

import (
	"context"

	inventorydto "go_wp/internal/module/inventory/dto"
	productcontract "go_wp/internal/module/product/contract"
	"gorm.io/gorm"
)

// StockFact 一条货行的只读事实（641 起的寻址单位）。
//
// 为什么不是「按变体汇总好了再给」：一条货行可以被多个变体引用（同一仓同一批货
// 同时挂两个套装），所以「这个变体有多少」必须由持有引用关系的一方（商品侧）
// 自己按归属求和 —— 库存侧按变体求和只会漏掉或重复计算共享行。
type StockFact struct {
	StockID       string
	WarehouseID   string
	WarehouseCode string
	WarehouseName string
	SKUCode       string
	// Quantity / ReservedQuantity 跟踪行才有意义的两个数字（不跟踪行恒 0）。
	Quantity         int
	ReservedQuantity int
	// TrackQuantity false = 不跟踪（无限）：数量不构成约束。
	TrackQuantity bool
	// CostPrice 元；nil = 尚未核算（0 是合法的显式成本）。
	CostPrice *float64
	// Name / ImageURL 货行主档（迁移 642）：商品页的「仓库明细」要按名称认货、带缩略图，
	// 与库存台账页同一份主档字段。空串 = 这条货还没填过名称 / 图（不是错误）。
	Name     string
	ImageURL string
}

// ProductStockReader exposes only inventory facts used by product projections and deletion guards.
type ProductStockReader interface {
	// StockFactsByStockIDs 按货行 id 批量取事实（商品侧把变体引用的 id 清单交过来）。
	StockFactsByStockIDs(ctx context.Context, projectID string, stockIDs []string) (facts []*StockFact, err error)
	// VariantHasStockMovement 该变体是否有过任何库存流水（删变体的守卫）。
	VariantHasStockMovement(ctx context.Context, projectID, variantID string) (exists bool, err error)
}

// ProductStockLookup holds read-only warehouse and inventory facts for product use cases.
type ProductStockLookup interface {
	ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error)
	GetWarehouseSKU(ctx context.Context, req *inventorydto.GetWarehouseSKUReq) (res *inventorydto.WarehouseSKUResp, err error)
	// StockFactsByStockIDs 与 ProductStockReader 上同名方法同一个实现（这里再列一次是
	// 为了让「只要读货行」的调用方不必依赖整个 Reader）。
	StockFactsByStockIDs(ctx context.Context, projectID string, stockIDs []string) (facts []*StockFact, err error)
	// WarehouseStocksByProducts 一次取回若干商品在**各仓**的库存行（分仓聚合契约）。
	WarehouseStocksByProducts(ctx context.Context, projectID string, productIDs []string) (out []inventorydto.ProductWarehouseStock, err error)
	// ResolveVariantWarehouseCosts 解析若干行的「归属仓 + 该 (仓库, SKU) 的当前成本」。
	ResolveVariantWarehouseCosts(ctx context.Context, projectID string, refs []inventorydto.VariantWarehouseCostRef) (out []inventorydto.VariantWarehouseCost, err error)
	VariantHasStockMovement(ctx context.Context, projectID, variantID string) (exists bool, err error)
}

// ProductStockWriter holds only product-initiated inventory creation and its validation.
// Tx methods use the caller's transaction to preserve product and inventory atomicity.
type ProductStockWriter interface {
	// EnsureVariantStock / EnsureVariantStockTx / EnsureVariantStockWithExternalTx 都在
	// 「该仓已有同裸码的行」时**复用**那一条，并把它的 id 返回给调用方。
	//
	// 返回 stockID 的理由（迁移 637）：商品侧要把这条货行记进变体的 warehouse_skus，
	// 而它是新建还是复用只有库存侧知道。让调用方建完再按 (仓, 裸码) 反查一次，
	// 等于把「怎么定位一条货行」这件事复制一份到调用方 —— 两处口径迟早不一致。
	// WarehouseStockIDBySKU 按 (仓, 裸码) 取货行 id；不存在时返回空串与 nil 错。
	//
	// 商品侧「从仓库选」必须先问这一句：仓库里的货可以先于商品存在（先收货后建品），
	// 那时要**引用既有行**，而不是按 (变体, 仓) 幂等插入 —— 后者会撞 244 的
	// UNIQUE (warehouse_id, sku_code)（23505 冒到页面上就是内部错误）。
	WarehouseStockIDBySKU(ctx context.Context, projectID, warehouseID, skuCode string) (stockID string, err error)
	EnsureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (stockID string, err error)
	EnsureVariantStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (stockID string, err error)
	EnsureVariantStockWithExternalTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (stockID string, err error)
	NormalizeExternalSKU(raw string) (code string, err error)
	CheckExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error)
	// CalibrateVariantStocksTx 把一批货行的库存**校准到目标值**（B8 商品表单的仓库明细）。
	//
	// 口径是 adjust：Quantity 是目标绝对量，不是增量。不跟踪（无限）的行**不参与校准**
	// —— 调用方负责把它从 targets 里剔出去，这里对「不跟踪行 + 非 0 数量」原样报
	// ErrStockUntrackedQuantity（库存侧不会替调用方把无限改成有限）。
	//
	// 为什么单列这条窄口、而不是让商品侧自己拼 ChangeStockReq：方向 / 原因 / 来源类型
	// 是库存域的协议常量（adjust / manual_adjust / product_form），

	// 让每个调用方各写一遍就是让它们各自漂移；而 InventoryService 上的 ChangeStockTx
	// 宽到商品侧不该持有（那条口上还有 Reserve / Deduct / Transfer 二十来个能力）。
	//
	// tx 非 nil 时全部写入落在调用方事务里（商品与库存的数量校准同生共死）。
	CalibrateVariantStocksTx(ctx context.Context, tx *gorm.DB, projectID, productID, variantID string, targets []ProductStockTarget) (err error)
}

// ProductStockTarget 一条「按目标值校准」的库存目标（仓 × 仓库 SKU 维度）。
//
// StockID 与 SKUCode 二选一即可：StockID 是调用方从引用清单里读到的既有行，
// SKUCode 是裸码（不带仓码前缀）。两者都给时以本行在三处都对得上的那个为准 ——
// 库存侧按 (仓, 裸码) 定行，StockID 只用于调用方自己的回执与判重。
type ProductStockTarget struct {
	StockID     string
	WarehouseID string
	SKUCode     string
	Quantity    int
}

// ProductStockPort groups the two required capabilities supplied by one inventory service.
type ProductStockPort interface {
	ProductStockLookup
	ProductStockWriter
}
