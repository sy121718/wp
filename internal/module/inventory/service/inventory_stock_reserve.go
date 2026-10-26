package inventoryservice

// inventory_stock_reserve.go — 预占 / 释放（迁移 624）。
//
// 两条主线：
//
//	预占  下单：按 (变体, 仓) 找行，reserved_quantity += n（本文件 ReserveStockTx）
//	出库  发货：quantity -= n 且 reserved_quantity -= min(reserved, n)，写 sale_out 流水
//	      → inventory_change.go 的 deductStockTx（经 model.DeductStockOnShipTx）
//	释放  取消 / 退款 / 超时：只 reserved_quantity -= n，不写流水（本文件 ReleaseStockTx）
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
// 加锁顺序与 applyStockChangesTx 一致：按 (仓库, 仓库 SKU) 升序逐一
// `FOR UPDATE`。出入库与预占共享同一个全局顺序，等待链不可能成环。

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"gorm.io/gorm"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"
)

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
//  3. 取目标行并按 (仓库, 仓库 SKU) 升序 `FOR UPDATE`；
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
	key       stockKey
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
		// 行的身份是 (仓库, 仓库 SKU)（迁移 641）：预占也必须落到具体那一条货行上，
		// 否则同变体同仓有多条货时「占了哪一条」无从回答。
		key, serr := s.resolveLineStockKey(ctx, projectID, wh, variantID, line.SKUCode)
		if serr != nil {
			return nil, serr
		}
		if t, ok := byKey[key]; ok {
			t.delta += line.Quantity
			continue
		}
		byKey[key] = &reserveTarget{
			key: key, warehouse: wh, variantID: variantID,
			skuCode: key.skuCode,
			delta:   line.Quantity,
		}
		order = append(order, key)
	}
	// 锁序固定：与 applyStockChangesTx 同一条升序（uniqueSortedKeys 就是干这个的）。
	sorted := make([]stockKey, len(order))
	copy(sorted, order)
	slices.SortFunc(sorted, func(a, b stockKey) int {
		return cmp.Or(cmp.Compare(a.warehouseID, b.warehouseID), cmp.Compare(a.skuCode, b.skuCode))
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
		row, lerr := s.m.LockStockBySKUTx(ctx, tx, projectID, t.key.warehouseID, t.key.skuCode)
		if lerr != nil {
			if errors.Is(lerr, gorm.ErrRecordNotFound) {
				// 该 (仓, 仓库 SKU) 没有库存行：预占无从谈起。
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
