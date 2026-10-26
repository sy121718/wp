package productmodel

// variant_warehouse_sku_scan.go — 按库存行反查引用它的变体（迁移 637 的 GIN 索引服务这个查询）。
//
// 查询形态是 JSONB **包含**（@>）：`warehouse_skus @> [{"stockId":"<id>"}]`。
// jsonb_path_ops 的 GIN 索引（idx_product_variants_warehouse_skus）正好覆盖它；
// 换成 `jsonb_array_elements + ->` 那类写法就走不了索引，守卫会变成全表扫。
//
// 工程隔离：product_variants 没有 project_id 列，隔离在归属商品那一步做 ——
// 所以这条查询 JOINS products 并带 `p.project_id = ?`。这是 product 模块内部的
// 两张表 join（同模块），不违反表隔离约定。

import (
	"context"
	"strings"

	json "go_wp/pkg/jsonx"
)

// VariantIDsByWarehouseStock 反查引用着这条库存行的变体 id（同工程内）。
//
// 返回空切片 = 没有变体引用它；调用方据此判断「可以删」与「有人用着」。
// 顺序按变体 id 升序，让同一份数据每次给出同样的答案（守卫报错信息里的清单不该抖）。
func (m *Model) VariantIDsByWarehouseStock(ctx context.Context, projectID, stockID string) (variantIDs []string, err error) {
	// 参数用 json.Marshal 拼而不是字符串拼接：stockID 是 uuid，但仍按参数化传，
	// 免得「库里的 stockId 带特殊字符」这类假设日后被打破时变成注入点。
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

// VariantsByWarehouseStocks 批量反查：取出引用了这批库存行**任意一条**的变体（含其引用列）。
//
// 谓词用 `@> ANY(ARRAY[...]::jsonb[])`：一次索引扫描覆盖全部探针，比逐行查少 N-1 次往返。
// 返回整实体是刻意的 —— 调用方要按 stockId 归位，归位信息就在 warehouse_skus 里。
func (m *Model) VariantsByWarehouseStocks(ctx context.Context, projectID string, stockIDs []string) (rows []*VariantEntity, err error) {
	if len(stockIDs) == 0 {
		return nil, nil
	}
	// 探针逐个参数化后拼成 ARRAY[?::jsonb, ...]：gorm 会把切片参数展开成 `?,?,?`，
	// 落到 ANY('[...]','[...]') 是语法错（42601）；而把整批塞进一个 jsonb 再靠
	// jsonb_array_elements 展开，在 ANY 里不参与谓词求值（恒空）。逐个占位符是两条都不踩的写法。
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
