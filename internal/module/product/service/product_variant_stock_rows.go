package productservice

import (
	"context"
	"sort"
	"strings"
	"time"

	inventorycontract "go_wp/internal/module/inventory/contract"
	inventoryenums "go_wp/internal/module/inventory/enums"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"

	"gorm.io/gorm"
)

func (s *Service) fillVariantListStocks(ctx context.Context, projectID string, rows []*productdto.VariantResp) error {
	if s.inv == nil || len(rows) == 0 {
		return nil
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		if row != nil && row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	entities, err := s.m.ListVariantsByIDs(ctx, ids, projectID)
	if err != nil {
		return err
	}
	refsByVariant := make(map[string][]productmodel.WarehouseSKURef, len(entities))
	stockIDs := make([]string, 0)
	seen := map[string]bool{}
	for _, e := range entities {
		refs := productmodel.DecodeWarehouseSKUs(e.WarehouseSKUs)
		refsByVariant[e.ID] = refs
		for _, r := range refs {
			if !seen[r.StockID] {
				seen[r.StockID] = true
				stockIDs = append(stockIDs, r.StockID)
			}
		}
	}
	byStock := make(map[string]*inventorycontract.StockFact, len(stockIDs))
	if len(stockIDs) > 0 {
		facts, err := s.inv.StockFactsByStockIDs(ctx, projectID, stockIDs)
		if err != nil {
			return err
		}
		for _, f := range facts {
			if f != nil {
				byStock[f.StockID] = f
			}
		}
	}
	for _, row := range rows {
		if row == nil {
			continue
		}
		refs := refsByVariant[row.ID]
		row.Stocks = groupVariantStockRows(refs, byStock)
		row.StockTotal = 0
		for _, ref := range refs {
			if fact := byStock[ref.StockID]; fact != nil && fact.TrackQuantity {
				row.StockTotal += fact.Quantity
			}
		}
		row.StocksKnown = true
	}
	return nil
}

func groupVariantStockRows(refs []productmodel.WarehouseSKURef,
	byStock map[string]*inventorycontract.StockFact) []productdto.VariantStockGroupResp {
	groups := make([]productdto.VariantStockGroupResp, 0, 2)
	index := make(map[string]int, 2)
	for _, r := range refs {
		f := byStock[r.StockID]
		if f == nil {
			continue
		}
		at, ok := index[f.WarehouseID]
		if !ok {
			at = len(groups)
			index[f.WarehouseID] = at
			groups = append(groups, productdto.VariantStockGroupResp{
				WarehouseID:   f.WarehouseID,
				WarehouseCode: f.WarehouseCode,
				WarehouseName: f.WarehouseName,
			})
		}
		groups[at].Rows = append(groups[at].Rows, productdto.VariantStockRowResp{
			WarehouseID:   f.WarehouseID,
			WarehouseCode: f.WarehouseCode,
			WarehouseName: f.WarehouseName,
			StockID:       f.StockID,
			SKUCode:       f.SKUCode,
			Quantity:      f.Quantity,
			TrackQuantity: f.TrackQuantity,
			Name:          f.Name,
			ImageURL:      f.ImageURL,
		})
	}
	return groups
}

type variantStockApply struct {
	VariantID string
	Prune     bool
	Rows      []productdto.VariantStockRowResp
}

func planVariantStockApply(byVariant map[string][]productdto.VariantStockRowResp,
	prune map[string]bool) (plan []variantStockApply, err error) {
	if len(byVariant) == 0 {
		return nil, nil
	}
	variantIDs := make([]string, 0, len(byVariant))
	for id := range byVariant {
		variantIDs = append(variantIDs, id)
	}
	sort.Strings(variantIDs)

	planned := make(map[string]int, len(variantIDs))
	plan = make([]variantStockApply, 0, len(variantIDs))
	for _, variantID := range variantIDs {
		rows := make([]productdto.VariantStockRowResp, 0, len(byVariant[variantID]))
		seen := make(map[string]bool, len(byVariant[variantID]))
		for _, r := range byVariant[variantID] {
			key := stockRowKey(r)
			if key == "" {
				continue
			}
			if r.Quantity < 0 {

				return nil, inventoryenums.Err(inventoryenums.ErrStockQuantityInvalid)
			}
			if seen[key] {
				return nil, productenums.Err(productenums.ErrVariantStockDuplicated)
			}
			seen[key] = true
			if qty, ok := planned[key]; ok {
				if qty != r.Quantity {
					return nil, productenums.Err(productenums.ErrVariantStockDuplicated)
				}
				continue
			}
			planned[key] = r.Quantity
			rows = append(rows, r)
		}
		plan = append(plan, variantStockApply{VariantID: variantID, Prune: prune[variantID], Rows: rows})
	}
	return plan, nil
}

func stockRowKey(r productdto.VariantStockRowResp) string {
	warehouseID := strings.TrimSpace(r.WarehouseID)
	sku := strings.TrimSpace(r.SKUCode)
	if warehouseID == "" || sku == "" {
		return ""
	}
	return warehouseID + "|" + sku
}

func (s *Service) applyVariantStockRowsTx(ctx context.Context, tx *gorm.DB, projectID, productID string,
	step variantStockApply) error {
	if s.invSvc == nil || strings.TrimSpace(step.VariantID) == "" {
		return nil
	}
	v, gerr := s.m.GetVariantScopedTx(ctx, tx, step.VariantID, projectID)
	if gerr != nil {
		return mapNotFound(gerr)
	}
	type resolvedRow struct {
		ref *productcontract.WarehouseRef
		sku string
	}
	resolved := make([]resolvedRow, 0, len(step.Rows))
	targets := make([]inventorycontract.ProductStockTarget, 0, len(step.Rows))
	for _, r := range step.Rows {
		warehouseID := strings.TrimSpace(r.WarehouseID)
		if warehouseID == "" {
			return productenums.Err(productenums.ErrInvalidParam)
		}
		ref, rerr := s.invSvc.ResolveWarehouse(ctx, projectID, warehouseID)
		if rerr != nil {
			return rerr
		}
		if ref == nil {
			return productenums.Err(productenums.ErrInvalidParam)
		}
		sku := strings.TrimSpace(r.SKUCode)
		if sku == "" {
			return productenums.Err(productenums.ErrVariantSKUEmpty)
		}

		sku = stripWarehousePrefix(sku, ref.Code)
		resolved = append(resolved, resolvedRow{ref: ref, sku: sku})
		targets = append(targets, inventorycontract.ProductStockTarget{
			StockID: r.StockID, WarehouseID: ref.ID, SKUCode: sku, Quantity: r.Quantity,
		})
	}

	if len(targets) > 0 {
		if cerr := s.invSvc.CalibrateVariantStocksTx(ctx, tx, projectID, productID, step.VariantID, targets); cerr != nil {
			return cerr
		}
	}

	submitted := make(map[string]bool, len(resolved))
	for _, r := range resolved {
		stockID, aerr := s.attachWarehouseStockTx(ctx, tx, r.ref, v, r.sku, "", nil)
		if aerr != nil {
			return aerr
		}
		if stockID != "" {
			submitted[stockID] = true
		}
	}
	if !step.Prune {
		return nil
	}
	return s.pruneVariantStockRefsTx(ctx, tx, projectID, v, submitted)
}

func (s *Service) pruneVariantStockRefsTx(ctx context.Context, tx *gorm.DB, projectID string,
	v *productmodel.VariantEntity, submitted map[string]bool) error {
	refs := productmodel.DecodeWarehouseSKUs(v.WarehouseSKUs)
	kept := make([]productmodel.WarehouseSKURef, 0, len(refs))
	for _, r := range refs {
		if submitted[r.StockID] {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(refs) {

		return nil
	}
	raw := productmodel.EncodeWarehouseSKUs(kept)
	if err := s.m.UpdateVariantWarehouseSKUsTx(ctx, tx, v.ID, raw, time.Now().UTC()); err != nil {
		return err
	}
	v.WarehouseSKUs = raw
	return nil
}
