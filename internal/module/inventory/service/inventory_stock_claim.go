package inventoryservice

// inventory_stock_claim.go — 仓库商品 SKU 主档化：认领 / 解除 / 建档 / 预占 / 释放（迁移 624）。
//
// 主线（本仓与 ERP 对齐的那条链）：
//
//	A 建档  仓库里先有货：一条编码、一个数量、一个成本，**可能还不知道是哪个商品**
//	        → inventory_stocks，variant_id IS NULL（本文件 CreateUnclaimedStock）
//	B 认领  运营把这条货认领到商品变体（或建商品时「从仓库选」直接认领）
//	        → variant_id / product_id 落值（本文件 ClaimStock / UnclaimStock）
//	C 预占  下单：按 (variant_id, warehouse_id) 找行，reserved_quantity += n
//	        → 本文件 ReserveStockTx（不写流水）
//	D 出库  发货：quantity -= n 且 reserved_quantity -= min(reserved, n)，写 sale_out 流水
//	        → inventory_change.go 的 deductStockTx（经 model.DeductStockOnShipTx）
//	E 释放  取消 / 退款 / 超时：只 reserved_quantity -= n，不写流水
//	        → 本文件 ReleaseStockTx
//
// 三量恒等式（照抄 ERP product_inventory_reserve.go 文件头）：
//
//	on_hand   = quantity
//	reserved  = reserved_quantity
//	available = quantity - reserved_quantity
//
// **预占 / 释放只改 reserved_quantity，绝不改 quantity、绝不写流水。**
// 流水只在真实出入库时写 —— 预占是可从 reserved_quantity 与「订单未发货行数量」
// 反算的派生量，单独写一张表只会与主档漂移。
//
// 加锁顺序与 applyStockChangesTx 一致：按 (variant_id, warehouse_id) 升序逐一
// `FOR UPDATE`。出入库与预占共享同一个全局顺序，等待链不可能成环。

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

