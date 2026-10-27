package inventoryservice

// inventory_stock_calibrate.go — 「按目标值校准」这条窄口的实现（B8 商品表单的仓库明细）。
//
// 为什么单开一个文件而不是往 inventory_change.go 里再加一段：那条文件已经装着三条
// 写入形态（自足 / 事务透传 / 归还策略），本方法**一行写入逻辑都没有** —— 它做的全部
// 事情是「把商品表单给的 (仓, 仓库 SKU, 目标数量) 翻译成一次 adjust 变动」，真正建行 /
// 加锁 / 写回 / 流水全在 changeStockTrackingTx 里。把这段翻译放在变动实现旁边会让人
// 以为这里还有第二条写路径。
//
// 与库存页行内编辑（UpdateStockTracking）的分工：那条口同时改「跟踪开关 + 数量」，
// 是运营在库存页做的决策；本口**只校准数量**。
//
// 「不跟踪（无限）」与「填了数字」的关系：**填了数字就是要求跟踪**，由变动路径把该行
// 切成跟踪并写入目标值（inventory_change.go ③ 段的既有口径），两列同一条 UPDATE，
// 满足 DDL 的 CHECK (track_quantity OR quantity = 0)。
//
// 为什么这条口不拒绝「既有不跟踪行 + 非 0 目标值」：商品表单里一个变体的货行**天生**
// 是不跟踪的（SaveVariantList 建行时 quantity 传 nil，见 ensureVariantStockTx），
// 运营在仓库明细里填的数字就是这个口径的第一次声明 —— 拒绝它等于让这块面板永远只能
// 显示、不能录入，功能整体不可用。真正要守的边界是 DDL 那条 CHECK（不跟踪必须为 0），
// 而不是「谁来按下开关」。界面上对不跟踪行给了显式提示（填数字即改为跟踪）。
//
// 目标值 0 且不跟踪 = 口径不变（「不动」就是什么都不填），跳过：既有不跟踪行的当前值
// 恒 0，所以这一支与「目标值与当前值相同」是同一判据。

import (
	"context"
	"errors"
	"strings"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
	inventorymodel "go_wp/internal/module/inventory/model"

	"gorm.io/gorm"
)

// calibrateSourceRefPrefix 校准流水的来源引用前缀：一次校准落在哪个商品上，对账时要能查回去。
const calibrateSourceRefPrefix = "product:"

// CalibrateVariantStocksTx 把一批货行的库存校准到目标值（tx 非 nil 时落在调用方事务里）。
//
// 三条判据（都先于任何写入算出结论，不留半截）：
//
//  1. **填了数字就是要求跟踪**：不跟踪（无限）的行给非 0 目标值 → 变动路径把它切成跟踪
//     并写入该值（见文件头，以及 DDL 的 CHECK (track_quantity OR quantity = 0)）。
//     行不存在时同理：变动路径建行即跟踪 + 目标值 + 一条 adjust 流水。
//  2. **目标值与当前值相同**的行整行跳过：一次「保存商品但没改数量」不该产生流水
//     （流水是「货动过」的证据，不是「页面被点过保存」的日志）。
//  3. **行不存在**且目标值为 0：跳过（建一条 0 的跟踪行没有意义，反而把「还没入库」
//     渲染成「有货但卖光了」）。目标值非 0 才建行，由变动路径保证它同时被登记进
//     变体的引用清单（641 的 ②.5 段）。
func (s *Service) CalibrateVariantStocksTx(ctx context.Context, tx *gorm.DB, projectID, productID, variantID string,
	targets []inventorycontract.ProductStockTarget) (err error) {
	variantID = strings.TrimSpace(variantID)
	if variantID == "" {
		return inventoryenums.Err(inventoryenums.ErrStockVariantRequired)
	}
	if len(targets) == 0 {
		return nil
	}
	if len(targets) > maxBatchLines {
		return inventoryenums.Err(inventoryenums.ErrStockLinesTooMany)
	}
	pid, err := s.resolveProjectID(ctx, projectID)
	if err != nil {
		return err
	}
	lines := make([]inventorydto.StockChangeLineReq, 0, len(targets))
	for _, t := range targets {
		line, keep, lerr := s.calibrateLine(ctx, pid, productID, variantID, t)
		if lerr != nil {
			return lerr
		}
		if keep {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		// 全部行都「已经是目标值」或「不参与校准」：没有变动就没有流水，也没有写入。
		return nil
	}
	req := &inventorydto.ChangeStockReq{
		ProjectID:  pid,
		Direction:  inventoryenums.DirectionAdjust,
		ReasonCode: inventoryenums.ReasonManualAdjust,
		SourceType: inventoryenums.MovementSourceProductForm,
		SourceRef:  calibrateSourceRefPrefix + strings.TrimSpace(productID),
		Lines:      lines,
	}
	// 复用变动契约的事件实现：加锁序、流水构造、变体引用登记三件事只此一份。
	_, _, _, cerr := s.changeStockTrackingTx(ctx, tx, req, nil)
	return cerr
}

// calibrateLine 把一条目标翻译成变动行；keep 为假表示这一行不需要写。
func (s *Service) calibrateLine(ctx context.Context, projectID, productID, variantID string,
	t inventorycontract.ProductStockTarget) (line inventorydto.StockChangeLineReq, keep bool, err error) {
	if t.Quantity < 0 {
		return line, false, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
	}
	wh, werr := s.resolveWarehouse(ctx, projectID, t.WarehouseID)
	if werr != nil {
		return line, false, werr
	}
	// 编码归一：仓库里的行永远叫裸码（前缀只属于商品侧）。调用方给的是商品侧编码时
	// 这里顺手剥掉 —— 与变动契约同一口径（normalizeStockSKU）。
	bare, berr := normalizeStockSKU(strings.TrimSpace(t.SKUCode), wh.Code)
	if berr != nil {
		bare = ""
	}
	row := (*inventorymodel.StockEntity)(nil)
	if bare != "" {
		got, gerr := s.m.GetStockByWarehouseSKU(ctx, wh.ID, bare, projectID)
		switch {
		case gerr == nil:
			row = got
		case errors.Is(gerr, gorm.ErrRecordNotFound):
		default:
			return line, false, gerr
		}
	}
	if row == nil && strings.TrimSpace(t.StockID) != "" {
		// 没给编码时退到 id：引用清单里记着 stockId，那一行就是这条目标的对象。
		got, gerr := s.m.GetStock(ctx, strings.TrimSpace(t.StockID), projectID)
		switch {
		case gerr == nil:
			row = got
			bare = row.SKUCode
		case errors.Is(gerr, gorm.ErrRecordNotFound):
		default:
			return line, false, gerr
		}
	}
	if bare == "" {
		return line, false, inventoryenums.Err(inventoryenums.ErrStockSKURequired)
	}
	if row == nil {
		// 行还不存在：目标值为 0 就是「什么都不用建」（见方法注释第 3 条）。
		if t.Quantity == 0 {
			return line, false, nil
		}
	} else {
		if row.Quantity == t.Quantity {
			// 数量已经就是目标值：不写。不跟踪行当前值恒 0，所以「目标 0」也走这一支
			// —— 口径不变，不该产生一次「切成跟踪又立刻是 0」的改动。
			return line, false, nil
		}

	}
	return inventorydto.StockChangeLineReq{
		WarehouseID: wh.ID,
		ProductID:   strings.TrimSpace(productID),
		VariantID:   variantID,
		SKUCode:     bare,
		Quantity:    t.Quantity,
	}, true, nil
}
