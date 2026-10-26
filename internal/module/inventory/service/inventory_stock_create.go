package inventoryservice

// inventory_stock_create.go — 给仓库建一条货行（迁移 624）。
//
// 库存的真源在仓库侧：(仓库, 裸码) 一行货，带数量、成本、外部编码。它**先于商品存在** ——
// 收货时常常还不知道这批货要挂到哪个商品变体上。商品侧的变体在自己的
// warehouse_skus（迁移 637）里**引用**这些行，反向不再由库存行指回变体。
//
// 所以建档不写 product / variant：那两列在库里是迁移遗留，业务上已经不用。

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	json "go_wp/pkg/jsonx"
)

// CreateStock 在某个仓建一条货行（仓库 + 裸码 + 数量 + 成本）。
//
// 三条口径：
//
//   - 编码是**仓库侧裸码**：走 normalizeStockSKU（去前缀 + 非空判定），
//     与采购行、商品侧共用同一份规则（脚本 check-inventory-sku-format.sh 巡检）；
//   - 同仓同码已有行 → ErrWarehouseSKUCodeTaken（244 的唯一索引只是最后一道兜底，
//     提前拦截才能给出可读的错误）；
//   - 填了数量就必须跟踪（迁移 261 的 CHECK）：数量 > 0 而 TrackQuantity = false
//     直接打回，不替调用方改判 —— 页面给了两个显式选项，猜错的方向是静默记错账；
//   - 成本可空（NULL = 尚未核算，0 是合法的显式成本）。
func (s *Service) CreateStock(ctx context.Context, req *inventorydto.CreateStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	if req.Quantity < 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
	}
	if req.Quantity > 0 && !req.TrackQuantity {
		// 数量与开关是同一件事的两种表达：给了数量却说不跟踪，是自相矛盾的输入。
		return nil, inventoryenums.Err(inventoryenums.ErrStockUntrackedQuantity)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	wh, err := s.resolveWarehouse(ctx, projectID, strings.TrimSpace(req.WarehouseID))
	if err != nil {
		return nil, err
	}
	skuCode, err := normalizeStockSKU(strings.TrimSpace(req.SKUCode), wh.Code)
	if err != nil {
		return nil, err
	}
	externalSKU, err := NormalizeExternalSKU(req.ExternalSKU)
	if err != nil {
		return nil, err
	}
	cost, err := normalizeExplicitCost(req.CostPrice)
	if err != nil {
		return nil, err
	}
	exists, err := s.m.StockSKUCodeExists(ctx, projectID, wh.ID, skuCode, "")
	if err != nil {
		return nil, err
	}
	if exists {
		return nil, inventoryenums.Err(inventoryenums.ErrWarehouseSKUCodeTaken)
	}
	now := time.Now().UTC()
	row := &inventorymodel.StockEntity{
		ID: uuid.NewString(), ProjectID: wh.ProjectID, WarehouseID: wh.ID,
		SKUCode: skuCode, ExternalSKU: externalSKU,
		TrackQuantity: req.TrackQuantity, Quantity: req.Quantity,
		CostPrice: cost, Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
	}
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.CreateStockRowTx(ctx, tx, row); cerr != nil {
			return cerr
		}
		res = s.toStockResp(ctx, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}