// ClaimStock 认领一条未认领的存货行到商品变体（验收 4/5）。
//
// 五步，全在一个事务里：
//
//  1. 锁目标行（FOR UPDATE）—— 判定与写入之间不能有别的请求改动它；
//  2. 校验它确实未认领（variant_id IS NULL），已认领返回 ErrStockNotClaimed；
//  3. 目标 (variant_id, warehouse_id) 已有库存行 → ErrStockAlreadyClaimed（**不合并**）；
//  4. 写 product_id / variant_id；
//  5. 提交。
//
// 第 3 步的判据用 model.StockSKUCodeExists 的同族查询（按变体 + 仓）而不是
// 244 的 UNIQUE (warehouse_id, sku_code)：认领不会改编码，所以撞的必须是
// 「同一个变体在该仓已经有行」这件事 —— 那才是两份事实。
func (s *Service) ClaimStock(ctx context.Context, req *inventorydto.ClaimStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	stockID := strings.TrimSpace(req.StockID)
	productID := strings.TrimSpace(req.ProductID)
	variantID := strings.TrimSpace(req.VariantID)
	if stockID == "" || productID == "" || variantID == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		row, lerr := s.m.LockStockByIDTx(ctx, tx, projectID, stockID)
		if lerr != nil {
			return mapStockNotFound(lerr)
		}
		if inventorymodel.PtrString(row.VariantID) != "" {
			// 已经认领过了：认领是单向动作，不提供「改挂到另一个变体」——
			// 那会让「这条货属于谁」变成可反复改写的事实，流水却只记了原主。
			return inventoryenums.Err(inventoryenums.ErrStockNotClaimed)
		}
		occupied, oerr := s.m.StockVariantExistsTx(ctx, tx, projectID, row.WarehouseID, variantID)
		if oerr != nil {
			return oerr
		}
		if occupied {
			// 不合并、不加后缀：两行库存是两份事实。
			return inventoryenums.Err(inventoryenums.ErrStockAlreadyClaimed)
		}
		affected, cerr := s.m.ClaimStockTx(ctx, tx, stockID, productID, variantID, now)
		if cerr != nil {
			return cerr
		}
		if affected == 0 {
			// 判定与写入之间被别人抢先认领：不谎报成功。
			return inventoryenums.Err(inventoryenums.ErrStockNotClaimed)
		}
		// 回执用**已在锁内的这一行**回填，而不是再查一次：重查走 m.db（另一条连接），
		// 看不到本事务里刚写进去的值 —— 那样回执会给出「认领成功但 variantId 为空」，
		// 比不返回还糟（调用方会据此以为写坏了）。
		row.ProductID, row.VariantID = &productID, &variantID
		row.UpdatedAt = now
		res = s.toStockResp(ctx, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// ClaimStockTx 认领的**事务透传版**（迁移 624 + 商品侧「从仓库选」，迁移 628 收口）。
//
// 与 ClaimStock 逐字同一条判定（锁行 → 未认领 → 目标无冲突 → 写两列），
// 差额只在事务边界：**不自己开事务**，落在调用方的 tx 里。
//
// 为什么商品侧需要它：建商品时那条「该仓已有同裸码的存货行」必须**在同一次建商品事务内**
// 认领到新变体 —— 用自足版本的话，商品事务回滚了而认领已经提交，货就被挂到一个
// 不存在的变体上（外键还在的话会拦，但那是 23503 而不是「事务回滚」）。
func (s *Service) ClaimStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ClaimStockReq) error {
	if req == nil || tx == nil {
		return inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	stockID := strings.TrimSpace(req.StockID)
	productID := strings.TrimSpace(req.ProductID)
	variantID := strings.TrimSpace(req.VariantID)
	if stockID == "" || productID == "" || variantID == "" {
		return inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return err
	}
	row, lerr := s.m.LockStockByIDTx(ctx, tx, projectID, stockID)
	if lerr != nil {
		return mapStockNotFound(lerr)
	}
	if inventorymodel.PtrString(row.VariantID) != "" {
		return inventoryenums.Err(inventoryenums.ErrStockNotClaimed)
	}
	occupied, oerr := s.m.StockVariantExistsTx(ctx, tx, projectID, row.WarehouseID, variantID)
	if oerr != nil {
		return oerr
	}
	if occupied {
		// 目标 (变体, 仓) 已有库存行：不合并、不加后缀（两行库存是两份事实）。
		return inventoryenums.Err(inventoryenums.ErrStockAlreadyClaimed)
	}
	affected, cerr := s.m.ClaimStockTx(ctx, tx, stockID, productID, variantID, time.Now().UTC())
	if cerr != nil {
		return cerr
	}
	if affected == 0 {
		return inventoryenums.Err(inventoryenums.ErrStockNotClaimed)
	}
	return nil
}

// UnclaimStock 解除认领（验收 6）：仅当 quantity = 0 且 reserved_quantity = 0。
//
// 为什么要求清零：解除之后这一行不再属于任何商品变体，变体删除守卫
// （CountNonZeroStocksByVariant）看不见它 —— 于是「这个变体的库存」变成一条
// 谁也发现不了的假设。有货就解除，等于让货从商品库存里消失。
func (s *Service) UnclaimStock(ctx context.Context, req *inventorydto.UnclaimStockReq) (res *inventorydto.StockResp, err error) {
	if req == nil || strings.TrimSpace(req.StockID) == "" {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	stockID := strings.TrimSpace(req.StockID)
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		row, lerr := s.m.LockStockByIDTx(ctx, tx, projectID, stockID)
		if lerr != nil {
			return mapStockNotFound(lerr)
		}
		if inventorymodel.PtrString(row.VariantID) == "" {
			return inventoryenums.Err(inventoryenums.ErrStockNotClaimed)
		}
		if row.Quantity != 0 || row.ReservedQuantity != 0 {
			return inventoryenums.Err(inventoryenums.ErrStockHasQuantity)
		}
		affected, uerr := s.m.UnclaimStockTx(ctx, tx, stockID, now)
		if uerr != nil {
			return uerr
		}
		if affected == 0 {
			// 并发下判定与写入之间被入了货：守卫在 SQL 里也带了一份，这里如实报错。
			return inventoryenums.Err(inventoryenums.ErrStockHasQuantity)
		}
		// 同 ClaimStock：回执用锁内的行回填（重查走另一条连接，看不到本事务的写入）。
		row.ProductID, row.VariantID = nil, nil
		row.UpdatedAt = now
		res = s.toStockResp(ctx, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return res, nil
}

// CreateUnclaimedStock 人工建档一条未认领存货（仓库 + 裸码 + 数量 + 成本）。
//
// 未认领行不能只靠迁移存量产出来 —— 那样「先有货后建商品」这个能力就是白做的。
// 四条口径：
//
//   - 编码是**仓库侧裸码**：走 normalizeStockSKU（去前缀 + 非空判定），
//     与采购行、商品侧共用同一份规则（脚本 check-inventory-sku-format.sh 巡检）；
//   - 同仓同码已有行 → ErrWarehouseSKUCodeTaken（244 的唯一索引只是最后一道兜底，
//     提前拦截才能给出可读的错误）；
//   - 填了数量就必须跟踪（迁移 261 的 CHECK）：数量 > 0 而 TrackQuantity = false
//     直接打回，不替调用方改判 —— 页面给了两个显式选项，猜错的方向是静默记错账；
//   - 成本可空（NULL = 尚未核算，0 是合法的显式成本）。
func (s *Service) CreateUnclaimedStock(ctx context.Context, req *inventorydto.CreateUnclaimedStockReq) (res *inventorydto.StockResp, err error) {
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
		// 未认领：product_id / variant_id 都是 NULL。
		ProductID: nil, VariantID: nil,
		SKUCode: skuCode, ExternalSKU: externalSKU,
		TrackQuantity: req.TrackQuantity, Quantity: req.Quantity,
		CostPrice: cost, Metadata: json.RawMessage("{}"), CreatedAt: now, UpdatedAt: now,
	}
	err = s.m.Transaction(ctx, func(tx *gorm.DB) error {
		if cerr := s.m.CreateUnclaimedStockTx(ctx, tx, row); cerr != nil {
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

// ReserveStock 下单预占（自己开事务形态）。
func (s *Service) ReserveStock(ctx context.Context, req *inventorydto.ReserveStockReq) (res *inventorydto.StockReserveResp, err error) {
	out, err := s.reserveStockOn(ctx, nil, req, resolveReserveOp)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ReserveStockTx 下单预占的**事务透传版**（订单建单必须与自己那个事务同生共死）。
func (s *Service) ReserveStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ReserveStockReq) (err error) {
	_, err = s.reserveStockOn(ctx, tx, req, resolveReserveOp)
	return err
}

// ReleaseStock 释放预占（自己开事务形态）。
func (s *Service) ReleaseStock(ctx context.Context, req *inventorydto.ReserveStockReq) (res *inventorydto.StockReserveResp, err error) {
	return s.reserveStockOn(ctx, nil, req, resolveReleaseOp)
}

// ReleaseStockTx 释放预占的**事务透传版**（取消 / 退款必须与状态推进同事务）。
func (s *Service) ReleaseStockTx(ctx context.Context, tx *gorm.DB, req *inventorydto.ReserveStockReq) (err error) {
	_, err = s.reserveStockOn(ctx, tx, req, resolveReleaseOp)
	return err
}

// reserveOp 预占 / 释放的差异只有两处：delta 的符号与「不足」的错误码。
//
// 两条路径共用**同一份**加锁 / 守卫 / 回执组装 —— 各写一份的代价是「预占修了、
// 释放没修」，而这两处的判据（可售、预占下限）恰好是镜像关系，最容易漏的是其中一边。
type reserveOp struct {
	// sign +1 = 预占（占用可售），-1 = 释放（归还可售）。
	sign int
	// insufficient 守卫失败时对外报的错误（两个方向的语义不同）。
	insufficient string
}

var (
	resolveReserveOp = reserveOp{sign: +1, insufficient: inventoryenums.ErrStockInsufficient}
	resolveReleaseOp = reserveOp{sign: -1, insufficient: inventoryenums.ErrReserveUnderflow}
)

// reserveStockOn 预占 / 释放的事务实现（tx 为 nil 时自己开事务）。
//
// 六步，全在一个事务里：
//
//  1. 归一入参（行数上限与变动同口径：maxBatchLines）；
//  2. 逐行解析仓 —— 与 DeductStock 的 resolveWarehouse 同一入口（空 = 默认仓）；
//  3. 取目标行并按 (variant_id, warehouse_id) 升序 `FOR UPDATE`；
//  4. 跳过不跟踪（无限）行 —— 无限行的 quantity 恒 0，预占会违反
//     CHECK (reserved_quantity <= quantity)；
//  5. 逐行原子更新 reserved_quantity（守卫写在 SQL 的 WHERE 里）；
//  6. 任一行守卫失败 → 整体回滚（**整单拒绝，不做部分预占**）。
//
// **不写流水**：预占不是一次出入库。流水的 source_type / source_ref 也没有落点 ——
// 对账靠一个查询（reserved_quantity vs 订单未发货行数量），见任务书 §14 的升级路径。
func (s *Service) reserveStockOn(ctx context.Context, tx *gorm.DB,
	req *inventorydto.ReserveStockReq, op reserveOp) (out *inventorydto.StockReserveResp, err error) {
	if req == nil {
		return nil, inventoryenums.Err(inventoryenums.ErrInvalidParam)
	}
	if len(req.Lines) == 0 {
		return nil, inventoryenums.Err(inventoryenums.ErrStockLinesRequired)
	}
	if len(req.Lines) > maxBatchLines {
		return nil, inventoryenums.Err(inventoryenums.ErrStockLinesTooMany)
	}
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	targets, err := s.reserveTargets(ctx, projectID, req.Lines)
	if err != nil {
		return nil, err
	}
	apply := func(t *gorm.DB) error {
		res, aerr := s.applyReservations(ctx, t, projectID, targets, op)
		if aerr != nil {
			return aerr
		}
		out = res
		return nil
	}
	if tx != nil {
		return out, apply(tx)
	}
	if err = s.m.Transaction(ctx, apply); err != nil {
		return nil, err
	}
	return out, nil
}

// reserveTarget 一行预占目标：要锁哪一行、动多少。
type reserveTarget struct {
	stockID   string
	warehouse *inventorymodel.WarehouseEntity
	variantID string
	skuCode   string
	delta     int
}

// reserveTargets 解析每一行的仓并合并同一行的多行请求（同一 (变体, 仓) 出现多次时累加）。
//
// 合并的意义：加锁与守卫都按行做，同一行出现两次时若分两次 `reserved += n1`、`+= n2`，
// 第一次可能过、第二次才撞守卫 —— 中间态在事务里虽然会回滚，但对账日志里会留下
// 一条「预占了一半」的痕迹。合并之后一次判定、一次写入。
func (s *Service) reserveTargets(ctx context.Context, projectID string,
	lines []inventorydto.ReserveStockLine) (targets []*reserveTarget, err error) {
	byKey := make(map[stockKey]*reserveTarget, len(lines))
	order := make([]stockKey, 0, len(lines))
	for i := range lines {
		line := lines[i]
		variantID := strings.TrimSpace(line.VariantID)
		if variantID == "" {
			return nil, inventoryenums.Err(inventoryenums.ErrStockVariantRequired)
		}
		if line.Quantity <= 0 {
			return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
		}
		wh, werr := s.resolveWarehouse(ctx, projectID, strings.TrimSpace(line.WarehouseID))
		if werr != nil {
			return nil, werr
		}
		key := stockKey{variantID: variantID, warehouseID: wh.ID}
		if t, ok := byKey[key]; ok {
			t.delta += line.Quantity
			continue
		}
		byKey[key] = &reserveTarget{
			warehouse: wh, variantID: variantID,
			skuCode: strings.TrimSpace(line.SKUCode),
			delta:   line.Quantity,
		}
		order = append(order, key)
	}
	// 锁序固定：与 applyStockChangesTx 同一条升序（uniqueSortedKeys 就是干这个的）。
	sorted := make([]stockKey, len(order))
	copy(sorted, order)
	slices.SortFunc(sorted, func(a, b stockKey) int {
		return cmp.Or(cmp.Compare(a.variantID, b.variantID), cmp.Compare(a.warehouseID, b.warehouseID))
	})
	targets = make([]*reserveTarget, 0, len(sorted))
	for _, k := range sorted {
		targets = append(targets, byKey[k])
	}
	return targets, nil
}

// applyReservations 在事务内锁定目标行并逐行更新 reserved_quantity。
//
// 跳过不跟踪（无限）行：无限行的 quantity 恒 0（迁移 261 的 CHECK），
// 预占任何正数都会违反 CHECK (reserved_quantity <= quantity) —— 而「无限」的语义
// 就是数量不构成约束，所以根本不需要占用可售。
func (s *Service) applyReservations(ctx context.Context, tx *gorm.DB, projectID string,
	targets []*reserveTarget, op reserveOp) (out *inventorydto.StockReserveResp, err error) {
	now := time.Now().UTC()
	out = &inventorydto.StockReserveResp{
		ProjectID: projectID,
		Lines:     make([]*inventorydto.StockReserveLineResp, 0, len(targets)),
	}
	for _, t := range targets {
		variantID := t.variantID
		row, lerr := s.m.LockStockRowTx(ctx, tx, &variantID, t.warehouse.ID)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {
				// 该 (变体, 仓) 没有库存行：预占无从谈起。
				return nil, inventoryenums.Err(inventoryenums.ErrStockNotFound)
			}
			return nil, lerr
		}
		if !row.TrackQuantity {
			// 无限库存不参与预占（见方法注释）。
			continue
		}
		delta := op.sign * t.delta
		affected, uerr := s.m.UpdateStockReservedTx(ctx, tx, row.ID, delta, now)
		if uerr != nil {
			return nil, uerr
		}
		if affected == 0 {
			// 守卫失败：预占超可售（或释放超已预占）。**整单回滚**，不做部分成功。
			return nil, inventoryenums.Err(op.insufficient)
		}
		remaining := row.ReservedQuantity + delta
		out.Lines = append(out.Lines, &inventorydto.StockReserveLineResp{
			StockID: row.ID, WarehouseID: row.WarehouseID,
			WarehouseCode:     t.warehouse.Code,
			VariantID:         variantID,
			SKUCode:           orString(t.skuCode, row.SKUCode),
			Delta:             delta,
			ReservedQuantity:  remaining,
			Quantity:          row.Quantity,
			AvailableQuantity: availableOf(row.Quantity, remaining),
		})
	}
	return out, nil
}

// availableOf 可售数量（track_quantity ? max(quantity - reserved, 0) : 0 的 track 侧的算法）。
//
// 夹 0 的理由见 dto.StockResp.AvailableQuantity：DDL 的 CHECK 保证正常不为负，
// 但历史脏数据可能违反；读模型显示负数比显示 0 更容易吓到运营。**写路径不夹**。
func availableOf(quantity, reserved int) int {
	if v := quantity - reserved; v > 0 {
		return v
	}
	return 0
}

// —— 台账页读模型（/admin/inventory/skus）——
//
// 为什么读模型在 service 而不是控制器：页面要的「认领到谁」不是库存行上的一个字段，
// 而是 variant_id → 「商品名 · SKU」的一次映射；这段派生（连同未认领的空态文案）
// 属于读模型，控制器只按下标把两段数据交出去（controller_orchestration_test.go 的
// 单入口 ≤6 条语句判据）。模板更不该做它 —— 那要在前端持一张变体表（template_logic）。

// ListStockLedger 台账页数据：库存行（按认领状态 / 仓 / 关键字过滤）+ 认领候选变体。
//
// 变体标签的来源是调用方传进来的 options（页面已经为下拉查过一次，复用同一份，
// 不在这里再查一次商品域）：两个下拉与台账列必须给出同一个标签，
// 各查一份就会出现「这行认领显示 A、下拉里叫 B」。
func (s *Service) ListStockLedger(ctx context.Context, req *inventorydto.StockLedgerQueryReq,
	options []inventorydto.VariantOptionResp) (*inventorydto.StockLedgerPage, error) {
	projectID, err := s.resolveProjectID(ctx, req.ProjectID)
	if err != nil {
		return nil, err
	}
	page, size := ledgerPageArgs(req)
	rows, err := s.m.ListWarehouseSKUs(ctx, inventorymodel.WarehouseSKUFilter{
		ProjectID:   projectID,
		WarehouseID: strings.TrimSpace(req.WarehouseID),
		Keyword:     strings.TrimSpace(req.Keyword),
		Claimed:     claimedFilterOf(req.Claimed),
	}, size, (page-1)*size)
	if err != nil {
		return nil, err
	}
	label := make(map[string]string, len(options))
	for _, o := range options {
		label[o.VariantID] = o.Label
	}
	out := &inventorydto.StockLedgerPage{
		Rows:    make([]inventorydto.StockLedgerRowResp, 0, len(rows)),
		Total:   len(rows),
		Options: options,
	}
	for _, r := range rows {
		if r == nil {
			continue
		}
		// 「认领到谁」的文案：已认领 = 变体标签，未认领 = 一个明确的徽标文案。
		// 变体不在候选里（跨工程 / 已删除）时退化成变体 id —— 空白会让
		// 「已认领但看不到认到哪」变成一个没人能回答的问题。
		claimedLabel := r.SKUCode
		if !r.HasVariant {
			claimedLabel = inventoryenums.FallbackFor(inventoryenums.ErrStockNotClaimed)
		} else if v := inventorymodel.PtrString(r.VariantID); label[v] != "" {
			claimedLabel = label[v]
		} else if v := inventorymodel.PtrString(r.VariantID); v != "" {
			claimedLabel = v
		}
		out.Rows = append(out.Rows, inventorydto.StockLedgerRowResp{
			ID: r.ID, WarehouseID: r.WarehouseID,
			WarehouseName: r.WarehouseName, WarehouseCode: r.WarehouseCode,
			SKUCode: r.SKUCode, ExternalSKU: r.ExternalSKU,
			VariantID:         inventorymodel.PtrString(r.VariantID),
			Claimed:           r.HasVariant,
			ClaimedLabel:      claimedLabel,
			Quantity:          r.Quantity,
			ReservedQuantity:  r.ReservedQuantity,
			AvailableQuantity: availableOf(r.Quantity, r.ReservedQuantity),
			TrackQuantity:     r.TrackQuantity,
		})
	}
	return out, nil
}

// ledgerPageArgs 台账页分页参数归一（缺省与库存流水页同口径：一页 50、上限 200）。
func ledgerPageArgs(req *inventorydto.StockLedgerQueryReq) (page, size int) {
	inPage, inSize := 0, 0
	if req != nil {
		inPage, inSize = req.Page, req.Limit
	}
	paging := utils.NormalizePaging(inPage, inSize, ledgerDefaultPageSize, ledgerMaxPageSize)
	return paging.Page, paging.Size
}

const (
	ledgerDefaultPageSize = 50
	ledgerMaxPageSize     = 200
)

// StockAlert 库存预警（迁移 624 / 623，W4）：只统计跟踪数量的行。
//
// Alert = OutOfStock + LowStock：两个数字相加是**展示口径**（「共 N 项要处理」），
// 不是 SQL 的分组 —— 放在 service 里算一次，模板与报表不必各自再算。
func (s *Service) StockAlert(ctx context.Context, projectID string) (res *inventorydto.StockAlertResp, err error) {
	pid, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	row, err := s.m.StockAlertCounts(ctx, pid)
	if err != nil {
		return nil, err
	}
	return &inventorydto.StockAlertResp{
		ProjectID: pid, Total: row.Total,
		OutOfStock: row.OutOfStock, LowStock: row.LowStock,
		Alert: row.OutOfStock + row.LowStock,
	}, nil
}
