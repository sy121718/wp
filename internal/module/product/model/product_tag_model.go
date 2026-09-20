// product_tag_model.go — 商品标签表访问 + 「商品 ↔ 标签」归属读写（issue #11）。
//
// 标签与规则合一张表（081 已建）：kind=manual 的手工标签、kind=rule 的自动标签，
// 规则类型存 rule_type、参数存 rule_params（JSONB）。本文件是「表访问单元」：
// 只做 product_tags 的 CRUD、products.tag_ids 的读写、以及规则求值所需的取数；
// 内置规则类型与参数的合法性判定、重算时机一律在 service（product_tag_rule.go / product_tag.go）。
//
// 归属写在 products.tag_ids（JSONB 数组，081 建了 GIN 索引）：手工标签与自动标签共用同一个
// 数组列，靠「重算只替换自己那一个 tag id」保证两者互不覆盖（见 ReplaceTagProductsTx）。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ProductTagEntity 商品标签（手工 / 自动规则同表）。
type ProductTagEntity struct {
	ID        string `gorm:"column:id;primaryKey"`
	ProjectID string `gorm:"column:project_id;not null"`
	Name      string `gorm:"column:name;not null"`
	Slug      string `gorm:"column:slug;not null"`
	// Kind manual=手工挂载 / rule=按内置规则自动维护。
	Kind string `gorm:"column:kind;not null"`
	// RuleType 内置规则类型（kind=manual 时为空串，091 的 CHECK 约束维持这个形状）。
	RuleType string `gorm:"column:rule_type;not null"`
	// RuleParams 规则参数（JSONB 对象；键集合由规则类型决定）。
	RuleParams json.RawMessage `gorm:"column:rule_params;type:jsonb;not null"`
	// RecalcAt 最近一次按规则重算的时间（手工标签恒为 NULL）。
	RecalcAt  *time.Time      `gorm:"column:recalc_at"`
	Sort      int             `gorm:"column:sort;not null"`
	Metadata  json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ProductTagEntity) TableName() string { return "product_tags" }

// TagDB 标签表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) TagDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductTagEntity{})
}

// Transaction 透传事务（service 编排跨聚合原子写入，如「解绑商品引用 + 删除标签」）。
func (m *Model) Transaction(ctx context.Context, fn func(tx *gorm.DB) error) error {
	return m.db.WithContext(ctx).Transaction(fn)
}

// GetTag 按 ID 查标签（工程内）。
//
// project_id 显式写进谓词，而不是只靠迁移 215 的策略：策略读会话变量 app.project_id，
// 而**超级用户角色会绕过 RLS**（DB-009 至今如此），换角色之前「按 id 单查」实际能读到
// 别的工程的行 —— 表现为「拿别的工程的标签 id 能查出它的名字」。按 id 读是本方法唯一的
// 入口语义，所以隔离必须写在查询里，不能停在策略上。
func (m *Model) GetTag(ctx context.Context, id, projectID string) (e *ProductTagEntity, err error) {
	e = &ProductTagEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductTagEntity{}).
			Where("id = ? AND project_id = ?", id, projectID).First(e).Error
	})
	return e, err
}

// GetTagWithoutScope 按 ID 读行，**不设工程作用域**（审计 DB-009 的显式例外）。
//
// 原唯一调用方 ResolverFor 已改成读 core.BuildProjectID(ctx) 的带作用域入口 ——
// presentation 侧在调用它之前就用 core.WithBuildProjectID 把工程放进了 ctx。
//// 不设工程作用域，**当前没有任何生产调用方**（DB-009 第四批已把调用方改到带作用域的入口）。
//
// 它记录的是「拿不到工程上下文时的那一类入口」的形状：不加空串兜底（那会被 rls 拒掉，
// 把「静默 0 行」换成一个更难懂的错误），也不假装已被隔离。在非超级角色下它 fail closed
// （策略谓词为 NULL ⇒ 0 行 ⇒ ErrRecordNotFound）—— public/test/rls 的
// TestRLS_ProductTaxonomyScope_ExplicitExceptionsUnaffected 把这一形状钉住。
//
// 不要给本方法加新的调用方：需要按 id 读的一律用带 projectID 的那个。

func (m *Model) GetTagWithoutScope(ctx context.Context, id string) (e *ProductTagEntity, err error) {
	e = &ProductTagEntity{}
	err = m.TagDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// TagSlugExists 同工程下 slug 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) TagSlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {
	// 唯一性判定要作用域：缺 scope 时恒「不存在」⇒ 重复创建被静默放行（DB-009）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductTagEntity{}).
			Where("project_id = ? AND slug = ?", projectID, slug)
		if excludeID != "" {
			q = q.Where("id <> ?", excludeID)
		}
		var n int64
		if cerr := q.Count(&n).Error; cerr != nil {
			return cerr
		}
		exists = n > 0
		return nil
	})
	return exists, err
}

