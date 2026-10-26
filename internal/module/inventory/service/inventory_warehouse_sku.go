package inventoryservice

// inventory_warehouse_sku.go — 仓库商品 SKU 主档（迁移 642 / B3）。
//
// 主档的真源是 inventory_stocks 那九列（name / image_url / barcode / freight_price /
// cost_currency / cost_base_currency / cost_fx_rate / status / remark）。
// 「一条货」的身份仍是 (仓库, 仓库 SKU) —— 641 删掉了 (variant, 仓) 那条唯一键之后，
// 这两列是库里唯一还在回答「这是不是同一条货」的东西。
//
// 本文件的三个写动作都**不碰数量**：数量只能经变动契约改（有变动必有流水）。
// 建行时允许带初量，那是「第一次入库」—— service 会连带写一条 adjust 流水，
// 而不是静默给一个数字（见 CreateStock）。

import (
	"context"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	i18n "go_wp/pkg/i18n"
	"go_wp/pkg/money"
)

// 主档字段的长度上限。
//
// 为什么在这里限而不在 DDL 限：这些是**展示字段**（名称太长是排版问题，不是数据完整性问题），
// 而 DDL 的 varchar(n) 会给出一个没有上下文的 22001。给可读的业务错误更合适；
// 上限取「够用且不会撑破表格」的值。
const (
	maxStockNameLen     = 200
	maxStockBarcodeLen  = 64
	maxStockRemarkLen   = 500
	maxStockImageURLLen = 512
)

// UpdateStock 修改一条货行的主档（名称 / 图 / 条码 / 备注 / 状态 / 成本四件套）。
//
// 两条不可变的东西不在入参里，也不在这里补判：编码与所在仓（本行的身份）。
//
// 成本四件套的归一有几条硬口径（与采购入库的成本口径同源）：
//   - CostFXRate 为 nil 时**不推算**：计价币与基准币相同则把汇率置 nil（未固化无意义），
//     不同则保留原值不动 —— 静默按 1:1 折算会在报表里伪造一个汇率；
//   - 计价币为空时沿用它原值（编辑表单没提这一项 ≠ 要清空它）。
func (s *Service) UpdateStock(ctx context.Context, req *inventorydto.UpdateStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
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
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	// 外码的归一与 N:1 弱校验走既有那一套（BindExternalSKU 的同一条规则）：
	// 编辑主档时顺手改外码不能绕开「同仓同外码必须属于同一商品」。
	externalSKU, err := NormalizeExternalSKU(req.ExternalSKU)
	if err != nil {
		return nil, err
	}
	var out *inventorymodel.StockEntity
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		row, lerr := s.m.GetStockScopedTx(ctx, tx, req.ID, projectID)
		if lerr != nil {
			return mapStockNotFound(lerr)
		}
		if aerr := s.assertExternalSKUProductScope(ctx, projectID, row.WarehouseID, externalSKU,
			inventorymodel.PtrString(row.ProductID)); aerr != nil {
			return aerr
		}
		costCurrency := strings.TrimSpace(req.CostCurrency)
		if costCurrency == "" {
			costCurrency = row.CostCurrency
		}
		baseCurrency := strings.TrimSpace(req.CostBaseCurrency)
		if baseCurrency == "" {
			baseCurrency = costCurrency
		}
		if fx == nil && !strings.EqualFold(costCurrency, baseCurrency) {
			// 换了计价币却没给汇率：保留原汇率是错的（它对应另一个币对），
			// 按 1:1 猜更错。置 nil = 未固化，让报表如实显示「汇率未固化」。
			fx = nil
		} else if fx == nil && strings.EqualFold(costCurrency, baseCurrency) {
			fx = nil
		}
		patch := map[string]any{
			"name":               name,
			"image_url":          imageURL,
			"barcode":            barcode,
			"remark":             remark,
			"external_sku":       externalSKU,
			"cost_price":         cost,
			"freight_price":      freightPriceOr(row.FreightPrice, freight),
			"cost_currency":      costCurrency,
			"cost_base_currency": baseCurrency,
			"cost_fx_rate":       fx,
			"update_time":        time.Now().UTC(),
		}
		if status != "" {
			patch["status"] = status
		}
		if uerr := s.m.UpdateStockFieldsTx(ctx, tx, req.ID, projectID, patch); uerr != nil {
			return uerr
		}
		row2, gerr := s.m.GetStockScopedTx(ctx, tx, req.ID, projectID)
		if gerr != nil {
			return mapStockNotFound(gerr)
		}
		out = row2
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.toStockResp(ctx, out), nil
}

