// inventory_stock.go — 库存记录读写（issue #15 验收 2/3/4）。
//
// 死线（spec §库存）：一切影响可用量的判断只能读 inventory_stocks 这一张真源表，
// 绝不读 product_variants.stock_total —— 那是后台列表展示用的冗余缓存，
// 一旦被当成扣减依据，缓存滞后就会直接变成超卖。本文件的读取路径全部走真源。
//
// 「按 SKU 增减 + 写流水 + 行锁」是 issue #16 的内容；本票的写入路径只有
// 「确保库存记录存在（初始 0）」这一条，因此 quantity 恒为 0，不做任何数值变更。
package inventoryservice

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
)

// EnsureStock 幂等地确保某 SKU 在某仓有一条库存记录（初始 0）。
func (s *Service) EnsureStock(ctx context.Context, req *inventorydto.EnsureStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.VariantID) == "" {
		return nil, errors.New(inventoryenums.ErrStockVariantRequired)
	}
	// quantity 传 nil：本条契约入口只说「确保这一行存在」这一步（新行 = 不跟踪 = 无限），
	// 要落一个具体数量走变动契约（ChangeStock）或商品侧的 EnsureVariantStock。
	e, err := s.ensureStockRow(ctx, req.ProjectID, req.WarehouseID,
		strings.TrimSpace(req.ProductID), strings.TrimSpace(req.VariantID), strings.TrimSpace(req.SKUCode), nil)
	if err != nil {
		return nil, err
	}
	return s.toStockResp(ctx, e), nil
}

// ensureStockRow 幂等生成库存行的内部实现（契约入口与商品端口共用同一条路径）。
//
// quantity 语义（迁移 261，2026-09-19 冻结）：
//
//	nil     → 新建行 track_quantity = false（**不跟踪 = 无限**，新建行的默认口径）；
//	非 nil  → 新建行 track_quantity = true 并写入该数量。
//	          0 是**合法且明确**的值（= 没货），与 nil 严格区分 —— 「没填数量」与
//	          「填了 0」是两回事，这也是 DDL 侧 CHECK (track_quantity OR quantity = 0)
//	          存在的意义：不跟踪的行不允许带数字。
//
// skuCode 是**仓库侧裸码**（如 DRAWERSMOKE_001）：仓库里的 SKU 永远不带仓码前缀，
// 前缀只出现在商品侧（SZ_DRAWERSMOKE_001，标注归属 / 认领仓）。**本层不剥前缀、
// 也不加前缀** —— 剥前缀是商品侧的职责（另一批），本层只把调用方给的编码原样写下。
// 唯一的例外是存量迁移 262：它会把**历史数据里**多余的仓码前缀剥掉一次，那是数据订正，
// 不是这条写路径的行为。
func (s *Service) ensureStockRow(ctx context.Context, projectID, warehouseID, productID, variantID, skuCode string, quantity *int) (e *inventorymodel.StockEntity, err error) {
	return s.ensureStockRowWithExternalTx(ctx, nil, projectID, warehouseID, productID, variantID, skuCode, "", quantity)
}

// ensureStockRowTx 与 ensureStockRow 同义，但在**调用方的事务**里执行（tx 非 nil）。
func (s *Service) ensureStockRowTx(ctx context.Context, tx *gorm.DB, projectID, warehouseID, productID, variantID, skuCode string, quantity *int) (e *inventorymodel.StockEntity, err error) {
	return s.ensureStockRowWithExternalTx(ctx, tx, projectID, warehouseID, productID, variantID, skuCode, "", quantity)
}

// ensureStockRowWithExternal 同上，并带上该仓的外部编码（迁移 251）。
func (s *Service) ensureStockRowWithExternal(ctx context.Context, projectID, warehouseID, productID, variantID, skuCode, externalSKU string, quantity *int) (e *inventorymodel.StockEntity, err error) {
	return s.ensureStockRowWithExternalTx(ctx, nil, projectID, warehouseID, productID, variantID, skuCode, externalSKU, quantity)
}

