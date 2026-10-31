package inventoryservice

import (
	"context"
	"strings"

	inventorydto "go_wp/internal/module/inventory/dto"
	inventoryenums "go_wp/internal/module/inventory/enums"
)

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
			continue
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
	candidates, err := s.deductCandidatesOf(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}

	avail := make(map[string][]deductCandidate, len(variantIDs))
	for _, c := range candidates {
		if c == nil || c.variantID == "" || !c.trackQuantity {

			continue
		}
		avail[c.variantID] = append(avail[c.variantID], *c)
	}
	out := make([]inventorydto.StockChangeLineReq, len(lines))
	copy(out, lines)

	cursor := make(map[string]int, len(variantIDs))
	for _, idx := range pending {
		line := out[idx]
		v := strings.TrimSpace(line.VariantID)
		pieces, aerr := allocatePieces(avail[v], cursor[v], line.Quantity)
		if aerr != nil {
			return nil, aerr
		}
		cursor[v] += line.Quantity

		out[idx].WarehouseID = pieces[0].warehouseID
		out[idx].SKUCode = pieces[0].skuCode
		out[idx].Quantity = pieces[0].take
		for _, p := range pieces[1:] {
			extra := line
			extra.WarehouseID = p.warehouseID
			extra.SKUCode = p.skuCode
			extra.Quantity = p.take
			out = append(out, extra)
		}
	}
	return out, nil
}

func (s *Service) deductCandidatesOf(ctx context.Context, projectID string, variantIDs []string) (out []*deductCandidate, err error) {
	if s.refs == nil {
		rows, rerr := s.m.ListDeductCandidates(ctx, projectID, variantIDs)
		if rerr != nil {
			return nil, rerr
		}
		for _, r := range rows {
			if r == nil || r.VariantID == nil {
				continue
			}
			out = append(out, &deductCandidate{
				variantID: *r.VariantID, warehouseID: r.WarehouseID, skuCode: r.SKUCode,
				available: availableOf(r.Quantity, r.ReservedQuantity), trackQuantity: r.TrackQuantity,
			})
		}
		return out, nil
	}
	byVariant, err := s.refs.WarehouseStockIDsByVariant(ctx, projectID, variantIDs)
	if err != nil {
		return nil, err
	}
	stockIDs := make([]string, 0, len(byVariant)*2)
	for _, ids := range byVariant {
		stockIDs = append(stockIDs, ids...)
	}
	if len(stockIDs) == 0 {
		return nil, nil
	}
	rows, err := s.m.ListStockCandidatesByIDs(ctx, projectID, stockIDs)
	if err != nil {
		return nil, err
	}

	owners := make(map[string][]string, len(stockIDs))
	for variantID, ids := range byVariant {
		for _, id := range ids {
			owners[id] = append(owners[id], variantID)
		}
	}
	for _, r := range rows {
		if r == nil {
			continue
		}
		for _, variantID := range owners[r.ID] {
			out = append(out, &deductCandidate{
				variantID: variantID, warehouseID: r.WarehouseID, skuCode: r.SKUCode,
				available: availableOf(r.Quantity, r.ReservedQuantity), trackQuantity: r.TrackQuantity,
			})
		}
	}
	return out, nil
}

type deductCandidate struct {
	variantID     string
	warehouseID   string
	skuCode       string
	available     int
	trackQuantity bool
	take          int
}

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
		pieces = append(pieces, deductCandidate{
			variantID: c.variantID, warehouseID: c.warehouseID, skuCode: c.skuCode, take: take,
		})
		need -= take
	}
	if len(pieces) == 0 {

		return nil, inventoryenums.Err(inventoryenums.ErrStockInsufficient)
	}
	return pieces, nil
}
