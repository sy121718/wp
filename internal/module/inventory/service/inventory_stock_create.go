package inventoryservice

// inventory_stock_create.go — 给仓库建一条货行（迁移 624 / 642）。
//
// 库存的真源在仓库侧：(仓库, 裸码) 一行货，带数量、成本、主档与外部编码。它**先于商品存在** ——
// 收货时常常还不知道这批货要挂到哪个商品变体上。商品侧的变体在自己的
// warehouse_skus（迁移 637）里**引用**这些行，反向不再由库存行指回变体。
//
// 所以建档不写 product / variant：那两列在库里是迁移遗留，业务上已经不用。
//
// 642 之后这里还负责写主档（名称 / 图 / 条码 / 运费 / 币种三件套 / 状态 / 备注）：
// 「一条货」的展示属性挂在它自己身上，不在商品上 —— 同一款货在不同仓可以有不同的叫法与图。

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	i18n "go_wp/pkg/i18n"
	json "go_wp/pkg/jsonx"
)

// CreateStock 在某个仓建一条货行（仓库 + 裸码 + 数量 + 成本 + 主档）。
//
// 五条口径：
//
//   - 编码是**仓库侧裸码**：走 normalizeStockSKU（去前缀 + 非空判定），
//     与采购行、商品侧共用同一份规则（脚本 check-inventory-sku-format.sh 巡检）；
//   - 同仓同码已有行 → ErrWarehouseSKUCodeTaken（244 的唯一索引只是最后一道兜底，
//     提前拦截才能给出可读的错误）；
//   - 填了数量就必须跟踪（迁移 261 的 CHECK）：数量 > 0 而 TrackQuantity = false
//     直接打回，不替调用方改判 —— 页面给了两个显式选项，猜错的方向是静默记错账；
//   - 数量 > 0 时**不写流水**：641 之后货行可以没有变体归属（先收货后建品），而
//     inventory_stock_movements.variant_id 是 NOT NULL —— 建行那一刻写不出这一条。
//     「第一次入库」的留痕由调用方（采购收货 / 调拨签收 / 商品建档）各自经变动契约完成，
//     那几条路径手里都有变体；本入口只服务「仓库里先有一批货」这种无归属的建档。
//   - 成本 / 运费 / 汇率都可空（NULL = 尚未核算 / 用默认，0 是合法的显式值）。
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
	status := strings.TrimSpace(req.Status)
	if status != "" && status != inventoryenums.StatusActive && status != inventoryenums.StatusDisabled {
		return nil, inventoryenums.Err(inventoryenums.ErrStockStatusInvalid)
	}
	name := strings.TrimSpace(req.Name)
	if len([]rune(name)) > maxStockNameLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrStockNameTooLong,
			i18n.ErrorDetail(inventoryenums.DetailStockNameLimit, "max", strconv.Itoa(maxStockNameLen)))
	}
	barcode := strings.TrimSpace(req.Barcode)
	if len(barcode) > maxStockBarcodeLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrStockBarcodeTooLong,
			i18n.ErrorDetail(inventoryenums.DetailStockBarcodeLimit, "max", strconv.Itoa(maxStockBarcodeLen)))
	}
	remark := strings.TrimSpace(req.Remark)
	if len([]rune(remark)) > maxStockRemarkLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrStockRemarkTooLong,
			i18n.ErrorDetail(inventoryenums.DetailStockRemarkLimit, "max", strconv.Itoa(maxStockRemarkLen)))
	}
	imageURL := strings.TrimSpace(req.ImageURL)
	if len(imageURL) > maxStockImageURLLen {
		return nil, inventoryenums.ErrDetail(inventoryenums.ErrStockImageTooLong,
			i18n.ErrorDetail(inventoryenums.DetailStockImageLimit, "max", strconv.Itoa(maxStockImageURLLen)))
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
	if err = s.assertExternalSKUProductScope(ctx, projectID, wh.ID, externalSKU, ""); err != nil {
		return nil, err
	}
	cost, err := normalizeExplicitCost(req.CostPrice)
	if err != nil {
		return nil, err
	}
	freight, err := normalizeFreight(req.FreightPrice)
	if err != nil {
		return nil, err
	}
	fx, err := normalizeFXRate(req.CostFXRate)
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
	costCurrency, baseCurrency := normalizeCostCurrencies(req.CostCurrency, req.CostBaseCurrency)
	now := time.Now().UTC()
	row := &inventorymodel.StockEntity{
		ID: uuid.NewString(), ProjectID: wh.ProjectID, WarehouseID: wh.ID,
		SKUCode: skuCode, ExternalSKU: externalSKU,
		TrackQuantity: req.TrackQuantity, Quantity: req.Quantity,
		CostPrice: cost,
		// Name / ImageURL / Barcode / Remark 的空串就是空串（642 的列是 NOT NULL DEFAULT ''）。
		Name: name, ImageURL: imageURL, Barcode: barcode, Remark: remark,
		// 运费 / 币种 / 状态带 gorm default 标签：nil 与空串时 gorm 不写该列，
		// 让 DB 取默认（70 / CNY / active）—— 显式 0（免运费）与显式币种才真的写进去。
		FreightPrice: freight, CostFXRate: fx,
		CostCurrency: costCurrency, CostBaseCurrency: baseCurrency,
		Status:   statusOf(req.Status),
		Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
	}
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.CreateStockRowTx(ctx, tx, row); cerr != nil {
			return cerr
		}
		// 不在这里写流水：见函数头最后一条口径（movements.variant_id NOT NULL，
		// 而无归属货行建行时没有变体）。
		res = s.toStockResp(ctx, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// normalizeCostCurrencies 计价币 / 基准币的归一。
//
// 两条口径：① 空串一律落 CNY（DDL 的 CHECK 不允许空串）；② 没给基准币时基准币 = 计价币
// （同币种不需要换算，cost_fx_rate 留 nil 表示「无需汇率」而不是「汇率未固化」）。
func normalizeCostCurrencies(currency, base string) (out, baseOut string) {
	out = strings.ToUpper(strings.TrimSpace(currency))
	if out == "" {
		out = defaultCostCurrency
	}
	baseOut = strings.ToUpper(strings.TrimSpace(base))
	if baseOut == "" {
		baseOut = out
	}
	return out, baseOut
}

// statusOf 空串按启用（新建行默认可用）。
func statusOf(status string) string {
	if v := strings.TrimSpace(status); v != "" {
		return v
	}
	return inventoryenums.StatusActive
}

// defaultCostCurrency 未指定计价币时的落点（与迁移 642 的列默认值一致）。
const defaultCostCurrency = "CNY"