// ensureStockRowWithExternalTx 同上，并接受**调用方的事务**（nil = 自己开事务）。
//
// externalSKU 与 quantity 都只作用于**新建**那一行：EnsureStock 幂等（已存在则返回既有行），
// 既有的 external_sku / track_quantity / quantity 不会被这里覆盖 —— 覆盖式改外码走
// BindExternalSKU、改跟踪开关与数量走 UpdateStockTracking 这两条显式入口，
// 免得「再 ensure 一次」把运营登记过的对方编码或数量悄悄清掉。
//
// tx 非 nil 时**不再自己开事务**：幂等建行（model.EnsureStockTx）在传入的 tx 上设
// 工程作用域（rls.ScopeTx），插入冲突走 ON CONFLICT DO NOTHING —— 它绝不报错，
// 因为一句报错的 INSERT 会把调用方的整个事务标记为 aborted（商品 + 变体一起回滚）。
// 归属仓解析（resolveWarehouse）是只读且自足事务的，不参与外层事务的原子性。
func (s *Service) ensureStockRowWithExternalTx(ctx context.Context, tx *gorm.DB, projectID, warehouseID, productID, variantID, skuCode, externalSKU string, quantity *int) (e *inventorymodel.StockEntity, err error) {
	wh, err := s.resolveWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := &inventorymodel.StockEntity{
		ID: uuid.NewString(), ProjectID: wh.ProjectID, WarehouseID: wh.ID,
		ProductID: productID, VariantID: variantID, SKUCode: skuCode,
		ExternalSKU: externalSKU,
		// 默认 = 不跟踪（无限）：新建行「不填数量就是无限」与列默认 false 是同一个口径。
		TrackQuantity: false,
		Quantity:      0, Metadata: json.RawMessage("{}"),
		CreatedAt: now, UpdatedAt: now,
	}
	if quantity != nil {
		if *quantity < 0 {
			return nil, errors.New(inventoryenums.ErrStockQuantityInvalid)
		}
		// 给了一个具体数量 ⇒ 这行要跟踪（0 也一样：0 是「明确没货」，不是「没填」）。
		row.TrackQuantity = true
		row.Quantity = *quantity
	}
	if tx != nil {
		// 调用方已在事务里：不再自己开事务，在其上设作用域并幂等建行（ON CONFLICT DO NOTHING）。
		return s.m.EnsureStockTx(ctx, tx, row)
	}
	return s.m.EnsureStock(ctx, row)
}

// GetStock 单条库存记录（按 id，或按 变体 × 仓库）。
func (s *Service) GetStock(ctx context.Context, req *inventorydto.GetStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	var e *inventorymodel.StockEntity
	if strings.TrimSpace(req.ID) != "" {
		if e, err = s.m.GetStock(ctx, req.ID, projectID); err != nil {
			return nil, mapStockNotFound(err)
		}
	} else if strings.TrimSpace(req.VariantID) != "" && strings.TrimSpace(req.WarehouseID) != "" {
		if e, err = s.m.GetStockByVariantWarehouse(ctx, req.VariantID, req.WarehouseID, projectID); err != nil {
			return nil, mapStockNotFound(err)
		}
	} else {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	return s.toStockResp(ctx, e), nil
}

// ListStocksBySKU 某 SKU 在各仓的库存（验收 3/4）。
//
// 返回的每一行都是 (SKU, 仓库) 一条真源记录：本期单仓时只有一行，
// 多仓时同一个 SKU 会出现多行 —— 这正是「库存以 SKU × 仓库 为维度」的读取口径。
func (s *Service) ListStocksBySKU(ctx context.Context, req *inventorydto.ListStockBySKUReq) (list []*inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.SKUCode) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.ListStockRows(ctx, inventorymodel.StockFilter{
		ProjectID: projectID, SKUCode: strings.TrimSpace(req.SKUCode),
	}, 0, 0)
	if err != nil {
		return nil, err
	}
	return toStockRespList(rows), nil
}