// ListTags 工程内标签列表（条件以参数传入；按排序号 + 创建时间稳定排序）。
// kind 为空表示不过滤（后台列表要同时展示手工与自动标签）。
//
// 作用域必填（DB-009 切角色收口）：product_tags 带 FORCE 策略，裸查在非超级角色下静默 0 行 ——
// 标签列表为空、新品/促销规则的取数来源为空（规则不报错，只是永远筛不出命中）。
func (m *Model) ListTags(ctx context.Context, projectID, kind, keyword string) (list []*ProductTagEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductTagEntity{})
		if kind != "" {
			q = q.Where("kind = ?", kind)
		}
		if keyword != "" {
			q = q.Where("name ILIKE ?", "%"+keyword+"%")
		}
		return q.Order("sort ASC, create_time ASC, id ASC").Find(&list).Error
	})
	return list, err
}

// ListTagsByIDs 批量取标签（商品引用校验用，避免 N+1），**必带工程作用域**。
//
// 与 ListCategoriesByIDs 同形：product_tags 在迁移 215 名单里，缺作用域时换连接角色后
// 静默 0 行，标签引用会被误判成「标签不存在」（审计 db-03 §2.5）。
// projectID 必填，空串由 rls 直接拒（ErrInvalidProjectID）。
func (m *Model) ListTagsByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductTagEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductTagEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

// CreateTag 写入标签。
func (m *Model) CreateTag(ctx context.Context, e *ProductTagEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return m.CreateTagTx(ctx, tx, e)
	})
}

// CreateTagTx 复用调用方事务写入标签（product_tags 带策略，调用方须先 rls.ScopeTx）。
//
// service 需要「标签行 + 该标签的归属重算」原子：规则型标签建好即算一次，
// 分开提交时会留下「有标签、没归属」的半截状态（列表里显示命中 0，直到下一次重算）。
func (m *Model) CreateTagTx(ctx context.Context, tx *gorm.DB, e *ProductTagEntity) (err error) {
	return tx.WithContext(ctx).Model(&ProductTagEntity{}).Create(e).Error
}

// UpdateTag 更新标签（整行保存）。
func (m *Model) UpdateTag(ctx context.Context, e *ProductTagEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return m.UpdateTagTx(ctx, tx, e)
	})
}

// UpdateTagTx 复用调用方事务更新标签行（同上：与归属重算 / RecalcAt 同事务）。
func (m *Model) UpdateTagTx(ctx context.Context, tx *gorm.DB, e *ProductTagEntity) (err error) {
	return tx.WithContext(ctx).Model(&ProductTagEntity{}).Where("id = ?", e.ID).Save(e).Error
}

// DeleteTagTx 在给定事务里删除标签行（service 编排：先解绑引用再删）。
func (m *Model) DeleteTagTx(tx *gorm.DB, id string) (err error) {
	return tx.Model(&ProductTagEntity{}).Where("id = ?", id).Delete(&ProductTagEntity{}).Error
}

// ListProductsByTag 反查挂了某标签的商品（后台「某标签命中哪些商品」）。
//
// tag_ids 是 JSONB 数组：用包含谓词命中 GIN 索引；limit <= 0 表示不限条数。
// 按页取用 ListProductsByTagPage（本方法就是它的第一页）。
//
// projectID 由**调用方**给出：反查的是 products（迁移 215 名单），工程上下文只有调用方有。
// 缺作用域时这里静默返回空列表（fail closed 不报错）—— 表现为「标签明明命中商品，
// 列表却是空的」，比报错更难排查。
func (m *Model) ListProductsByTag(ctx context.Context, tagID, projectID string, limit int) (list []*ProductEntity, err error) {
	return m.ListProductsByTagPage(ctx, tagID, projectID, 0, limit)
}

// ListProductsByTagPage 反查挂了某标签的商品，带偏移（后台展开区一页一取）。
//
// offset <= 0 时不写 OFFSET —— PG 对 OFFSET 0 与省略等价，少一段拼接少一处出错。
// project_id 显式写进谓词（不是只靠 RLS）：RLS 在超级用户连接上不生效，
// 「空工程不得拿到其它工程的商品」这条不能只指望策略。
func (m *Model) ListProductsByTagPage(ctx context.Context, tagID, projectID string, offset, limit int) (list []*ProductEntity, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return nil, merr
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("project_id = ? AND tag_ids @> ?::jsonb", projectID, string(probe)).
			Order("sort ASC, create_time ASC, id ASC")
		if limit > 0 {
			q = q.Limit(limit)
		}
		if offset > 0 {
			q = q.Offset(offset)
		}
		return q.Find(&list).Error
	})
	return list, err
}

