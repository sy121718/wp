package inventoryservice

// 死线（spec §库存）：一切影响可用量的判断只能读 inventory_stocks 这一张真源表，
// 绝不读 product_variants.stock_total —— 那是后台列表展示用的冗余缓存，
// 一旦被当成扣减依据，缓存滞后就会直接变成超卖。本文件的读取路径全部走真源。
//
// 「按 SKU 增减 + 写流水 + 行锁」是 issue #16 的内容；本票的写入路径只有
// 「确保库存记录存在（初始 0）」这一条，因此 quantity 恒为 0，不做任何数值变更。

// 口径（docs/14 §1.1 与迁移 262，2026-09-19 冻结）：
//
//	· **仓库里的 SKU 永远是裸码**（DRAWERSMOKE_001），不带仓码前缀；
//	  仓码前缀只出现在**商品侧**（SZ_DRAWERSMOKE_001，标注归属 / 认领仓）。
//
// 为什么入库侧必须自己剥一次前缀，而不是继续指望「商品侧负责剥前缀」：
//
//	商品侧那条路径（product_crud / product_variant → EnsureVariantStock）剥的是
//	**商品自己那条 SKU**，它天然带着自己的认领仓前缀，两者恒对齐。入库不是那条路径：
//
//	  · 页面表单是操作者从候选里手选的编码；
//	  · 接口调用方（外部系统 / 脚本 / 运维）给的通常是商品侧的带前缀编码 —— docs/14
//	    只规定了「商品侧建库存行时剥前缀」，而入库建库存行根本不经商品侧；
//	  · 结果是**新建**的库存行可能落成空串或带前缀的编码，直接把不变量捅破：
//	    空串还会在 UNIQUE (warehouse_id, sku_code)（迁移 244）上撞成一句没有上下文的 23505。
//
// 所以本文件是入库入口的唯一规则，**空串一律拒绝**（绝不再用空串建库存行）。
// 归一实现与商品侧的 productservice.stripWarehousePrefix 逐字同口径（幂等 / 大小写不敏感 /
// 只剥一次），也与迁移 262 的存量清理谓词一致 —— 三处必须同时改，
// public/test/inventory/feature 里有一条把两边钉在一起的等价性断言。
//
// 为什么在同模块里再写一份而不直接调商品侧那个函数：本模块对商品模块的依赖**只允许经
// product 契约**（AGENTS.md 的表隔离约定），而 stripWarehousePrefix / StripWarehousePrefix
// 都不在契约上（它是商品的 service 层导出，给商品自己的 inbound 用的）。为这一处归一去
// 扩商品契约，会让「仓码前缀怎么剥」这条库存域规则挂到商品模块的对外接口上。

// 商品模块建变体时需要两件本模块才知道的事：
//  1. 归属仓的短码 —— SKU 编码形如 {仓短码}_{商品码}_{序号}，未指定仓时用默认仓；
//  2. 在归属仓生成一条初始 0 的库存记录。
//
// 商品模块只依赖本端口（端口定义在 product/contract），实现留在这里 ——
// 商品模块不认识仓库模块，装配期由顶层把本服务注入。

// 用户 2026-09-19 补充确认的三条口径，本文件是它们的唯一落点：
//
//  1. **属性属于商品**，仓库侧只回答「这条货在这个仓叫什么」—— 那件事落在
//     inventory_stocks.external_sku 上（第三方仓 / 平台仓的编码我们改不了，只能映射）；
//  2. 映射是 **N:1**：同一个商品的多个变体（十几个口味）在仓库侧可以共用同一个外码。
//     因此 DDL 上**没有**唯一索引，只有一条弱校验 —— 同一仓内同一外码必须指向
//     同一个 product_id（多口味共用合法，两个不同商品共用一个外码报
//     ErrExternalSKUProductConflict）；
//  3. 空串不是「缺失」而是一种合法状态：该仓用我们自己的 SKU（自营仓的常态）。
//
// 「仓库 SKU」不另建目录（docs/14 §4）：它就是库存真源上已有的 (warehouse_id, sku_code)。
// 本文件提供商品侧「从仓库选」需要的三件事：列出候选、按仓库 + 编码定位、写外码。

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"go_wp/internal/module/inventory/contract"
	"go_wp/internal/module/inventory/dto"
	"go_wp/internal/module/inventory/enums"
	"go_wp/internal/module/inventory/model"
	"go_wp/internal/module/product/contract"
	"go_wp/pkg/i18n"
	"go_wp/pkg/utils"
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