// ListStocks 库存记录列表（按工程 / 仓 / 商品 / 变体 / SKU 过滤 + 分页）。
func (s *Service) ListStocks(ctx context.Context, req *inventorydto.ListStockReq) (list []*inventorydto.StockResp, err error) {
	page, size := pageArgs(req)
	filter := inventorymodel.StockFilter{
		WarehouseID: strings.TrimSpace(req.WarehouseID),
		ProductID:   strings.TrimSpace(req.ProductID),
		VariantID:   strings.TrimSpace(req.VariantID),
		SKUCode:     strings.TrimSpace(req.SKUCode),
		ExternalSKU: strings.TrimSpace(req.ExternalSKU),
	}
	if filter.ProjectID, err = s.resolveProjectID(ctx, strings.TrimSpace(req.ProjectID)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListStockRows(ctx, filter, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	return toStockRespList(rows), nil
}

// toStockResp 库存实体 → 响应（补仓库展示信息，避免后台二次查询）。
func (s *Service) toStockResp(ctx context.Context, e *inventorymodel.StockEntity) *inventorydto.StockResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.StockResp{
		ID: e.ID, ProjectID: e.ProjectID, WarehouseID: e.WarehouseID,
		ProductID: e.ProductID, VariantID: e.VariantID, SKUCode: e.SKUCode,
		// 外部编码随库存行一起出：它回答的是「这条货在这个仓叫什么」，
		// 与成本一样是 (仓库, 变体) 维度的附加事实，后台不必再查一次。
		ExternalSKU: e.ExternalSKU,
		// 跟踪开关与数量**必须成对**出（迁移 261）：quantity = 0 有两义
		//（跟踪且卖光 / 不跟踪无限），漏了开关就会把无限显示成「没货」。
		TrackQuantity: e.TrackQuantity,
		Quantity:      e.Quantity,
		// 成本随库存行一起出（(仓库, SKU) 一个当前值）：后台不必再为「这条货按多少
		// 算成本」跑第二次查询，也保证「库存与成本同源」不会各查各的漂移。
		CostPrice: e.CostPrice,
		CreatedAt: e.CreatedAt.Format(time.RFC3339), UpdatedAt: e.UpdatedAt.Format(time.RFC3339),
	}
	if wh, err := s.m.GetWarehouse(ctx, e.WarehouseID, e.ProjectID); err == nil {
		resp.WarehouseCode, resp.WarehouseName = wh.Code, wh.Name
	}
	return resp
}

// toStockRespList 投影行 → 响应列表（join 已带仓库信息，无需逐行回查）。
func toStockRespList(rows []*inventorymodel.StockRow) []*inventorydto.StockResp {
	list := make([]*inventorydto.StockResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, &inventorydto.StockResp{
			ID: r.ID, ProjectID: r.ProjectID, WarehouseID: r.WarehouseID,
			WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName,
			ProductID: r.ProductID, VariantID: r.VariantID, SKUCode: r.SKUCode,
			// 外部编码同源（投影里带上 s.external_sku）：与单条 GetStock 给出同一个值。
			ExternalSKU: r.ExternalSKU,
			// 跟踪开关同源（投影里带上 s.track_quantity）：列表 / 按 SKU 各仓两条读路径
			// 与单条 GetStock 给出同一个值，不会出现「详情是无限、列表是 0」。
			TrackQuantity: r.TrackQuantity,
			Quantity:      r.Quantity,
			// 成本同源（投影里带上 s.cost_price）：列表 / 按 SKU 各仓两条读路径
			// 与单条 GetStock 给出同一个值，不会出现「详情有成本、列表没有」。
			CostPrice: r.CostPrice,
			CreatedAt: r.CreatedAt.Format(time.RFC3339), UpdatedAt: r.UpdatedAt.Format(time.RFC3339),
		})
	}
	return list
}

// WarehouseStocksByProducts 一次取回若干商品在**各仓**的库存行（分仓聚合契约）。
//
// **冻结签名（商品侧依赖，2026-09-19）**：入参 (ctx, projectID, productIDs)，
// 返回 []dto.ProductWarehouseStock，元素八项字段（ProductID / WarehouseID /
// WarehouseCode / WarehouseName / SKUCode / TrackQuantity / Quantity / CostPrice）
// 一个都不能少 —— 商品列表的「分仓库存」列直接吃这个形状。
//
// 一次查询覆盖全部商品的全部仓，不逐商品查（一页商品几十个 × 每个几个仓，
// 逐商品查就是几十次往返）。只做投影，不做业务判断：哪些仓要显示、无限怎么渲染
// （∞ / 空 / 跨仓求和时的取舍）留给调用方 —— 各调用方的分组口径不同，
// 服务端替它们选定一种就是替它们丢了另一种。
//
// TrackQuantity 必须随行出去：quantity = 0 有两义（跟踪且卖光 / 不跟踪无限），
// 只给数量会让「无限」在商品列表里显示成「没货」。
//
// projectID 用于工程作用域（审计 DB-009）：缺它时 inventory_stocks 的 FORCE 策略
// 会让整条查询**静默返回空集**（商品列表整齐地显示「所有商品都没有分仓库存」，
// 既不报错也没有日志），所以这里先经 resolveProjectID 解析、由 rls 在空工程时报错。
func (s *Service) WarehouseStocksByProducts(ctx context.Context, projectID string, productIDs []string) (out []inventorydto.ProductWarehouseStock, err error) {
	if len(productIDs) == 0 {
		return []inventorydto.ProductWarehouseStock{}, nil
	}
	projectID, err = s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	rows, err := s.m.WarehouseStocksByProducts(ctx, projectID, productIDs)
	if err != nil {
		return nil, err
	}
	out = make([]inventorydto.ProductWarehouseStock, 0, len(rows))
	for _, r := range rows {
		out = append(out, inventorydto.ProductWarehouseStock{
			ProductID:     r.ProductID,
			WarehouseID:   r.WarehouseID,
			WarehouseCode: r.WarehouseCode,
			WarehouseName: r.WarehouseName,
			SKUCode:       r.SKUCode,
			TrackQuantity: r.TrackQuantity,
			Quantity:      r.Quantity,
			CostPrice:     r.CostPrice,
		})
	}
	return out, nil
}

