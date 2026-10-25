package inventoryservice

// inventory_deduct_priority.go — 出库跨仓分配（迁移 626，W2）。
//
// 对齐 ERP 的 listDeductCandidates：一次 IN 取全部候选 → 内存里按优先级分配 → 统一下传。
//
// 为什么不「逐行 ORDER BY 可售 DESC LIMIT 1 再更新」：
//
//	· 每次更新都会加锁，逐行取会把锁范围从「本批要动的行」扩大到「扫到的行」；
//	· 往返次数随变体数 × 仓数增长，一批十几个口味就是几十次查询；
//	· 更关键的是**加锁顺序不固定** —— 每次按「当前可售」选仓，两个并发批次可能选到不同的
//	  第一个仓，锁序就散了，等待链有环（applyStockChangesTx 的顺序保证随之失效）。
//
// 候选按固定顺序取（priority → 仓短码 → 仓 id → 行 id），分配在内存里完成，
// 最后把**填好仓库的行**交回 buildChangeItems 走既有那条路径（解析 / 校验 / 加锁 / 流水
// 全部复用，不新开第二条写路径）。
//
// **可售 vs 在库**：分配用可售（quantity - reserved_quantity），不是 quantity ——
// 预占是留给订单的，不能被另一张单吃掉。
//
// **不跟踪（无限）行**：没有数量，reserved 恒 0，不参与分配（沿现有 stockPolicy 语义：
// 某变体只有无限行时出库直接成功、不扣减）。

import (
	"context"
	"strings"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

// assignDeductWarehouses 给「没写仓」的出库行按仓库优先级分配具体的仓。
//
// 判据是**请求里真的写了仓**（line.WarehouseID 或 req.WarehouseID 非空），
// 不是「解析出来等于默认仓」：后者分不清「运营选的就是默认仓」与「没选」，
// 而这两种情况下该不该让优先级改道，答案完全相反 —— 显式选择永远优先。
//
// 只处理出库（DirectionOut）：入库 / 调整的仓是「收货到哪」，不是「从哪发货」，
// 让优先级替它挑仓会把货记到运营没指定的仓里。
//
// 返回的行是**副本**（原入参不动）：调用方随后把它交给 buildChangeItems，
// 那条路径不再区分「原来有没有写仓」。
func (s *Service) assignDeductWarehouses(ctx context.Context, projectID, direction, defaultWarehouseID string,
	lines []inventorydto.StockChangeLineReq) ([]inventorydto.StockChangeLineReq, error) {
	if direction != inventoryenums.DirectionOut || strings.TrimSpace(defaultWarehouseID) != "" {
		return lines, nil
	}
	pending := make([]int, 0, len(lines))
	variantIDs := make([]string, 0, len(lines))
	seen := make(map[string]bool, len(lines))
	for i := range lines {
		if strings.TrimSpace(lines[i].WarehouseID) != "" {
			continue // 显式指定的仓：优先级不得覆盖。
		}
		pending = append(pending, i)
		v := strings.TrimSpace(lines[i].VariantID)
		if v != "" && !seen[v] {
			seen[v] = true
			variantIDs = append(variantIDs, v)
		}
	}
	if len(pending) == 0 {
		return lines, nil
	}
	candidates, err := s.m.ListDeductCandidates(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}
	// 按变体收集候选（候选已按全局顺序排好；组内顺序与全局顺序一致）。
	avail := make(map[string][]deductCandidate, len(variantIDs))
	for _, c := range candidates {
		if c == nil || c.VariantID == nil || !c.TrackQuantity {
			// 无限行不参与分配：quantity 恒 0，可售也恒 0，留着只会空转。
			continue
		}
		v := strings.TrimSpace(*c.VariantID)
		avail[v] = append(avail[v], deductCandidate{
			warehouseID: c.WarehouseID, available: availableOf(c.Quantity, c.ReservedQuantity),
		})
	}
	out := make([]inventorydto.StockChangeLineReq, len(lines))
	copy(out, lines)
	// 每个变体的游标：多行同变体时接着上一行切，不重复从第一个仓开始。
	cursor := make(map[string]int, len(variantIDs))
	for _, idx := range pending {
		line := out[idx]
		v := strings.TrimSpace(line.VariantID)
		pieces, aerr := allocatePieces(avail[v], cursor[v], line.Quantity)
		if aerr != nil {
			return nil, aerr
		}
		cursor[v] += line.Quantity
		// 第一片写回原行，其余追加（同一 (变体, 仓) 由 buildChangeItems 之后的
		// applyStockChangesTx 按 key 汇总，不会重复加锁）。
		out[idx].WarehouseID = pieces[0].warehouseID
		out[idx].Quantity = pieces[0].take
		for _, p := range pieces[1:] {
			extra := line
			extra.WarehouseID = p.warehouseID
			extra.Quantity = p.take
			out = append(out, extra)
		}
	}
	return out, nil
}

// deductCandidate 一个跨仓分配的候选（仓 + 该仓的可售）；分配后 take 是分到这一仓的量。
type deductCandidate struct {
	warehouseID string
	available   int
	take        int
}

// allocatePieces 从一个变体的候选序列里切出若干片，凑够 quantity。
//
// skip 是「前面几行已经吃掉的量」：多行同变体时，第二行必须从第一行停下的地方继续切，
// 否则会从第一个仓重新分一遍（同一份可售被算两次，扣减时才发现不够）。
//
// 可售总合不足 → ErrStockInsufficient（**整单拒绝**：不做部分扣减、不推算 skipped）。
func allocatePieces(candidates []deductCandidate, skip, quantity int) (pieces []deductCandidate, err error) {
	total := 0
	for _, c := range candidates {
		total += c.available
	}
	if total-skip < quantity {
		return nil, inventoryenums.Err(inventoryenums.ErrStockInsufficient)
	}
	need := quantity
	remainSkip := skip
	for _, c := range candidates {
		if need <= 0 {
			break
		}
		if remainSkip >= c.available {
			remainSkip -= c.available
			continue
		}
		usable := c.available - remainSkip
		remainSkip = 0
		take := need
		if take > usable {
			take = usable
		}
		if take <= 0 {
			continue
		}
		pieces = append(pieces, deductCandidate{warehouseID: c.warehouseID, take: take})
		need -= take
	}
	if len(pieces) == 0 {
		// 走不到（上面已判 total-skip >= quantity），留着兜住未来的改动。
		return nil, inventoryenums.Err(inventoryenums.ErrStockInsufficient)
	}
	return pieces, nil
}