// skuSeparator 仓码与前缀后编码之间的分隔符（与商品侧同一个字面量）。
const skuSeparator = "_"

// stripWarehousePrefix 剥掉仓码前缀 —— 商品侧 attachWarehousePrefix 的逆操作。
//
// 三条性质（与商品侧逐条对称）：
//
//	· **幂等** —— 本来不带前缀时原样返回，剥两次与剥一次相同；
//	· **大小写不敏感** —— 前缀按大写比对（仓短码在工程内已归一为大写，历史数据不一定）；
//	· **只剥一次** —— SZ_SZ_X 剥成 SZ_X（不会一路剥到 X）。
//
// 仓码为空（未选仓 / 端口未注入）时原样返回：没有前缀就无所谓剥离。
// 返回值可能为空串（调用方给的就是「SZ_」这种只有前缀的编码）—— 空值判定留给
// normalizeStockSKU，阈值口径不在本函数里再散一份。
func stripWarehousePrefix(code, warehouseCode string) string {
	trimmed := strings.TrimSpace(code)
	prefix := strings.ToUpper(strings.TrimSpace(warehouseCode))
	if prefix == "" {
		return trimmed
	}
	if strings.HasPrefix(strings.ToUpper(trimmed), prefix+skuSeparator) {
		return trimmed[len(prefix)+len(skuSeparator):]
	}
	return trimmed
}

// normalizeStockSKU 把调用方给的 SKU 编码归一成**仓库侧裸码**，空串一律拒绝。
//
// 选这条口径（而不是「拒绝带前缀」）的理由：
//
//	· 入库是本模块接收 SKU 编码的**唯一入口**，编码来源不可控（页面手选 / 外部系统 /
//	  运维脚本），而带前缀是它们的常态 —— 一律拒绝等于把「调用方没按我们的内部表示传参」
//	  判成业务错误，运营拿到的会是「编码不合法」而不是货收进来了；
//	· 幂等剥前缀是纯粹的口径**归一**（不改变编码语义），与商品侧建行的做法完全一致：
//	  同一条编码在两边得到同一结果，不会出现「同一个 SKU 在商品侧是裸码、在库存侧带前缀」；
//	· 归一之后仍为空（给了空串 / 只给了「SZ_」）→ **明确拒绝**，不再用空串建库存行。
func normalizeStockSKU(code, warehouseCode string) (bare string, err error) {
	bare = stripWarehousePrefix(code, warehouseCode)
	if bare == "" {
		return "", errors.New(inventoryenums.ErrStockSKURequired)
	}
	return bare, nil
}

var _ inventorycontract.ProductStockPort = (*Service)(nil)

// NormalizeExternalSKU applies the inventory domain's canonical validation.
func (s *Service) NormalizeExternalSKU(raw string) (code string, err error) {
	return NormalizeExternalSKU(raw)
}

// ResolveWarehouse 解析归属仓（warehouseID 为空 → 该工程的默认仓），返回只读引用。
func (s *Service) ResolveWarehouse(ctx context.Context, projectID, warehouseID string) (ref *productcontract.WarehouseRef, err error) {
	wh, err := s.resolveWarehouse(ctx, projectID, warehouseID)
	if err != nil {
		return nil, err
	}
	return &productcontract.WarehouseRef{
		ID: wh.ID, ProjectID: wh.ProjectID, Code: wh.Code, Name: wh.Name,
	}, nil
}

