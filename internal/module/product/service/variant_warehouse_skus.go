package productservice

import (
	"context"
	"strings"
	"time"

	productcontract "go_wp/internal/module/product/contract"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"

	"gorm.io/gorm"
)

var _ productcontract.VariantWarehouseStockPort = (*Service)(nil)

func (s *Service) WarehouseStockIDsByVariant(ctx context.Context, projectID string, variantIDs []string) (ids map[string][]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	out := make(map[string][]string, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	list, err := s.m.ListVariantsByIDs(ctx, variantIDs, projectID)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		refs := productmodel.DecodeWarehouseSKUs(v.WarehouseSKUs)
		if len(refs) == 0 {
			continue
		}
		stockIDs := make([]string, 0, len(refs))
		for _, r := range refs {
			stockIDs = append(stockIDs, r.StockID)
		}
		out[v.ID] = stockIDs
	}
	return out, nil
}

func (s *Service) VariantProductIDs(ctx context.Context, projectID string, variantIDs []string) (byVariant map[string]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	out := make(map[string]string, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	list, err := s.m.ListVariantsByIDs(ctx, variantIDs, projectID)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		if v != nil && v.ProductID != "" {
			out[v.ID] = v.ProductID
		}
	}
	return out, nil
}

func (s *Service) AttachWarehouseStockTx(ctx context.Context, tx *gorm.DB, projectID, variantID string, ref productcontract.WarehouseStockRef) error {
	projectID = strings.TrimSpace(projectID)
	variantID = strings.TrimSpace(variantID)
	if projectID == "" || variantID == "" || strings.TrimSpace(ref.StockID) == "" {
		return productenums.Err(productenums.ErrInvalidParam)
	}

	var entity *productmodel.VariantEntity
	var err error
	if tx != nil {
		entity, err = s.m.GetVariantScopedTx(ctx, tx, variantID, projectID)
	} else {
		entity, err = s.m.GetVariantScoped(ctx, variantID, projectID)
	}
	if err != nil {
		return mapNotFound(err)
	}
	refs := productmodel.DecodeWarehouseSKUs(entity.WarehouseSKUs)
	next := productmodel.UpsertWarehouseSKURef(refs, productmodel.WarehouseSKURef{
		StockID:       ref.StockID,
		WarehouseID:   ref.WarehouseID,
		WarehouseCode: ref.WarehouseCode,
		SKUCode:       ref.SKUCode,
	})

	return s.m.UpdateVariantWarehouseSKUsTx(ctx, tx, variantID, productmodel.EncodeWarehouseSKUs(next), time.Now().UTC())
}

func (s *Service) VariantSKUCodes(ctx context.Context, projectID string, variantIDs []string) (byVariant map[string]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	out := make(map[string]string, len(variantIDs))
	if len(variantIDs) == 0 {
		return out, nil
	}
	list, err := s.m.ListVariantsByIDs(ctx, variantIDs, projectID)
	if err != nil {
		return nil, err
	}
	for _, v := range list {
		if v == nil {
			continue
		}
		if code := strings.TrimSpace(v.SKUCode); code != "" {
			out[v.ID] = code
		}
	}
	return out, nil
}

func (s *Service) VariantIDsByWarehouseStock(ctx context.Context, projectID, stockID string) (variantIDs []string, err error) {
	projectID = strings.TrimSpace(projectID)
	stockID = strings.TrimSpace(stockID)
	if projectID == "" || stockID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	return s.m.VariantIDsByWarehouseStock(ctx, projectID, stockID)
}

func (s *Service) VariantIDsByWarehouseStocks(ctx context.Context, projectID string, stockIDs []string) (byStock map[string][]string, err error) {
	projectID = strings.TrimSpace(projectID)
	if projectID == "" {
		return nil, productenums.Err(productenums.ErrInvalidParam)
	}
	out := make(map[string][]string, len(stockIDs))
	if len(stockIDs) == 0 {
		return out, nil
	}
	refs, err := s.m.VariantsByWarehouseStocks(ctx, projectID, stockIDs)
	if err != nil {
		return nil, err
	}
	wanted := make(map[string]bool, len(stockIDs))
	for _, id := range stockIDs {
		wanted[id] = true
	}
	for _, r := range refs {
		for _, ref := range productmodel.DecodeWarehouseSKUs(r.WarehouseSKUs) {
			if !wanted[ref.StockID] {
				continue
			}
			out[ref.StockID] = append(out[ref.StockID], r.ID)
		}
	}
	return out, nil
}
