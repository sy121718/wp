// product_category_model.go — 商品分类表访问（issue #10）。
//
// 分类是树形自引用表（081 已建）：父子层级、工程内 slug 唯一、排序与 SEO 字段都在表上。
// 本文件仍是「表访问单元」：只做 product_categories 的 CRUD，不判环、不派生 slug、
// 不做删除前置校验（那些在 service）。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ProductCategoryEntity 商品分类（树形自引用；parent_id 为空即顶级）。
type ProductCategoryEntity struct {
	ID             string          `gorm:"column:id;primaryKey"`
	ProjectID      string          `gorm:"column:project_id;not null"`
	ParentID       *string         `gorm:"column:parent_id"`
	Name           string          `gorm:"column:name;not null"`
	Slug           string          `gorm:"column:slug;not null"`
	Description    string          `gorm:"column:description;not null"`
	Image          string          `gorm:"column:image;not null"`
	SEOTitle       string          `gorm:"column:seo_title;not null"`
	SEODescription string          `gorm:"column:seo_description;not null"`
	Sort           int             `gorm:"column:sort;not null"`
	Metadata       json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt      time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt      time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ProductCategoryEntity) TableName() string { return "product_categories" }

// CategoryDB 分类表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) CategoryDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductCategoryEntity{})
}

// GetCategory 按 ID 查分类。
//
// projectID 由**调用方**给出（不从行里读回来）：product_categories 在迁移 215 名单里，
// 跨工程的分类在策略下不可见，读不到即 ErrRecordNotFound —— 隔离生效后的期望结果。
func (m *Model) GetCategory(ctx context.Context, id, projectID string) (e *ProductCategoryEntity, err error) {
	e = &ProductCategoryEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// GetCategoryWithoutScope 按 ID 读行，**不设工程作用域**（审计 DB-009 的显式例外）。
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

func (m *Model) GetCategoryWithoutScope(ctx context.Context, id string) (e *ProductCategoryEntity, err error) {
	e = &ProductCategoryEntity{}
	err = m.CategoryDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// CategorySlugExists 同工程下 slug 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) CategorySlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {
	// 唯一性判定要作用域：缺 scope 时恒「不存在」⇒ 重复创建被静默放行（DB-009）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
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

// ListCategories 工程内分类列表（条件以参数传入；同级按排序号 + 创建时间稳定排序）。
//
// 返回的是**扁平**列表：树的组装（父子挂接与环数据兜底）在 service，model 只负责读。
//
// 作用域必填（DB-009 切角色收口）：product_categories 带 FORCE 策略，**WHERE project_id 只是
// 普通过滤**，不设 app.project_id 时策略谓词为 NULL ⇒ 静默 0 行。这条曾经就是裸查 ——
// 换 go_wp_app 实测「GET /api/product/category/list?projectId=有数据的工程」返回空数组，
// 而库里那一行确实存在（后台分类列表整页空白、且没有任何错误日志）。
// 空串 / 非 uuid 由 rls 在入口拒掉，不退化成一个更难排查的形态。
func (m *Model) ListCategories(ctx context.Context, projectID, keyword string) (list []*ProductCategoryEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductCategoryEntity{})
		if keyword != "" {
			q = q.Where("name ILIKE ?", "%"+keyword+"%")
		}
		return q.Order("sort ASC, create_time ASC, id ASC").Find(&list).Error
	})
	return list, err
}

// ListCategoriesByIDs 批量取分类（商品引用校验与反查用，避免 N+1），**必带工程作用域**。
//
// projectID 是**必填**的工程作用域（不是可选过滤条件）：product_categories 在迁移 215
// 名单里，策略谓词读会话变量 app.project_id，而 WHERE id IN (...) 只是普通过滤 ——
// 换连接角色后**没有作用域的查询会静默返回 0 行**（fail closed 不报错），
// 调用方会把它读成「这些分类都不存在」。空串或非 uuid 会被 rls 直接拒掉
// （rls.ErrInvalidProjectID），不退化成一个更难排查的形态。
//
// **绝不能退回 m.CategoryDB(ctx)**：那会另取一条连接、脱离事务，策略谓词读到的仍是
// NULL，查询静默返回 0 行（与 ListByIDs / ListForCollection 同一条禁令）。
//
// 语义后果（审计 db-03 §2.5 要求补 scope 时已记录）：跨工程的 id 在这里「读不到」，
// 校验路径上会从「工程不匹配」退化成「不存在」—— 可见性由策略决定，不由调用方的
// row.ProjectID != projectID 判断决定，那一条现在是第二道防线而不是唯一防线。
func (m *Model) ListCategoriesByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductCategoryEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