// EnsureVariantStock 在归属仓为该 SKU 生成库存记录（已存在则复用）。
//
// 签名与 quantity 语义（2026-09-19 商品侧冻结，照此实现）：
//
//	· quantity == nil → 新建行 track_quantity = false（**不跟踪 = 无限**，新建行默认口径）；
//	· quantity != nil → 新建行 track_quantity = true 并写入该数量。0 是合法值
//	（= 明确没货），与 nil 严格区分 —— 这正是 CHECK (track_quantity OR quantity = 0)
//	要表达的事：不跟踪的行不允许带数字。
//
// skuCode 的语义是**仓库侧裸码**（如 DRAWERSMOKE_001）：仓库里的 SKU 永远不带仓码前缀，
// 前缀只出现在商品侧（SZ_DRAWERSMOKE_001，标注归属 / 认领仓）。**本层不剥前缀、也不加前缀**，
// 剥前缀是商品侧的职责；唯一例外是存量迁移 262 会把历史数据里多余的仓码前缀剥掉一次。
//
// 幂等：已存在的那一行原样返回，quantity / track_quantity / external_sku 都不被覆盖
// （覆盖式修改走 UpdateStockTracking / BindExternalSKU 这两条显式入口）。
func (s *Service) EnsureVariantStock(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (err error) {
	// 非 Tx 版本只负责**事务边界**：自己开一个事务并委托给 Tx 版本，
	// 两者语义逐字一致（不会各写一份判定）。
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.EnsureVariantStockTx(ctx, tx, ref, productID, variantID, skuCode, quantity)
	})
}

// EnsureVariantStockTx 在**调用方的事务**里为归属仓生成库存记录（幂等）。
//
// 为什么需要 Tx 变体（2026-09-19 商品域要求）：商品保存要把「商品 + 变体 + 各仓库存行
// + 变更记录」放进同一个事务，任何一步失败整体回滚。非 Tx 版本每次自己开事务，
// 商品回滚时库存行已经提交 —— 正是用户说的「很容易翻车」。
//
// 三条约束：
//   - 本方法**不再自己开事务**（调用方已经开着）；
//   - 工程作用域设在传入的 tx 上（rls.ScopeTx），**错误原样返回绝不吞掉**；
//   - 幂等建行走 ON CONFLICT (variant_id, warehouse_id) DO NOTHING：并发命中唯一键时
//     不报错，然后在同一事务内回读 —— 一句报错的 INSERT 会把整个事务标记为 aborted，
//     调用方后续的写入会全部失败。
//
// quantity / skuCode 的语义与非 Tx 版本完全一致（见 EnsureVariantStock）。
func (s *Service) EnsureVariantStockTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode string, quantity *int) (err error) {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return errors.New(inventoryenums.ErrStockWarehouseNeeded)
	}
	if strings.TrimSpace(variantID) == "" {
		return errors.New(inventoryenums.ErrStockVariantRequired)
	}
	_, err = s.ensureStockRowTx(ctx, tx, ref.ProjectID, ref.ID, productID, variantID, skuCode, quantity)
	return err
}

// EnsureVariantStockWithExternal 在归属仓为该 SKU 生成库存行，并写上该仓的外部编码（迁移 251）。
//
// 「创建商品即入库并带上外码」走这一条（docs/14 §9.3 的两条来路都只改映射，不动属性真源）。
// 三点语义：
//
//	· 外码只作用于**新建**那一行：已存在的库存行沿用既有值（覆盖式改外码走 BindExternalSKU
//	  这条显式入口）—— 免得「再 ensure 一次」把运营登记过的对方编码悄悄清掉；
//	· externalSKU 为空串是合法的：该仓用我们自己的 SKU（自营仓的常态）；
//	· 归一 / 校验共用一个入口（NormalizeExternalSKU），与库存侧的写入口径只有一份规则。
func (s *Service) EnsureVariantStockWithExternal(ctx context.Context, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (err error) {
	// 同 EnsureVariantStock：非 Tx 版本只负责开事务，判定与写入都在 Tx 版本里。
	return s.m.Transaction(ctx, func(tx *gorm.DB) error {
		return s.EnsureVariantStockWithExternalTx(ctx, tx, ref, productID, variantID, skuCode, externalSKU, quantity)
	})
}

// EnsureVariantStockWithExternalTx 在**调用方的事务**里建库存行并写上该仓的外部编码。
//
// 与非 Tx 版本的差别只有事务边界（见 EnsureVariantStockTx 的三条约束）；
// 外码的归一与校验（NormalizeExternalSKU）在这里先做，失败时不碰数据库。
func (s *Service) EnsureVariantStockWithExternalTx(ctx context.Context, tx *gorm.DB, ref *productcontract.WarehouseRef, productID, variantID, skuCode, externalSKU string, quantity *int) (err error) {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return errors.New(inventoryenums.ErrStockWarehouseNeeded)
	}
	if strings.TrimSpace(variantID) == "" {
		return errors.New(inventoryenums.ErrStockVariantRequired)
	}
	code, err := NormalizeExternalSKU(externalSKU)
	if err != nil {
		return err
	}
	_, err = s.ensureStockRowWithExternalTx(ctx, tx, ref.ProjectID, ref.ID, productID, variantID, skuCode, code, quantity)
	return err
}

