package orderstock

// ship_probe.go — 发货前「这批货现在能不能发」的只读投影（B6-a，对齐 dy_erp 仓库发货台）。
//
// 订单侧持有 ordercontract.ShipStockProbe 这个窄端口，实现放在库存侧 ——
// 与 Operator / WarehouseResolver / WarehouseSource 同一手法（实现方依赖调用方契约）。
//
// 判据取自写路径，不另造一套：
//   · 变体为空 → 库存侧 buildChangeItems 直接 ErrStockVariantRequired；
//   · 该仓没有这个变体的货行 → 出库会幂等建一条 track_quantity=false 的行，
//     而 applyStockChangesOn 对「不跟踪 + 出库」是 continue（不校验也不扣减）→ 恒可发；
//   · 有跟踪行时判 quantity >= 本次件数（DeductStockOnShipTx 的 WHERE），
//     **不减 reserved_quantity**：这张订单自己的预占也在 reserved 里，而发货 SQL
//     同时把它释放掉 —— 减掉会把「货已经替这张单留好了」的单误判成发不出去。
//
// 这里只做投影：投影说能发、并发卖掉了，提交仍会被行锁拒绝。禁用按钮是提示，不是正确性。

import (
	"context"

	inventorycontract "go_wp/internal/module/inventory/contract"
	ordercontract "go_wp/internal/module/order/contract"
	productcontract "go_wp/internal/module/product/contract"
)

// ShipProbe 库存侧实现的发货可发性投影。
type ShipProbe struct {
	stocks inventorycontract.ProductStockLookup
	refs   productcontract.VariantWarehouseStockPort
}

// 编译期断言：适配器必须满足订单侧的可发性端口。
var _ ordercontract.ShipStockProbe = (*ShipProbe)(nil)

// NewShipProbe 构造。stocks 读货行事实与默认仓，refs 回答「这个变体引用了哪些货行」。
func NewShipProbe(stocks inventorycontract.ProductStockLookup, refs productcontract.VariantWarehouseStockPort) *ShipProbe {
	return &ShipProbe{stocks: stocks, refs: refs}
}

// ShipAvailabilityByLine 逐行给出可发性事实（键 = 订单行 id）。
//
// 一次批量：变体 id 去重后只取一次引用清单、只取一次货行事实 —— 逐行查会把
// 「一屏 20 单 × 每单几行」放大成上百次往返。
func (p *ShipProbe) ShipAvailabilityByLine(ctx context.Context, projectID string,
	lines []ordercontract.ShipProbeLine) (map[uint64]ordercontract.ShipAvailability, error) {
	out := make(map[uint64]ordercontract.ShipAvailability, len(lines))
	if len(lines) == 0 {
		return out, nil
	}
	variantIDs := make([]string, 0, len(lines))
	for _, line := range lines {
		if line.VariantID != "" {
			variantIDs = append(variantIDs, line.VariantID)
		}
	}
	// 引用清单与 SKU 兜底：两者都空 = 库存侧认不出这件货（发货会 ErrStockSKURequired）。
	byVariant := map[string][]string{}
	skuOf := map[string]string{}
	if len(variantIDs) > 0 && p.refs != nil {
		ids, err := p.refs.WarehouseStockIDsByVariant(ctx, projectID, variantIDs)
		if err != nil {
			return nil, err
		}
		byVariant = ids
		skus, serr := p.refs.VariantSKUCodes(ctx, projectID, variantIDs)
		if serr != nil {
			return nil, serr
		}
		skuOf = skus
	}
	stockIDs := make([]string, 0, len(variantIDs))
	for _, ids := range byVariant {
		stockIDs = append(stockIDs, ids...)
	}
	facts := map[string][]*inventorycontract.StockFact{}
	if len(stockIDs) > 0 {
		list, err := p.stocks.StockFactsByStockIDs(ctx, projectID, stockIDs)
		if err != nil {
			return nil, err
		}
		for _, f := range list {
			if f == nil {
				continue
			}
			facts[f.StockID] = append(facts[f.StockID], f)
		}
	}
	// 默认仓只解析一次（多行都没固仓时共用同一个答案）。
	defaultWarehouse, resolved := "", false
	for _, line := range lines {
		avail := ordercontract.ShipAvailability{}
		switch {
		case line.VariantID == "":
			// 下单快照里就没有变体：发货时库存侧 ErrStockVariantRequired。
			out[line.OrderItemID] = avail
			continue
		case len(byVariant[line.VariantID]) == 0 && skuOf[line.VariantID] == "":
			// 引用清单与 SKU 都拿不到：库存侧连该建哪一行都不知道（ErrStockSKURequired）。
			avail.Unmatched = true
			out[line.OrderItemID] = avail
			continue
		}
		warehouseID := line.WarehouseID
		if warehouseID == "" {
			if !resolved {
				resolved = true
				ref, err := p.stocks.ResolveWarehouse(ctx, projectID, "")
				if err != nil && !isNoWarehouse(err) {
					return nil, err
				}
				if ref != nil {
					defaultWarehouse = ref.ID
				}
			}
			warehouseID = defaultWarehouse
			if warehouseID == "" {
				avail.NoWarehouse = true
				out[line.OrderItemID] = avail
				continue
			}
		}
		tracked, total := 0, 0
		for _, id := range byVariant[line.VariantID] {
			for _, f := range facts[id] {
				if f.WarehouseID != warehouseID {
					continue
				}
				tracked++
				total += f.Quantity
			}
		}
		if tracked == 0 {
			// 该仓还没有这件货的任何行：出库会建一条不跟踪的行，数量不构成约束。
			avail.Infinite = true
		}
		avail.Available = total
		out[line.OrderItemID] = avail
	}
	return out, nil
}
