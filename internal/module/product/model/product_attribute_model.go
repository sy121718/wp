// product_attribute_model.go — 商品属性组表访问（issue #7）。
//
// 属性组与属性值合一张表（group + values JSONB，081 已定），故本文件仍是
// 「表访问单元」：只做 product_attributes 的 CRUD，不做 key 派生、默认值、
// 引用校验等业务判断（那些在 service）。
package productmodel

import (
	"context"
	"encoding/json"
	"time"

	"gorm.io/gorm"

	"go_wp/pkg/rls"
)

// ProductAttributeEntity 属性组（一行一组；值以 JSONB 数组承载）。
//
// Values 沿用 081 的 jsonb 列：本票把元素结构固定为 [{id,key,label,sort,enabled}]，
// 历史纯字符串数组由 service 读取时兜底归一（见 attribute_values.go）。
type ProductAttributeEntity struct {
	ID          string          `gorm:"column:id;primaryKey"`
	ProjectID   string          `gorm:"column:project_id;not null"`
	Key         string          `gorm:"column:key;not null"`
	Name        string          `gorm:"column:name;not null"`
	IsVariation bool            `gorm:"column:is_variation;not null"`
	Sort        int             `gorm:"column:sort;not null"`
	Values      json.RawMessage `gorm:"column:values;type:jsonb;not null"`
	Metadata    json.RawMessage `gorm:"column:metadata;type:jsonb;not null"`
	CreatedAt   time.Time       `gorm:"column:create_time;not null"`
	UpdatedAt   time.Time       `gorm:"column:update_time;not null"`
}

// TableName 实现 gorm 表名。
func (ProductAttributeEntity) TableName() string { return "product_attributes" }

// AttributeDB 属性表句柄（内部实现细节，只允许被本 model 的仓储方法消费）。
func (m *Model) AttributeDB(ctx context.Context) *gorm.DB {
	return m.db.WithContext(ctx).Model(&ProductAttributeEntity{})
}

// GetAttribute 按 ID 查属性组。
//
// projectID 由调用方给出：product_attributes 在迁移 215 名单里，跨工程的行不可见。
func (m *Model) GetAttribute(ctx context.Context, id, projectID string) (e *ProductAttributeEntity, err error) {
	e = &ProductAttributeEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id = ?", id).First(e).Error
	})
	return e, err
}

// GetAttributeWithoutScope 按 ID 读行，**不设工程作用域**（审计 DB-009 的显式例外）。
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

func (m *Model) GetAttributeWithoutScope(ctx context.Context, id string) (e *ProductAttributeEntity, err error) {
	e = &ProductAttributeEntity{}
	err = m.AttributeDB(ctx).Where("id = ?", id).First(e).Error
	return e, err
}

// AttributeKeyExists 同工程下属性组 key 是否被占用（excludeID 为空表示新建场景）。
func (m *Model) AttributeKeyExists(ctx context.Context, projectID, key, excludeID string) (exists bool, err error) {
	// 唯一性判定同样要作用域：缺 scope 时这里恒为「不存在」⇒ 重复创建被静默放行（DB-009）。
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductAttributeEntity{}).
			Where("project_id = ? AND key = ?", projectID, key)
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
	if err != nil {
		return false, err
	}
	return exists, nil
}

// ListAttributes 属性组列表（分页 + 可选过滤；variation 为 nil 表示不过滤）。
//
// 作用域必填（DB-009 切角色收口）：product_attributes 带 FORCE 策略，裸查在非超级角色下
// 静默 0 行 —— 属性组列表为空、变体生成没有维度可选。与 CountAttributes 同一把作用域，
// 「列表有 N 条、总数是 0」这类自相矛盾的组合不会再出现。
func (m *Model) ListAttributes(ctx context.Context, projectID, keyword string, variation *bool, limit, offset int) (list []*ProductAttributeEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductAttributeEntity{})
		if keyword != "" {
			q = q.Where("name ILIKE ? OR key ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
		}
		if variation != nil {
			q = q.Where("is_variation = ?", *variation)
		}
		return q.Order("sort ASC, create_time ASC").Limit(limit).Offset(offset).Find(&list).Error
	})
	return list, err
}

// CountAttributes 属性组总数（与 ListAttributes 同过滤条件）。
func (m *Model) CountAttributes(ctx context.Context, projectID, keyword string, variation *bool) (n int64, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		q := tx.WithContext(ctx).Model(&ProductAttributeEntity{})
		if keyword != "" {
			q = q.Where("name ILIKE ? OR key ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
		}
		if variation != nil {
			q = q.Where("is_variation = ?", *variation)
		}
		return q.Count(&n).Error
	})
	return n, err
}

// ListAttributesByIDs 批量取属性组（商品引用校验与构建期批量解析用，避免 N+1），
// **必带工程作用域**。
//
// product_attributes 在迁移 215 名单里：缺作用域时换连接角色后属性组整批读空，
// 变体生成的维度归一与商品详情的内联属性都会静默退化成「没有属性」
// （审计 db-03 §2.5）。projectID 必填，空串由 rls 直接拒。
func (m *Model) ListAttributesByIDs(ctx context.Context, ids []string, projectID string) (list []*ProductAttributeEntity, err error) {
	if len(ids) == 0 {
		return nil, nil
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id IN ?", ids).Find(&list).Error
	})
	return list, err
}