// AvailableQuantities 批量读 SKU 的可用量（product 契约的 VariantAvailabilityPort，issue #20）。
//
// 读的是 inventory_stocks **真源**、跨仓求和，且与「缓存该被同步成什么值」用的是同一条
// 汇总口径（model.StockTotals）—— 套餐里显示能买几件，与商品侧缓存里写了几件，
// 不会出现两套算法各自算出不同数字。
//
// 无库存记录的变体不出现在返回值里（调用方按 0 兜底），filter 跨工程由 projectID 限定。
func (s *Service) AvailableQuantities(ctx context.Context, projectID string, variantIDs []string) (out map[string]int, err error) {
	out = make(map[string]int, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	rows, err := s.m.StockTotals(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.VariantID] = r.Total
	}
	return out, nil
}

const (
	// maxExternalSKULen 外部编码长度上限。
	//
	// 对方编码不受我们控制，但上限要挡住明显的脏写（整段 HTML / 备注粘进来）。
	// 128 足够覆盖各平台的实际编码长度。
	maxExternalSKULen = 128
	// 仓库 SKU 候选列表的分页（抽屉里的快捷入口，默认给一屏）。
	defaultWarehouseSKUSize = 50
	maxWarehouseSKUSize     = 200
)

// NormalizeExternalSKU 归一 + 校验外部 / 第三方编码：去首尾空白、长度上限、拒绝控制字符。
//
// 空串是**合法值**（= 该仓用我们自己的 SKU），不报错。归一规则只有这一份：
// 商品侧「创建即入库」与库存侧「绑定」都先过它，再把结果写库。
func NormalizeExternalSKU(raw string) (string, error) {
	code := strings.TrimSpace(raw)
	if code == "" {
		return "", nil
	}
	if utf8.RuneCountInString(code) > maxExternalSKULen {
		return "", errors.New(inventoryenums.ErrExternalSKUInvalid)
	}
	for _, r := range code {
		// 控制字符（含换行 / 制表 / DEL）：编码是给对方系统解析的标识，不是自由文本。
		if r < 0x20 || r == 0x7f {
			return "", errors.New(inventoryenums.ErrExternalSKUInvalid)
		}
	}
	return code, nil
}

// ListWarehouseSKUs 按工程 / 仓库列出可选的仓库 SKU（商品新建抽屉「从仓库选」的数据源）。
//
// 返回每行的 warehouse_id / sku_code / external_sku / has_variant（还要仓码与名称，
// 免得调用方为展示再查一次仓库）。关键字同时命中我们自己的编码与外部编码 ——
// 运营手上可能是其中任意一个。
func (s *Service) ListWarehouseSKUs(ctx context.Context, req *inventorydto.ListWarehouseSKUReq) (list []*inventorydto.WarehouseSKUResp, err error) {
	page, size := warehouseSKUPageArgs(req)
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, listWarehouseSKUProjectID(req)); err != nil {
		return nil, err
	}
	rows, err := s.m.ListWarehouseSKUs(ctx, inventorymodel.WarehouseSKUFilter{
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(listWarehouseSKUWarehouseID(req)),
		Keyword:     strings.TrimSpace(listWarehouseSKUKeyword(req)),
	}, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	list = make([]*inventorydto.WarehouseSKUResp, 0, len(rows))
	for _, r := range rows {
		list = append(list, toWarehouseSKUResp(r))
	}
	return list, nil
}