// UpdateStockTracking 库存页行内编辑：切换某 (仓库, 变体) 库存行的**跟踪开关**并写入数量。
//
// 三条语义（迁移 261 的口径）：
//
//  1. 切成跟踪 + 数量：数量经**变动契约**（adjust，原因 = 手工调整）写 —— 数量的任何
//     变化都要有流水，行内编辑不是例外；applyStockChanges 在同一事务里把该行切成跟踪。
//     数量与开关都没变时什么都不做（不写流水、不写库）。
//  2. 切成不跟踪（无限）：数量清零与关开关在**同一个变动事务**里完成（一次 UPDATE、
//     一条流水）。分两次写会留下「清了一半」的中间态，而「先关开关再清数量」还会被
//     CHECK (track_quantity OR quantity = 0) 直接拒绝（23514，没有上下文）。
//  3. 不跟踪的行不接受非 0 数量：表单在无限态把数量框禁用并留空，这里再拦一道
//     （直接构造请求绕过页面时给出可读的业务错误，而不是让 DDL 抛 23514）。
//
// 数量走变动契约而不是直接 UPDATE：库存真源的两条铁律（判定只读真源、
// 有变动必有流水）不能在「页面上的一个开关」上开口子。
func (s *Service) UpdateStockTracking(ctx context.Context, req *inventorydto.UpdateStockTrackingReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" || strings.TrimSpace(req.VariantID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	if req.Quantity < 0 {
		return nil, errors.New(inventoryenums.ErrStockQuantityInvalid)
	}
	if !req.TrackQuantity && req.Quantity != 0 {
		// 不跟踪（无限）不允许带数字：数量与开关是同一件事的两种表达。
		return nil, errors.New(inventoryenums.ErrStockUntrackedQuantity)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	warehouseID := strings.TrimSpace(req.WarehouseID)
	variantID := strings.TrimSpace(req.VariantID)
	row, err := s.m.GetStockByVariantWarehouse(ctx, variantID, warehouseID, projectID)
	if err != nil {
		return nil, mapStockNotFound(err)
	}

	// adjustTo 把该行数量调成目标绝对量，并（可选）在同一事务里写回目标跟踪开关 ——
	// 两者都经**变动契约**：加锁顺序、流水构造、成本写回全部沿用既有那一套。
	adjustTo := func(target int, trackOverride map[stockKey]bool) error {
		_, cerr := s.changeStockTracking(ctx, &inventorydto.ChangeStockReq{
			ProjectID:   projectID,
			WarehouseID: warehouseID,
			Direction:   inventoryenums.DirectionAdjust,
			ReasonCode:  inventoryenums.ReasonManualAdjust,
			SourceType:  inventoryenums.MovementSourceInventoryPage,
			Remark:      "库存页行内编辑：跟踪开关 / 数量",
			Lines: []inventorydto.StockChangeLineReq{{
				WarehouseID: warehouseID,
				VariantID:   variantID,
				ProductID:   row.ProductID,
				SKUCode:     row.SKUCode,
				Quantity:    target,
			}},
		}, trackOverride)
		return cerr
	}

	if req.TrackQuantity {
		// 数量或开关有变化才走变动契约；两者都没变就是一次空转（不写库、不写流水）。
		if !row.TrackQuantity || row.Quantity != req.Quantity {
			if err = adjustTo(req.Quantity, nil); err != nil {
				return nil, err
			}
		}
		return s.GetStock(ctx, &inventorydto.GetStockReq{
			ProjectID: projectID, VariantID: variantID, WarehouseID: warehouseID,
		})
	}

	// 切成不跟踪（无限）：数量清零 + 关开关，**同一个事务、同一条流水**。
	// 行本来就无限且数量已经是 0 时才是空转。
	if row.Quantity != 0 || row.TrackQuantity {
		key := stockKey{variantID: variantID, warehouseID: warehouseID}
		if err = adjustTo(0, map[stockKey]bool{key: false}); err != nil {
			return nil, err
		}
	}
	return s.GetStock(ctx, &inventorydto.GetStockReq{
		ProjectID: projectID, VariantID: variantID, WarehouseID: warehouseID,
	})
}

