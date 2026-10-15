package productmodel

// product_ref_batch.go — 批量引用扫描（task-156，dd.txt §5.4）。
//
// 单条版（scanProductRefs）一次检查一个实体：逐工程开作用域、每工程一条采样查询
// （采样满额再补一条 COUNT 取总数）。批量删除要对 N 个实体做同样的检查，逐个调用就是
// O(N × 工程数) 条查询。
//
// 批量版把「N 个实体」压成一条查询：每个工程一次，用 jsonb_exists_any(col, ?::text[])
// （jsonb 数组列）或 col IN ?（真列）取出「引用了这批 id 里任意一个」的商品行，
// 再在 Go 里把每行命中的 id 归到各自的桶 —— 于是查询数是 O(工程数)，与实体个数无关。
//
// 与单条版的**刻意差异**：单条版要给人看，会带回最多 3 个样例商品（refRow /
// maxRefSamplePerProject）；批量版的调用方只需要「谁被引用了 / 被哪些工程引用」，
// 因此只回工程集合，不带样例，也不引入第二份样例格式。

import (
	"context"
	"encoding/json"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// BatchRefHit 批量扫描里「某个实体被谁引用」的结论。
type BatchRefHit struct {
	// RefColumns 命中的引用列（写进拒绝理由，形如 products.category_ids）。
	RefColumns []string
	// ProjectIDs 引用它的工程 id（去重、升序 —— 与 normalizeProjectIDs 的顺序一致）。
	ProjectIDs []string
}

// Referenced 是否被任何工程引用。
func (h *BatchRefHit) Referenced() bool { return h != nil && len(h.ProjectIDs) > 0 }

// addProject 记下一个引用工程（重复调用幂等；工程按升序枚举，故结果天然有序）。
func (h *BatchRefHit) addProject(projectID string) {
	if h == nil || projectID == "" {
		return
	}
	for _, got := range h.ProjectIDs {
		if got == projectID {
			return
		}
	}
	h.ProjectIDs = append(h.ProjectIDs, projectID)
}

// Outside 返回除 projectID 之外的引用工程（标签用：本工程内的引用可解绑，跨工程必须拒绝）。
func (h *BatchRefHit) Outside(projectID string) []string {
	if h == nil {
		return nil
	}
	out := make([]string, 0, len(h.ProjectIDs))
	for _, pid := range h.ProjectIDs {
		if pid != projectID {
			out = append(out, pid)
		}
	}
	return out
}

// refBatchRow 批量扫描的一行（只取判定需要的列，不加载整行）。
type refBatchRow struct {
	ID                string          `gorm:"column:id"`
	ProjectID         string          `gorm:"column:project_id"`
	SKUCode           string          `gorm:"column:sku_code"`
	BrandID           *string         `gorm:"column:brand_id"`
	PrimaryCategoryID *string         `gorm:"column:primary_category_id"`
	CategoryIDs       json.RawMessage `gorm:"column:category_ids"`
	AttributeIDs      json.RawMessage `gorm:"column:attribute_ids"`
	TagIDs            json.RawMessage `gorm:"column:tag_ids"`
}

// newBatchRefHits 给每个候选 id 预先铺一个空桶 —— 调用方按 id 取值时不必判 nil。
func newBatchRefHits(entityIDs []string, refColumns []string) map[string]*BatchRefHit {
	out := make(map[string]*BatchRefHit, len(entityIDs))
	for _, id := range entityIDs {
		out[id] = &BatchRefHit{RefColumns: refColumns}
	}
	return out
}

// matchCandidateIDs 从行的 jsonb id 数组里挑出落在候选集合里的 id。
//
// 解析失败返回 nil（不 panic）：列是 not null jsonb 且只由本模块写入，形状由写入侧保证；
// 真出现坏值也不该让一次删除请求 500 —— 坏值对应的实体只是「查不出引用」，
// 与「确实没有引用」在拒绝理由上的差别由 service 侧的行存在性检查兜住。
func matchCandidateIDs(raw json.RawMessage, set map[string]bool) []string {
	if len(raw) == 0 {
		return nil
	}
	var ids []string
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}

// eachRefScope 逐工程枚举作用域：每个工程设一次 RLS 作用域后跑 fn，全过程一个事务。
//
// 与单条版 scanProductRefs 的做法一致（一个事务里逐工程 rls.ScopeTx）：分开成每工程一个
// 事务会让同一次扫描看到不同的快照。工程清单为空时不查任何东西（调用方拿到空清单属于
// 「查不出引用」，service 侧必须先失败而不是静默放过）。
func (m *Model) eachRefScope(ctx context.Context, projectIDs []string, fn func(tx *gorm.DB, projectID string) error) error {
	scopes := normalizeProjectIDs(projectIDs)
	if len(scopes) == 0 {
		return nil
	}
	return m.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, pid := range scopes {
			if serr := rls.ScopeTx(tx, pid); serr != nil {
				return serr
			}
			if err := fn(tx, pid); err != nil {
				return err
			}
		}
		return nil
	})
}