// GetWarehouseSKU 「从仓库选」的最小查询：给定仓库 + 我们自己那条仓库 SKU，返回该行。
//
// 不存在时明确报 ErrWarehouseSKUNotFound（不返回空行、也不静默当作「没选」）：
// 商品侧据此拒绝创建，运营拿到的是「换个仓库或先建这条货」这种可行动的提示。
func (s *Service) GetWarehouseSKU(ctx context.Context, req *inventorydto.GetWarehouseSKUReq) (res *inventorydto.WarehouseSKUResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" || strings.TrimSpace(req.SKUCode) == "" {
		return nil, errors.New(inventoryenums.ErrWarehouseSKURequired)
	}
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	e, err := s.lookupWarehouseSKU(ctx, projectID, strings.TrimSpace(req.WarehouseID), strings.TrimSpace(req.SKUCode))
	if err != nil {
		return nil, err
	}
	return s.toWarehouseSKURespFromEntity(ctx, e), nil
}

// BindExternalSKU 绑定 / 更新某 (仓库, 变体) 库存行的外部编码。
//
// 三条判定，顺序不能换：
//  1. 归一编码（空串合法 = 清空，该仓改回用我们自己的 SKU）；
//  2. 那一行必须真实存在 —— 否则 UPDATE 匹配 0 行却返回成功，「绑定成功」是假的；
//  3. N:1 弱校验：同一仓内同一外码必须指向同一个商品（多口味共用合法，跨商品报错）。
func (s *Service) BindExternalSKU(ctx context.Context, req *inventorydto.BindExternalSKUReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.WarehouseID) == "" || strings.TrimSpace(req.VariantID) == "" {
		return nil, errors.New(inventoryenums.ErrInvalidParam)
	}
	var projectID string
	if projectID, err = s.resolveProjectID(ctx, req.ProjectID); err != nil {
		return nil, err
	}
	warehouseID := strings.TrimSpace(req.WarehouseID)
	variantID := strings.TrimSpace(req.VariantID)
	code, err := NormalizeExternalSKU(req.ExternalSKU)
	if err != nil {
		return nil, err
	}
	row, err := s.m.GetStockByVariantWarehouse(ctx, variantID, warehouseID, projectID)
	if err != nil {
		return nil, mapStockNotFound(err)
	}
	if err = s.assertExternalSKUProductScope(ctx, projectID, warehouseID, code, row.ProductID); err != nil {
		return nil, err
	}
	affected, err := s.m.SetExternalSKUByVariantWarehouse(ctx, projectID, warehouseID, variantID, code)
	if err != nil {
		return nil, err
	}
	if affected == 0 {
		// 行在读取与写入之间被删（或工程作用域不一致）：不谎报成功。
		return nil, errors.New(inventoryenums.ErrStockNotFound)
	}
	row.ExternalSKU = code
	return s.toStockResp(ctx, row), nil
}

// CheckExternalSKUProductScope 商品侧「创建即入库」前的 N:1 弱校验（按目标商品判定）。
//
// 与 BindExternalSKU 内部那条校验是同一条规则（共用 assertExternalSKUProductScope）：
// 新商品的库存行还没建，所以由调用方把**将要成为归属**的 product_id 传进来 ——
// 该仓若已有别的商品占了同一个外码，这里就会拦住。
func (s *Service) CheckExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error) {
	code, err := NormalizeExternalSKU(externalSKU)
	if err != nil {
		return err
	}
	return s.assertExternalSKUProductScope(ctx, projectID, warehouseID, code, productID)
}

// CheckStockSKUCodeFree 仓内 sku_code 唯一预检（写库存行之前调用）。
//
// 判据与 DDL 上的 UNIQUE (warehouse_id, sku_code)（迁移 244）一致，只是提前到写入之前：
// 直接撞约束只会拿到一个没有上下文的 23505，运营看不到「哪个仓、哪条编码」。
func (s *Service) CheckStockSKUCodeFree(ctx context.Context, projectID, warehouseID, skuCode string) (err error) {
	exists, err := s.m.StockSKUCodeExists(ctx, projectID, warehouseID, strings.TrimSpace(skuCode), "")
	if err != nil {
		return err
	}
	if exists {
		return errors.New(inventoryenums.ErrWarehouseSKUCodeTaken)
	}
	return nil
}