// CreateCategory 写入分类。
func (m *Model) CreateCategory(ctx context.Context, e *ProductCategoryEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductCategoryEntity{}).Create(e).Error
	})
}

// UpdateCategory 更新分类（整行保存；ParentID 为 nil 时写 NULL = 提升为顶级）。
func (m *Model) UpdateCategory(ctx context.Context, e *ProductCategoryEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductCategoryEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

// DeleteCategory 删除分类。子级由外键 ON DELETE SET NULL 兜底提升为顶级，
// 但「有子级即拒绝删除」是业务规则，判定在 service。
func (m *Model) DeleteCategory(ctx context.Context, id string) (err error) {
	return m.CategoryDB(ctx).Where("id = ?", id).Delete(&ProductCategoryEntity{}).Error
}

// CreateCategoryTx / UpdateCategoryTx / DeleteCategoryTx — 复用**调用方已开启的事务**。
//
// 为什么需要它们（审计 ARCH-01）：分类变更现在要在同一个事务里追加一条静态产物失效
// 事件（product_outbox_events）。分类行与事件行分属两次写入，不在一个事务里就会留下
// 「分类改了、事件没写」或反过来的半截状态 —— 而这两种半截状态都是静默的。
// tx 必须已由调用方设好工程作用域（rls.ScopeTx），model 不再另开事务。
func (m *Model) CreateCategoryTx(ctx context.Context, tx *gorm.DB, e *ProductCategoryEntity) error {
	return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Create(e).Error
}

func (m *Model) UpdateCategoryTx(ctx context.Context, tx *gorm.DB, e *ProductCategoryEntity) error {
	return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) DeleteCategoryTx(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("id = ?", id).Delete(&ProductCategoryEntity{}).Error
}

// CountCategoryChildren 直接子级数量（删除前置校验）。
//
// 作用域必填（DB-009 切角色收口）：裸查时非超级角色恒返回 0，于是「仍有子级即拒绝删除」
// 这条守卫静默放行 —— 分类树上会出现一批被外键 SET NULL 提升成顶级的孤儿（层级信息丢失，
// 且没有任何错误）。调用方（DeleteCategory）手里就有工程 id，顺手传进来即可。
func (m *Model) CountCategoryChildren(ctx context.Context, parentID, projectID string) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).Where("parent_id = ?", parentID).Count(&n).Error
	})
	return n, err
}

// ProductUsingCategory **本工程内**反查挂了某分类的商品（只取一行）。
//
// 两个引用面都查：附属分类走 category_ids 的 jsonb 包含谓词，主分类走真列。
//
// ⚠ 分类删除守卫**已不再用它**（审计 DB-03 §5.1 第 2 条 / PROD-02）：它的作用域是
// 发起删除的那个工程，别的工程仍引用这条分类时命中 0 行 ⇒ 删除放行 ⇒ 跨工程悬空 id。
// 守卫走 ProductRefsByCategory（跨工程，见 product_ref_scan.go）。
// 保留本方法是因为 public/test/rls/rls_product_taxonomy_scope_test.go 用它钉住
// 「ListXxxByIDs / ProductUsingXxx 这一族的工程作用域」这条 DB-009/DB-05 护栏，
// 不是留给删除守卫用的。**不要再把它接回删除路径。**
func (m *Model) ProductUsingCategory(ctx context.Context, categoryID, projectID string) (e *ProductEntity, err error) {
	probe, merr := json.Marshal([]string{categoryID})
	if merr != nil {
		return nil, gorm.ErrRecordNotFound
	}
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("category_ids @> ?::jsonb OR primary_category_id = ?", string(probe), categoryID).
			Order("sort ASC, create_time ASC").Limit(1).Take(e).Error
	})
	return e, err
}