// ProductRefsByCategoryIDs 批量反查「这批分类各自被哪些商品引用」。
//
// 两个引用面都查（与单条版 ProductRefsByCategory 一致）：附属分类走 category_ids 的
// jsonb_exists_any（GIN 索引 idx_products_category_ids），主分类走真列 primary_category_id。
func (m *Model) ProductRefsByCategoryIDs(ctx context.Context, categoryIDs, projectIDs []string) (map[string]*BatchRefHit, error) {
	ids := normalizeProjectIDs(categoryIDs)
	hits := newBatchRefHits(ids, []string{"products.category_ids", "products.primary_category_id"})
	if len(ids) == 0 {
		return hits, nil
	}
	set := idSet(ids)
	err := m.eachRefScope(ctx, projectIDs, func(tx *gorm.DB, pid string) error {
		rows := make([]refBatchRow, 0)
		if err := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, sku_code, category_ids, primary_category_id").
			// 真列的实参走 IN ?（与 id IN ? 同一条已被验证的路径）：写成 primary_category_id
			// = ANY(?::text[]) 会让 PG 把参数定成 text[]，而 uuid = text 没有算子 ——
			// 实测报 operator does not exist: uuid = text (SQLSTATE 42883)。
			Where("project_id = ? AND (jsonb_exists_any(category_ids, ?::text[]) OR primary_category_id IN ?)",
				pid, textArrayParam(ids), ids).
			Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			for _, id := range matchCandidateIDs(rows[i].CategoryIDs, set) {
				hits[id].addProject(pid)
			}
			if pc := rows[i].PrimaryCategoryID; pc != nil && set[*pc] {
				hits[*pc].addProject(pid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// ProductRefsByBrandIDs 批量反查「这批品牌各自被哪些商品引用」（products.brand_id 真列）。
func (m *Model) ProductRefsByBrandIDs(ctx context.Context, brandIDs, projectIDs []string) (map[string]*BatchRefHit, error) {
	ids := normalizeProjectIDs(brandIDs)
	hits := newBatchRefHits(ids, []string{"products.brand_id"})
	if len(ids) == 0 {
		return hits, nil
	}
	set := idSet(ids)
	err := m.eachRefScope(ctx, projectIDs, func(tx *gorm.DB, pid string) error {
		rows := make([]refBatchRow, 0)
		if err := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, sku_code, brand_id").
			// 真列实参同 ProductRefsByCategoryIDs：IN ? 让驱动按列型推断参数类型。
			Where("project_id = ? AND brand_id IN ?", pid, ids).
			Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			if b := rows[i].BrandID; b != nil && set[*b] {
				hits[*b].addProject(pid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// ProductRefsByAttributeIDs 批量反查「这批属性组各自被哪些商品引用」（attribute_ids jsonb 数组）。
func (m *Model) ProductRefsByAttributeIDs(ctx context.Context, attributeIDs, projectIDs []string) (map[string]*BatchRefHit, error) {
	ids := normalizeProjectIDs(attributeIDs)
	hits := newBatchRefHits(ids, []string{"products.attribute_ids"})
	if len(ids) == 0 {
		return hits, nil
	}
	set := idSet(ids)
	err := m.eachRefScope(ctx, projectIDs, func(tx *gorm.DB, pid string) error {
		rows := make([]refBatchRow, 0)
		if err := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, sku_code, attribute_ids").
			Where("project_id = ? AND jsonb_exists_any(attribute_ids, ?::text[])", pid, textArrayParam(ids)).
			Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			for _, id := range matchCandidateIDs(rows[i].AttributeIDs, set) {
				hits[id].addProject(pid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// ProductRefsByTagIDs 批量反查「这批标签各自被哪些商品引用」（tag_ids jsonb 数组）。
func (m *Model) ProductRefsByTagIDs(ctx context.Context, tagIDs, projectIDs []string) (map[string]*BatchRefHit, error) {
	ids := normalizeProjectIDs(tagIDs)
	hits := newBatchRefHits(ids, []string{"products.tag_ids"})
	if len(ids) == 0 {
		return hits, nil
	}
	set := idSet(ids)
	err := m.eachRefScope(ctx, projectIDs, func(tx *gorm.DB, pid string) error {
		rows := make([]refBatchRow, 0)
		if err := tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("id, project_id, sku_code, tag_ids").
			Where("project_id = ? AND jsonb_exists_any(tag_ids, ?::text[])", pid, textArrayParam(ids)).
			Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			for _, id := range matchCandidateIDs(rows[i].TagIDs, set) {
				hits[id].addProject(pid)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// idSet []string → 查找集合。
func idSet(ids []string) map[string]bool {
	set := make(map[string]bool, len(ids))
	for _, id := range ids {
		set[id] = true
	}
	return set
}
