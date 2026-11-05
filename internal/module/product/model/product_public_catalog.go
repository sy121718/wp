package productmodel

import (
	"context"

	"gorm.io/gorm"

	productcontract "go_wp/internal/module/product/contract"
	"go_wp/pkg/rls"
)

// publicCatalogPublicationTable 是公开商品目录专用只读关联的发布子查询表。
// 商品模块只在这里读 presentation 的两张表，写归属仍在 presentation。
const publicCatalogPublicationTable = "presentation_instances"

// publicCatalogPathTrimSet 是 Go unicode.IsSpace 覆盖的全部空白字符。
// SQL 侧用它裁边判空，保证「路径全是空白」的判定与 strings.TrimSpace 一致。
const publicCatalogPathTrimSet = " \t\n\v\f\r\u0085\u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000"

// publicCatalogQuery 是列表与计数共用的同一 query builder：
// 商品过滤复用 collectionQuery，再 join 一实体一行的有效发布子查询。
func (m *Model) publicCatalogQuery(tx *gorm.DB, ctx context.Context, f CollectionFilter, lang string) *gorm.DB {
	publications := tx.Table(publicCatalogPublicationTable+" AS i").
		Select("i.entity_id, MIN(pp.active_path) AS active_path").
		Joins("JOIN presentation_publications AS pp ON pp.presentation_id = i.id").
		Where("i.project_id = ? AND i.entity_type = ?", f.ProjectID, productcontract.EntityTypeProduct).
		Where("pp.lang = ? AND pp.artifact_id IS NOT NULL", lang).
		Where("btrim(pp.active_path, ?) <> ''", publicCatalogPathTrimSet).
		Group("i.entity_id")
	return m.collectionQuery(tx, ctx, f).
		Joins("JOIN (?) AS catalog_publications ON catalog_publications.entity_id = products.id", publications)
}

// ListPublicCatalogPageWithTotal 在一次只读查询里交出本页行、精确总数与该语言的发布路径。
// 候选资格（工程、语言、有产物、路径非空）全由 SQL 判定；路径原值返回，不做二次裁剪。
func (m *Model) ListPublicCatalogPageWithTotal(ctx context.Context, f CollectionFilter, lang string, limit, offset int) (list []*ProductEntity, paths map[string]string, total int64, err error) {
	// 与 collectionPageRow 同形但字段独立：匿名嵌入未导出类型时 GORM 的 Scan 可能整段忽略。
	type publicCatalogRow struct {
		ProductEntity
		TotalCount int64  `gorm:"column:total_count"`
		ActivePath string `gorm:"column:active_path"`
	}
	rows := make([]publicCatalogRow, 0)
	paths = map[string]string{}
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		q := orderCollectionQuery(m.publicCatalogQuery(tx, ctx, f, lang), f.Order)
		if limit > 0 {
			q = q.Limit(limit).Offset(offset)
		}
		return q.Select(collectionSelectColumns + ", catalog_publications.active_path, COUNT(*) OVER() AS total_count").
			Find(&rows).Error
	})
	if err != nil {
		return nil, nil, 0, err
	}
	list = make([]*ProductEntity, 0, len(rows))
	for i := range rows {
		list = append(list, &rows[i].ProductEntity)
		paths[rows[i].ID] = rows[i].ActivePath
		if rows[i].TotalCount > total {
			total = rows[i].TotalCount
		}
	}
	return list, paths, total, nil
}

// CountPublicCatalog 与列表共用同一 builder 与同一发布筛选，供越界页补总数。
func (m *Model) CountPublicCatalog(ctx context.Context, f CollectionFilter, lang string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, f.ProjectID, func(tx *gorm.DB) error {
		return m.publicCatalogQuery(tx, ctx, f, lang).Count(&n).Error
	})
	return n, err
}