// UpdateStockStatus 货行启停（列表行内的快速开关）。
//
// 停用不等于删除：已建立的变体引用与历史流水都要照旧可读，只是「从仓库选」的候选
// 不再列出它。删除一条被引用 / 有流水的货行是另一件事（由删除守卫处理）。
func (s *Service) UpdateStockStatus(ctx context.Context, req *inventorydto.UpdateStockStatusReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.ID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	status := strings.TrimSpace(req.Status)
	if status != inventoryenums.StatusActive && status != inventoryenums.StatusDisabled {
		return nil, inventoryenums.Err(inventoryenums.ErrStockStatusInvalid)
	}
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	var out *inventorymodel.StockEntity
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if uerr := s.m.UpdateStockFieldsTx(ctx, tx, req.ID, projectID, map[string]any{
			"status":      status,
			"update_time": time.Now().UTC(),
		}); uerr != nil {
			return uerr
		}
		row, gerr := s.m.GetStockScopedTx(ctx, tx, req.ID, projectID)
		if gerr != nil {
			return mapStockNotFound(gerr)
		}
		out = row
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.toStockResp(ctx, out), nil
}

// CountWarehouseSKUsByFilter 台账页的行数（与 ListWarehouseSKUs 共用同一份筛选）。
func (s *Service) CountWarehouseSKUsByFilter(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (total int64, err error) {
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, listWarehouseSKUProjectID(req)); err != nil {
		return 0, err
	}
	return s.m.CountWarehouseSKUs(ctx, inventorymodel.WarehouseSKUFilter{
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(listWarehouseSKUWarehouseID(req)),
		Keyword:     strings.TrimSpace(listWarehouseSKUKeyword(req)),
		Status:      strings.TrimSpace(listWarehouseSKUStatus(req)),
	})
}

// normalizeFreight 运费归一：可空、非负。
//
// nil = 「这次没提交这一项」由调用方决定沿用原值（freightPriceOr）；
// 显式 0 是**合法的**（免运费），不能当没填处理。
func normalizeFreight(p *float64) (*float64, error) {
	if p == nil {
		return nil, nil
	}
	if *p < 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockFreightInvalid)
	}
	return p, nil
}

// normalizeFXRate 汇率归一：可空、必须 > 0。
//
// 0 与负数都不是合法汇率（DDL 的 ck_inventory_stocks_cost_fx_rate 只拦库里那一层，
// 这里提前给可读的错误）。
func normalizeFXRate(p *float64) (*float64, error) {
	if p == nil {
		return nil, nil
	}
	if *p <= 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockFXRateInvalid)
	}
	return p, nil
}

// freightPriceOr 没提交运费时沿用原值（编辑表单不重填运费不该把默认 70 清成 0）。
func freightPriceOr(current, next *float64) *float64 {
	if next == nil {
		return current
	}
	return next
}

// —— 台账页读模型（B3）——

// usageShownPerRow 台账页每行最多列几个「被哪些变体用着」。
//
// 一屏 50 行、一条货可能被几十个变体引用（糖果那种 4 口味 × 3 型号拼出来的组合），
// 全列会把表格撑成一面墙。超出的部分由 VariantCount 表达（「还有 N 个」）。
const usageShownPerRow = 3

// ListStockSKURows 台账页一屏：货行 → 展示行（金额 / 币对 / 数量 / 变体引用都已成型）。
//
// 为什么这一段在 service 而不是 handler：金额格式化、币对拼接、汇率「未固化」的判断
// 都是**同一条口径**（成本列 NULL 不等于 0），落在 handler 会让它与库存页各写一份。
// 词条（状态文案 / 未核算提示）仍归模板，所以这里只交事实与已成型的字符串。
func (s *Service) ListStockSKURows(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (rows []inventorydto.StockSKURowResp, err error) {
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, listWarehouseSKUProjectID(req)); err != nil {
		return nil, err
	}
	scoped := inventorydto.ListWarehouseSKUReq{
		ProjectID:   projectID,
		WarehouseID: listWarehouseSKUWarehouseID(req),
		Keyword:     listWarehouseSKUKeyword(req),
		Status:      listWarehouseSKUStatus(req),
		Page:        req.Page,
		Size:        req.Size,
	}
	list, err := s.ListWarehouseSKUs(ctx, &scoped)
	if err != nil {
		return nil, err
	}
	out := make([]inventorydto.StockSKURowResp, 0, len(list))
	if len(list) == 0 {
		return out, nil
	}
	stockIDs := make([]string, 0, len(list))
	for _, r := range list {
		if r != nil && r.ID != "" {
			stockIDs = append(stockIDs, r.ID)
		}
	}
	byStock, codes := s.usageOf(ctx, projectID, stockIDs)
	for _, r := range list {
		if r == nil {
			continue
		}
		row := inventorydto.StockSKURowResp{
			ID: r.ID, WarehouseID: r.WarehouseID,
			WarehouseName: r.WarehouseName, WarehouseCode: r.WarehouseCode, IsDefault: r.IsDefault,
			SKUCode: r.SKUCode, ExternalSKU: r.ExternalSKU, Barcode: r.Barcode,
			Name: r.Name, ImageURL: r.ImageURL,
			ReservedQuantity: r.ReservedQuantity, AvailableQuantity: r.AvailableQuantity,
			TrackQuantity: r.TrackQuantity,
			CostText:      moneyTextOr(r.CostPrice),
			FreightText:   moneyTextOr(r.FreightPrice),
			CurrencyText:  currencyPairText(r.CostCurrency, r.CostBaseCurrency),
			FXText:        fxTextOf(r.CostFXRate, r.CostCurrency, r.CostBaseCurrency),
			FXMissing:     isFXMissing(r.CostFXRate, r.CostCurrency, r.CostBaseCurrency),
			Status:        r.Status,
			Remark:        r.Remark,
			UpdatedAt:     r.UpdatedAt,
		}
		// 不跟踪的行数量恒 0（CHECK 保证），QuantityText 留空由模板渲染「∞ + 无限」词条：
		// 显示 0 会让「无限」看起来像「没货」。
		if r.TrackQuantity {
			row.QuantityText = strconv.Itoa(r.Quantity)
		}
		ids := byStock[r.ID]
		row.VariantCount = len(ids)
		for i, id := range ids {
			if i >= usageShownPerRow {
				break
			}
			row.Variants = append(row.Variants, inventorydto.StockVariantUsage{
				VariantID: id, SKUCode: codes[id],
			})
		}
		out = append(out, row)
	}
	return out, nil
}

