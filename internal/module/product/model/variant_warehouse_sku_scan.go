package productmodel

import (
	"context"
	"strings"

	json "go_wp/pkg/jsonx"
)

func (m *Model) VariantIDsByWarehouseStock(ctx context.Context, projectID, stockID string) (variantIDs []string, err error) {

	probe, jerr := json.Marshal([]map[string]string{{"stockId": stockID}})
	if jerr != nil {
		return nil, jerr
	}
	err = m.db.WithContext(ctx).
		Table("product_variants AS v").
		Select("v.id").
		Joins("JOIN products AS p ON p.id = v.product_id").
		Where("p.project_id = ?", projectID).
		Where("v.warehouse_skus @> ?::jsonb", string(probe)).
		Order("v.id ASC").
		Pluck("v.id", &variantIDs).Error
	if err != nil {
		return nil, err
	}
	return variantIDs, nil
}

func (m *Model) VariantsByWarehouseStocks(ctx context.Context, projectID string, stockIDs []string) (rows []*VariantEntity, err error) {
	if len(stockIDs) == 0 {
		return nil, nil
	}

	args := make([]any, 0, len(stockIDs))
	marks := make([]string, 0, len(stockIDs))
	for _, id := range stockIDs {
		b, jerr := json.Marshal([]map[string]string{{"stockId": id}})
		if jerr != nil {
			return nil, jerr
		}
		args = append(args, string(b))
		marks = append(marks, "?::jsonb")
	}
	cond := "v.warehouse_skus @> ANY(ARRAY[" + strings.Join(marks, ", ") + "])"
	args = append([]any{projectID}, args...)
	err = m.db.WithContext(ctx).
		Table("product_variants AS v").
		Select("v.*").
		Joins("JOIN products AS p ON p.id = v.product_id").
		Where("p.project_id = ?", projectID).
		Where(cond, args[1:]...).
		Order("v.id ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}
