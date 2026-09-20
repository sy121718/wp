// product_brand_model.go — 商品品牌表访问（issue #10）。
//
// 品牌是独立实体（081 已建）：logo、描述、状态位在表上，工程内 slug 唯一。
// 本文件是「表访问单元」：只做 product_brands 的 CRUD，删除前置校验在 service。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ProductBrandEntity 商品品牌。
type ProductBrandEntity struct {
	ID             string          `gorm:"column:id;primaryKey"`
	ProjectID      string          `gorm:"column:project_id;not null"`
	Name           string          `gorm:"column:name;not null"`
	Slug           string          `gorm:"column:slug;not null"`
	Logo           string          `gorm:"column:logo;not null"`
	Description    string          `gorm:"column:description;not null"`
	SEOTitle       string          `gorm:"column:seo_title;not null"`
	SEODescription string          `gorm:"column:seo_description;not null"`
	Sort           int             `gorm:"column:sort;not null"`
	Metadata       json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt      time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt      time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ProductBrandEntity) TableName() string { return "product_brands" }

// BrandDB 品牌表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) BrandDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductBrandEntity{})
}

// GetBrand 按 ID 查品牌。
//
// projectID 由调用方给出：product_brands 在迁移 215 名单里，跨工程的行不可见。
func (m *Model) GetBrand(ctx context.Context, id, projectID string) (e *ProductBrandEntity, err error) {
	e = &ProductBrandEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// GetBrandWithoutScope 按 ID 读行，**不设工程作用域**（审计 DB-009 的显式例外）。
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

func (m *Model) GetBrandWithoutScope(ctx context.Context, id string) (e *ProductBrandEntity, err error) {
	e = &ProductBrandEntity{}
	err = m.BrandDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// BrandSlugExists 同工程下 slug 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) BrandSlugExists(ctx context.Context, projectID, slug, excludeID string) (exists bool, err error) {
	// 唯一性判定要作用域：缺 scope 时恒「不存在」⇒ 重复创建被静默放行（DB-009）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductBrandEntity{}).
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

// ListBrands 工程内品牌列表（条件以参数传入，按排序号 + 创建时间稳定排序）。
//
// 作用域必填（DB-009 切角色收口）：product_brands 带 FORCE 策略，WHERE project_id 只是普通过滤，
// 裸查时非超级角色静默 0 行（后台品牌列表整页空白、构建期筛选栏没有品牌选项），不报任何错。
func (m *Model) ListBrands(ctx context.Context, projectID, keyword string) (list []*ProductBrandEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductBrandEntity{})
		if keyword != "" {
			q = q.Where("name ILIKE ?", "%"+keyword+"%")
		}
		return q.Order("sort ASC, create_time ASC, id ASC").Find(&list).Error
	})
	return list, err
}

// ListBrandsByIDs 按 id 批量取品牌（集合源/构建期一次取好，零 N+1；顺序由调用方定），
// **必带工程作用域**。
//
// 唯一调用方是集合源的 taxonomyIndex，工程取自过滤条件（f.ProjectID）；
// product_brands 在迁移 215 名单里，缺作用域时换非超级角色后品牌名整列为空
// （静默 0 行，审计 db-03 §2.5）。projectID 必填，空串由 rls 直接拒。
func (m *Model) ListBrandsByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductBrandEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

// CreateBrand 写入品牌。
func (m *Model) CreateBrand(ctx context.Context, e *ProductBrandEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductBrandEntity{}).Create(e).Error
	})
}

// UpdateBrand 更新品牌（整行保存）。
func (m *Model) UpdateBrand(ctx context.Context, e *ProductBrandEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductBrandEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

// DeleteBrand 删除品牌。products.brand_id 有外键 ON DELETE SET NULL，
// 但「被商品引用即拒绝删除」是业务规则，判定在 service。
//
// 作用域必填（DB-009 切角色收口）：DELETE 在裸查下不报错、也不删（匹配 0 行）——
// 运营会看到「点了删除、提示成功、品牌还在」，而日志里什么都没有。
// projectID 由调用方给出（发起删除的那个工程）。
func (m *Model) DeleteBrand(ctx context.Context, id, projectID string) (err error) {
	return rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id = ?", id).Delete(&ProductBrandEntity{}).Error
	})
}

// CreateBrandTx / UpdateBrandTx / DeleteBrandTx — 复用**调用方已开启的事务**
// （审计 ARCH-01：品牌行与静态产物失效事件必须原子，理由同分类侧）。
func (m *Model) CreateBrandTx(ctx context.Context, tx *gorm.DB, e *ProductBrandEntity) error {
	return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Create(e).Error
}

func (m *Model) UpdateBrandTx(ctx context.Context, tx *gorm.DB, e *ProductBrandEntity) error {
	return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) DeleteBrandTx(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Model(&ProductBrandEntity{}).Where("id = ?", id).Delete(&ProductBrandEntity{}).Error
}

// ProductUsingBrand **本工程内**反查挂了某品牌的商品（删除前引用检查，只取一行用于拦截提示）。
//
// ⚠ 品牌删除守卫**已不再用它**（审计 DB-03 §5.1 第 2 条 / PROD-02）：作用域是发起删除的
// 那个工程，别的工程仍引用这个品牌时命中 0 行 ⇒ 删除放行 ⇒ products.brand_id 的外键
// ON DELETE SET NULL 静默解绑。守卫走 ProductRefsByBrand（跨工程，见 product_ref_scan.go）。
// 保留本方法供 public/test/rls/rls_product_taxonomy_scope_test.go 钉住工程作用域护栏，
// **不要再把它接回删除路径。**
func (m *Model) ProductUsingBrand(ctx context.Context, brandID, projectID string) (e *ProductEntity, err error) {
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).Where("brand_id = ?", brandID).
			Order("sort ASC, create_time ASC").Limit(1).Take(e).Error
	})
	return e, err
}