// lookupWarehouseSKU 按 (仓库, 仓库 SKU) 定位那一行；不存在时给业务错误。
func (s *Service) lookupWarehouseSKU(ctx context.Context, projectID, warehouseID, skuCode string) (e *inventorymodel.StockEntity, err error) {
	e, err = s.m.FindStockByWarehouseSKU(ctx, projectID, warehouseID, skuCode)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New(inventoryenums.ErrWarehouseSKUNotFound)
		}
		return nil, err
	}
	return e, nil
}

// assertExternalSKUProductScope N:1 弱校验：同一个 external_sku 在同一个仓库内必须指向
// 同一个 product_id。空串（= 该仓用我们自己的 SKU）不参与校验。
//
// 错误里带上冲突的商品 id：只说「冲突了」等于让人去猜哪一条占了它。
func (s *Service) assertExternalSKUProductScope(ctx context.Context, projectID, warehouseID, externalSKU, productID string) (err error) {
	if externalSKU == "" {
		return nil
	}
	owner, err := s.m.ExternalSKUProductConflict(ctx, projectID, warehouseID, externalSKU, productID)
	if err != nil {
		return err
	}
	if owner != "" {
		return fmt.Errorf("%s：%s", inventoryenums.ErrExternalSKUProductConflict,
			i18n.ErrorDetail(inventoryenums.DetailExternalSKUOwner, "name", owner))
	}
	return nil
}

// toWarehouseSKUResp 投影行 → 响应。
func toWarehouseSKUResp(r *inventorymodel.WarehouseSKURow) *inventorydto.WarehouseSKUResp {
	return &inventorydto.WarehouseSKUResp{
		WarehouseID: r.WarehouseID, WarehouseCode: r.WarehouseCode, WarehouseName: r.WarehouseName,
		IsDefault: r.IsDefault, SKUCode: r.SKUCode, ExternalSKU: r.ExternalSKU,
		ProductID: r.ProductID, VariantID: r.VariantID, HasVariant: r.HasVariant,
	}
}

// toWarehouseSKURespFromEntity 库存实体 → 响应（补仓库展示信息；查不到仓库时留空，
// 与 toStockResp 同一兜底：展示字段读不到不影响主结果）。
func (s *Service) toWarehouseSKURespFromEntity(ctx context.Context, e *inventorymodel.StockEntity) *inventorydto.WarehouseSKUResp {
	if e == nil {
		return nil
	}
	resp := &inventorydto.WarehouseSKUResp{
		WarehouseID: e.WarehouseID, SKUCode: e.SKUCode, ExternalSKU: e.ExternalSKU,
		ProductID: e.ProductID, VariantID: e.VariantID, HasVariant: strings.TrimSpace(e.VariantID) != "",
	}
	if wh, werr := s.m.GetWarehouse(ctx, e.WarehouseID, e.ProjectID); werr == nil {
		resp.WarehouseCode, resp.WarehouseName, resp.IsDefault = wh.Code, wh.Name, wh.IsDefault
	}
	return resp
}

// warehouseSKUPageArgs 归一化仓库 SKU 列表的分页参数。
func warehouseSKUPageArgs(req *inventorydto.ListWarehouseSKUReq) (page, size int) {
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Size
	}
	paging := utils.NormalizePaging(inPage, inSize, defaultWarehouseSKUSize, maxWarehouseSKUSize)
	return paging.Page, paging.Size
}

// 下面三个小取值器只为「req 可能为 nil」这一件事存在，避免在方法体里散落三处判空。
// 名字带 listWarehouseSKU 前缀：inventory_source.go 已有一组同用途的
// projectIDOf / keywordOf（那个是货源列表的 req），同名会在包级直接撞车。
func listWarehouseSKUProjectID(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.ProjectID
}

func listWarehouseSKUWarehouseID(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.WarehouseID
}

func listWarehouseSKUKeyword(req *inventorydto.ListWarehouseSKUReq) string {
	if req == nil {
		return ""
	}
	return req.Keyword
}