// mapStockNotFound 行不存在 → 业务错误，其余原样透出。
func mapStockNotFound(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New(inventoryenums.ErrStockNotFound)
	}
	return err
}

// —— 归属仓成本解析（成本快照口径收口，docs/14 §9.3）——

// ResolveVariantWarehouseCosts 解析若干行的「归属仓 + 该 (仓库, SKU) 的当前成本」。
//
// 口径（2026-09-19 商品域评审已定，不自行改口径）：
//
//	· 归属仓 = 行上显式 WarehouseID；为空时按本模块既有的归属仓解析规则（默认仓）解析
//	  —— 与扣减（buildChangeItems → resolveWarehouse）**同一个入口**，两条路径不会漂移；
//	· 成本 = 解析出的那个仓里该变体库存行的 cost_price；**未核算（NULL）返回 nil**，
//	  绝不用 0 冒充（0 是合法的显式成本：赠品 / 内部划拨）；
//	· 该仓没有这条库存行同样是 nil（「没有这条货」= 没有成本），不是错误。
//
// 用途是订单行的成本快照（product/service 的 VariantSnapshots 直调本方法）：成本搬到
// (仓库, SKU) 之后，变体级 product_variants.cost_price 不再是这条链路的来源。
//
// 显式仓非法（不存在 / 跨工程 / 停用）时沿用 resolveWarehouse 的业务错误 ——
// 「指定的发货仓不可用」必须当场告诉调用方，不能退化成「成本未知」。
func (s *Service) ResolveVariantWarehouseCosts(ctx context.Context, projectID string, refs []inventorydto.VariantWarehouseCostRef) (out []inventorydto.VariantWarehouseCost, err error) {
	if len(refs) == 0 {
		return nil, nil
	}
	projectID, err = s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	variantIDs := make([]string, 0, len(refs))
	for _, r := range refs {
		if id := strings.TrimSpace(r.VariantID); id != "" {
			variantIDs = append(variantIDs, id)
		}
	}
	rows, err := s.m.ListStockCostsByVariants(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}
	// key 用「变体 + 分隔符 + 仓」：uuid 不含 '|'，不会与另一个组合撞键。
	cost := make(map[string]*float64, len(rows))
	for _, r := range rows {
		cost[r.VariantID+"|"+r.WarehouseID] = r.CostPrice
	}
	// 归属仓解析在**同一批内复用**：留空的 ref 一律落到同一个默认仓、显式仓相同的也共用
	// 一次解析结果 —— 否则十几行的订单要查十几次默认仓，而且并发下「默认仓刚好被切换」
	// 会让同一批的行落在不同的仓上（同一次成本快照必须是同一个仓的口径）。
	resolved := make(map[string]*inventorymodel.WarehouseEntity, len(refs))
	out = make([]inventorydto.VariantWarehouseCost, 0, len(refs))
	for _, r := range refs {
		variantID := strings.TrimSpace(r.VariantID)
		if variantID == "" {
			return nil, errors.New(inventoryenums.ErrStockVariantRequired)
		}
		key := strings.TrimSpace(r.WarehouseID)
		wh, ok := resolved[key]
		if !ok {
			var werr error
			wh, werr = s.resolveWarehouse(ctx, projectID, key)
			if werr != nil {
				return nil, werr
			}
			resolved[key] = wh
		}
		out = append(out, inventorydto.VariantWarehouseCost{
			VariantID:   variantID,
			WarehouseID: wh.ID,
			CostPrice:   cost[variantID+"|"+wh.ID],
		})
	}
	return out, nil
}

// VariantHasStockMovement 该变体是否**有过任何库存流水**（契约方法）。
//
// 用途：变体删除守卫（docs/14 §8.2 的保存守卫）—— 订单一旦建单就一定会产生扣减流水，
// 所以「有流水」等价于「这个变体被订单用过」；历史单据（订单行 / 采购行 / 流水）
// 都按 variant_id 追溯，硬删会让追溯断链，调用方据此拒绝硬删。
//
// 与「非零库存」互补：卖出后补货清零的变体库存为 0 却仍被订单用过，单看库存守卫
// 会误放行 —— 两个守卫都要。本方法只做委派：表访问在 model 的具名方法里，
// 「有流水 ⇒ 不许硬删」这条业务判定留在调用方。
func (s *Service) VariantHasStockMovement(ctx context.Context, projectID, variantID string) (bool, error) {
	return s.m.VariantHasStockMovement(ctx, projectID, variantID)
}