// CountProductsByTag 某标签命中的商品数（列表页只数不取行）。
//
// projectID 由**调用方**给出：反查的是 products（迁移 215 名单）。缺作用域时恒为 0 ——
// 列表页上每个标签的「命中商品数」会静默全变成 0，而不是报错。
func (m *Model) CountProductsByTag(ctx context.Context, tagID, projectID string) (n int64, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return 0, merr
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("project_id = ? AND tag_ids @> ?::jsonb", projectID, string(probe)).Count(&n).Error
	})
	return n, err
}

// TagHitCount 一个标签的命中数（批量聚合的一行）。
type TagHitCount struct {
	TagID string `gorm:"column:tag_id"`
	Total int64  `gorm:"column:total"`
}

// CountProductsByTagIDs 一次聚合出这批标签各自的命中商品数（PERF-02）。
//
// 为什么必须是批量：列表页的「命中 N 个商品」此前是每个标签发一条 Count ——
// 页面 SQL 条数随标签数线性增长（1000 个标签就是 1000 条查询）。
// 这里用一条语句把「标签集 × 其命中的商品」按 tag_id 分组数出来：
// 未命中的标签不会出现在结果里（调用方按缺省 0 处理）。
//
// 形状上刻意与单条 CountProductsByTag 同源：project_id 显式写进谓词
// （RLS 在超级用户连接上不生效，工程隔离不能只指望策略），包含谓词命中
// products.tag_ids 的 GIN 索引。
//
// 入参 tagIDs 由调用方从本工程的 product_tags 读出，故只会是本工程的值；
// 空列表直接返回空表，不发 SQL（空 IN 是语法错误，也白跑一趟）。
func (m *Model) CountProductsByTagIDs(ctx context.Context, projectID string, tagIDs []string) (out map[string]int64, err error) {
	out = make(map[string]int64, len(tagIDs))
	if projectID == "" || len(tagIDs) == 0 {
		return out, nil
	}
	probe, merr := json.Marshal(tagIDs)
	if merr != nil {
		return nil, merr
	}
	// wanted 把标签 id 列表展开成行（jsonb 数组参数展开，避免动态拼 IN 列表）。
	// 用 LEFT JOIN 而不是 JOIN：INNER JOIN 会把「一个商品都没命中」的标签整个丢掉，
	// 调用方就分不清「0 个商品」与「这次没查到」。
	const q = "SELECT w.tag_id::text AS tag_id, COUNT(p.id) AS total " +
		"FROM (SELECT (jsonb_array_elements_text(?::jsonb))::uuid AS tag_id) w " +
		"LEFT JOIN products p ON p.project_id = ? AND p.tag_ids @> jsonb_build_array(w.tag_id::text) " +
		"GROUP BY w.tag_id"
	var rows []TagHitCount
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Raw(q, string(probe), projectID).Scan(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.TagID] = r.Total
	}
	return out, nil
}

// ReplaceTagProductsTx 把某标签的商品归属整体替换为 productIDs（service 编排的原子组合）。
//
// 两步必须在同一事务里：先摘掉本工程下所有带这个 tag id 的商品，再挂到新命中的商品上。
// 只动这一个 tag id（jsonb 数组的 - 与 || 都是按元素操作），因此**不会碰其它标签** ——
// 这正是「自动标签重算不覆盖手工标签归属」的实现基础。
func (m *Model) ReplaceTagProductsTx(tx *gorm.DB, tagID, projectID string, productIDs []string, now time.Time) (err error) {
	// 这里改的是 products 表（有策略），scope 必须设在调用方事务上：另开事务会看不到
	// 外层刚写的行，而 products 的 UPDATE 在缺 scope 时**匹配 0 行且不报错** ——
	// 表现为「标签关联保存成功但没生效」。
	if serr := rls.ScopeTx(tx, projectID); serr != nil {
		return serr
	}
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return merr
	}
	// 摘：本工程下带这个 tag id 的行全部去掉它。
	if err = tx.Exec(
		"UPDATE products SET tag_ids = tag_ids - ?::text, update_time = ? WHERE project_id = ? AND tag_ids @> ?::jsonb",
		tagID, now, projectID, string(probe)).Error; err != nil {
		return err
	}
	if len(productIDs) == 0 {
		return nil
	}
	// 挂：命中集合一次写出（id 列表走 jsonb 数组参数展开，避免动态拼 SQL）。
	ids, merr := json.Marshal(productIDs)
	if merr != nil {
		return merr
	}
	return tx.Exec(
		"UPDATE products SET tag_ids = tag_ids || ?::jsonb, update_time = ? "+
			"WHERE project_id = ? AND id IN (SELECT (jsonb_array_elements_text(?::jsonb))::uuid)",
		string(probe), now, projectID, string(ids)).Error
}