// ListAttributesByProject 某工程全部属性组（按排序）。
//
// 作用域必填（DB-009 切角色收口）：同 ListAttributes —— 裸查在非超级角色下静默 0 行，
// 构建期集合筛选栏会少了整个「属性」维度（页面照常渲染，只是筛选条少一截）。
func (m *Model) ListAttributesByProject(ctx context.Context, projectID string) (list []*ProductAttributeEntity, err error) {
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).
			Order("sort ASC, create_time ASC").Find(&list).Error
	})
	return list, err
}

// CreateAttribute 写入属性组。
func (m *Model) CreateAttribute(ctx context.Context, e *ProductAttributeEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductAttributeEntity{}).Create(e).Error
	})
}

// UpdateAttribute 更新属性组（整行保存）。
func (m *Model) UpdateAttribute(ctx context.Context, e *ProductAttributeEntity) (err error) {
	return rls.InProjectScope(ctx, m.db, e.ProjectID, func(tx *gorm.DB) error {
		return tx.Model(&ProductAttributeEntity{}).Where("id = ?", e.ID).Save(e).Error
	})
}

// DeleteAttribute 删除属性组。
func (m *Model) DeleteAttribute(ctx context.Context, id string) (err error) {
	return m.AttributeDB(ctx).Where("id = ?", id).Delete(&ProductAttributeEntity{}).Error
}

// CreateAttributeTx / UpdateAttributeTx / DeleteAttributeTx — 复用**调用方已开启的事务**。
//
// 为什么需要它们（审计 ARCH-01 尾巴）：属性组变更要在同一事务里追加一条静态产物失效
// 事件（product_outbox_events）。属性行与事件行分属两次写入，不在一个事务里就会留下
// 「属性改了、事件没写」（站点停在旧规格维度）或反过来的半截状态 —— 两者都是静默的。
// tx 必须已由调用方设好工程作用域（rls.ScopeTx），model 不再另开事务。
func (m *Model) CreateAttributeTx(ctx context.Context, tx *gorm.DB, e *ProductAttributeEntity) error {
	return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Create(e).Error
}

func (m *Model) UpdateAttributeTx(ctx context.Context, tx *gorm.DB, e *ProductAttributeEntity) error {
	return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id = ?", e.ID).Save(e).Error
}

func (m *Model) DeleteAttributeTx(ctx context.Context, tx *gorm.DB, id string) error {
	return tx.WithContext(ctx).Model(&ProductAttributeEntity{}).Where("id = ?", id).Delete(&ProductAttributeEntity{}).Error
}

// ListProductAttributeIDs 某商品引用的属性组 id 数组（products.attribute_ids）。
//
// 用于「删除属性组前的引用检查」与「商品详情回显」：只读一列，不整行加载。
//
// productID 是**商品** id，projectID 才是工程作用域 —— 两者不可混用：products 在迁移 215
// 名单里，缺作用域的读在非超级角色下静默返回 0 行（fail closed 不报错）。
//
// 当前**没有调用方**：商品详情回显直接读行上已有的 attribute_ids 列（product_resp.go），
// 删除前引用检查走 ProductUsingAttribute 的 jsonb 反查。保留它是因为「按商品取引用」
// 这个形状迟早要用；签名先按 DB-009 的口径带上作用域，免得将来有人顺手拿它做跨工程读。
func (m *Model) ListProductAttributeIDs(ctx context.Context, productID, projectID string) (raw json.RawMessage, err error) {
	var row struct {
		AttributeIDs json.RawMessage `gorm:"column:attribute_ids"`
	}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Select("attribute_ids").Where("id = ?", productID).Take(&row).Error
	})
	if err != nil {
		return nil, err
	}
	return row.AttributeIDs, nil
}

// ProductUsingAttribute **本工程内**反查引用了某属性组的商品（只取一行）。
//
// 以 jsonb 包含谓词查询：attribute_ids 是 id 数组，@> 命中即被引用。
//
// ⚠ 属性组删除守卫**已不再用它**（审计 DB-03 §5.1 第 2 条 / PROD-02）：作用域是发起删除的
// 那个工程，别的工程仍引用该属性组时命中 0 行 ⇒ 删除放行 ⇒ 永久悬空 id。
// 守卫走 ProductRefsByAttribute（跨工程，见 product_ref_scan.go）。
// 保留本方法供 public/test/rls/rls_product_taxonomy_scope_test.go 钉住工程作用域护栏，
// **不要再把它接回删除路径。**
func (m *Model) ProductUsingAttribute(ctx context.Context, attributeID, projectID string) (e *ProductEntity, err error) {
	probe, merr := json.Marshal([]string{attributeID})
	if merr != nil {
		return nil, gorm.ErrRecordNotFound
	}
	e = &ProductEntity{}
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductEntity{}).
			Where("attribute_ids @> ?::jsonb", string(probe)).
			Order("sort ASC, create_time ASC").Limit(1).Take(e).Error
	})
	return e, err
}
