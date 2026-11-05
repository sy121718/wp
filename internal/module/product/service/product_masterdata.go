package productservice

import (
	"context"

	"gorm.io/gorm"

	masterdatacontract "go_wp/internal/module/masterdata/contract"
	masterdataenums "go_wp/internal/module/masterdata/enums"
	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
)

func productChangeSnapshot(e *productmodel.ProductEntity) masterdatacontract.FieldSnapshot {
	if e == nil {
		return nil
	}
	return masterdatacontract.NewSnapshot(
		"name", e.Name,
		"slug", e.Slug,
		"status", e.Status,
		"default_price", masterdatacontract.FormatPricePtr(e.DefaultPrice),
		"brand_id", masterdatacontract.FormatStringPtr(e.BrandID),
	)
}

func variantChangeSnapshot(v *productmodel.VariantEntity, ref *productcontract.WarehouseRef) masterdatacontract.FieldSnapshot {
	if v == nil {
		return nil
	}
	snap := masterdatacontract.NewSnapshot(
		"sku_code", v.SKUCode,
		"barcode", v.Barcode,
		"price", masterdatacontract.FormatPrice(v.Price),
		"compare_price", masterdatacontract.FormatPricePtr(v.ComparePrice),
		"cost_price", masterdatacontract.FormatPricePtr(v.CostPrice),
		"enabled", masterdatacontract.FormatBool(v.Enabled),
		"option_values", masterdatacontract.FormatJSON(v.OptionValues),
	)
	if ref != nil {
		snap["home_warehouse_id"] = ref.ID
		snap["home_warehouse_code"] = ref.Code
	}
	return snap
}

func (s *Service) variantProjectID(ctx context.Context, v *productmodel.VariantEntity, scopeID string) (projectID string, err error) {
	if v == nil {
		return "", nil
	}

	p, gerr := s.m.GetRefFacts(ctx, v.ProductID, scopeID)
	if gerr != nil {
		return "", mapNotFound(gerr)
	}
	return p.ProjectID, nil
}

func (s *Service) recordChanges(ctx context.Context, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChanges(ctx, inputs)
}

func (s *Service) recordChangesTx(ctx context.Context, tx *gorm.DB, inputs ...*masterdatacontract.ChangeInput) (err error) {
	if len(inputs) == 0 {
		return nil
	}
	return s.changes.RecordChangesTx(ctx, tx, inputs)
}

func productChangeInput(e *productmodel.ProductEntity, action, origin, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if e == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: e.ProjectID, EntityType: masterdataenums.EntityProduct,
		EntityID: e.ID, EntityLabel: e.Name,
		Action: action, Origin: origin, OperatorID: operator,
		Before: before, After: after,
	}
}

func variantChangeInput(projectID string, v *productmodel.VariantEntity, action, origin, operator string,
	before, after masterdatacontract.FieldSnapshot) *masterdatacontract.ChangeInput {
	if v == nil {
		return nil
	}
	return &masterdatacontract.ChangeInput{
		ProjectID: projectID, EntityType: masterdataenums.EntityProductVariant,
		EntityID: v.ID, EntityLabel: v.SKUCode,
		Action: action, Origin: origin, OperatorID: operator,
		Before: before, After: after,
	}
}