// ListProductIDsByTagTx 列出本工程下**带该标签**的商品 id（升序）。
//
// 用途（审计 ARCH-01 尾巴）：自动标签归属重算要在替换前取一次旧成员集合，才能算出
// 「本次归属到底变了哪些商品」—— 变化的那些商品详情页要失效，没变的不该被重建。
// 不取旧集合就只能整集合发事件：一个万件商品的规则标签每次重算都会轰出上万条事件。
// tx 必须已设工程作用域（products 带 FORCE 策略，缺作用域会静默 0 行）。
func (m *Model) ListProductIDsByTagTx(tx *gorm.DB, tagID, projectID string) (ids []string, err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return nil, merr
	}
	err = tx.Raw(
		"SELECT id::text FROM products WHERE project_id = ? AND tag_ids @> ?::jsonb ORDER BY id",
		projectID, string(probe)).Scan(&ids).Error
	return ids, err
}

// RemoveTagFromProductsTx 在给定事务里摘掉本工程所有商品上的某标签（删除标签前调用）。
func (m *Model) RemoveTagFromProductsTx(tx *gorm.DB, tagID, projectID string, now time.Time) (err error) {
	probe, merr := json.Marshal([]string{tagID})
	if merr != nil {
		return merr
	}
	q := "UPDATE products SET tag_ids = tag_ids - ?::text, update_time = ? WHERE tag_ids @> ?::jsonb"
	args := []any{tagID, now, string(probe)}
	if projectID != "" {
		q += " AND project_id = ?"
		args = append(args, projectID)
	}
	return tx.Exec(q, args...).Error
}

// ListProductsByIDs 批量取商品行（引用校验、工程过滤用，避免 N+1）。
//
// projectID 由**调用方**给出：products 在迁移 215 名单里，跨工程的行读不到（这是期望行为 ——
// 本方法的两个用途「捆绑引用校验」「按工程过滤 id」都要求只看到本工程的行）。
// 缺作用域时的表现是**返回空列表**：调用方按「请求了哪些 id、拿到了哪些」做差集时，
// 会把本工程存在的商品也判成「已删除」。
func (m *Model) ListProductsByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

// ListProductIDsPublishedSince 取工程内「上架时间在 since 之后」的已发布商品 id
// （新品规则的求值来源；单表查询，条件以参数传入）。
//
// projectID 这里既是过滤条件也是工程作用域：products 在迁移 215 名单里，缺作用域时
// 命中 0 行 ⇒「新品」自动标签重算出来的归属会是空集（把已有归属整体清空），且不报错。
func (m *Model) ListProductIDsPublishedSince(ctx context.Context, projectID, status string, since time.Time) (ids []string, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("project_id = ? AND status = ? AND published_at IS NOT NULL AND published_at >= ?", projectID, status, since).
			Distinct().Pluck("id", &ids).Error
	})
	return ids, err
}

// ListProductIDsByVariantPrice 取「存在启用变体且价格落在区间内」的商品 id。
//
// 变体表没有 project_id，故这里只按价格筛出候选（单表查询），工程归属由 service 兜底过滤。
// min/max 为 nil 表示该方向不限。
func (m *Model) ListProductIDsByVariantPrice(ctx context.Context, min, max *float64) (ids []string, err error) {
	q := m.VariantDB(ctx).Where("enabled = ?", true)
	if min != nil {
		q = q.Where("price >= ?", *min)
	}
	if max != nil {
		q = q.Where("price <= ?", *max)
	}
	err = q.Distinct().Pluck("product_id", &ids).Error
	return ids, err
}

// ListProductIDsWithDiscount 取「存在启用变体且对比价高于售价」的商品 id（促销规则求值来源）。
func (m *Model) ListProductIDsWithDiscount(ctx context.Context) (ids []string, err error) {
	err = m.VariantDB(ctx).
		Where("enabled = ? AND compare_price IS NOT NULL AND compare_price > price", true).
		Distinct().Pluck("product_id", &ids).Error
	return ids, err
}