// usageOf 批量拿「谁在引用这批货行」：一次反查 + 一次取编码，避免逐行往返。
//
// 失败一律降级成空（页面少一列，不整页中断）：引用关系是展示用的附加信息，
// 它读不到不该让运营看不到库存本身 —— 与「预警读不到就报错」不是一回事。
func (s *Service) usageOf(ctx context.Context, projectID string, stockIDs []string) (byStock map[string][]string, codes map[string]string) {
	byStock, codes = map[string][]string{}, map[string]string{}
	if s.refs == nil || projectID == "" || len(stockIDs) == 0 {
		return byStock, codes
	}
	byStock, err := s.refs.VariantIDsByWarehouseStocks(ctx, projectID, stockIDs)
	if err != nil {
		return map[string][]string{}, codes
	}
	variantIDs := make([]string, 0, len(byStock))
	for _, ids := range byStock {
		variantIDs = append(variantIDs, ids...)
	}
	if codeMap, cerr := s.refs.VariantSKUCodes(ctx, projectID, variantIDs); cerr == nil {
		codes = codeMap
	}
	return byStock, codes
}

// moneyTextOr 可空金额 → 展示文本（nil 显示成破折号，不是空串）。
//
// 「未核算」与「零成本」是两件事（迁移 244 的口径：NULL 才表示未核算），
// 破折号是这条口径在页面上的落点 —— 模板不需要知道 nil 与 0 的差别。
func moneyTextOr(v *float64) string { return money.FormatYuanOrDash(v) }

// currencyPairText 成本币种展示：两币相同时只给一个，不同时给「USD → CNY」。
//
// 相同时仍要显示币种：报表与对账都要知道这一列是哪种钱（空着会被读成「元」）。
func currencyPairText(currency, base string) string {
	currency = strings.TrimSpace(currency)
	base = strings.TrimSpace(base)
	if currency == "" {
		return ""
	}
	if base == "" || strings.EqualFold(currency, base) {
		return currency
	}
	return currency + " → " + base
}

// fxTextOf 汇率文本；**空串 = 未固化**（两币不同却没有汇率）。
//
// 刻意不写成 "1"：未固化的汇率按 1:1 补上，报表就凭空多出一个能算的折算金额，
// 而那个数字没有任何依据。空串由模板渲染「汇率未固化」提示。
func fxTextOf(rate *float64, currency, base string) string {
	if rate != nil && *rate > 0 {
		return strconv.FormatFloat(*rate, 'f', -1, 64)
	}
	// 同币种本来就不需要汇率，不算「未固化」；不同币种却没有汇率 = 未固化，同样给空串
	// （两种情形在页面上的区别由 FxMissing 那个布尔决定，见下方 isFXMissing）。
	return ""
}

// isFXMissing 两个币种不同却没有汇率 —— 页面要显示「汇率未固化」而不是空白。
func isFXMissing(rate *float64, currency, base string) bool {
	if rate != nil && *rate > 0 {
		return false
	}
	currency = strings.TrimSpace(currency)
	base = strings.TrimSpace(base)
	if currency == "" || base == "" {
		return false
	}
	return !strings.EqualFold(currency, base)
}
