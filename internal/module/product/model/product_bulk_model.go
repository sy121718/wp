package productmodel

// product_bulk_model.go — 批量写用例的 model 侧支持（task-156，dd.txt §5.4）。
//
// 这一批方法只做三件事：
//   ① 批量删除：一条 DELETE ... WHERE id IN (...)，受影响行数回给 service 对账；
//   ② 批量前置读取：一次查回整批（哪些存在 / 哪些还有子级），替代逐条 Get + Count；
//   ③ 批量解绑：标签删除时一条 UPDATE 把整批 id 从 products.tag_ids 里减去。
//
// 事务边界一律由 service 决定 —— Tx 变体只接受**调用方已开启**、且已由调用方设好工程
// 作用域的句柄（与 DeleteXxxTx 同一口径）。这里的方法不自己开事务：一旦自己开，
// service 就没法把「删除 + 失效事件 + 留痕」放进同一个事务 —— 那正是本批要保证的事
// （AGENTS.md「写操作的事务与回滚」）。

import (
	"context"
	"database/sql/driver"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"go_wp/pkg/rls"
)

// textArrayParam 是 PG text[] 的**入参**形态（只写不读，故只实现 driver.Valuer）。
//
// 为什么不用裸 []string：GORM 经 pgx 把 []string 编码成**元组**字面量 ('a','b')，PG 报
// malformed array literal (22P02) —— 与 mail 模块 StringArray 记的是同一条实测（那边还试过
// pgtype.FlatArray[string]：GORM 走 database/sql 路径时不被识别，仍编码成 record，报 42804）。
// 所以自己产出 PG 数组字面量文本，交由 SQL 里的 ::text[] 把参数解析成数组。
type textArrayParam []string

// Value 实现 driver.Valuer：产出 PG 数组字面量文本（{"a","b"}）。
func (a textArrayParam) Value() (driver.Value, error) {
	return encodePGTextArray(a), nil
}

// encodePGTextArray []string → PG 数组字面量。
//
// 元素用 strconv.Quote：它产出的转义规则（双引号包裹、内部 " 与 \ 转义）正是 PG 数组元素的
// 规则。本函数的输入恒为 uuid（内部 id），不含需要 \uXXXX 转义的非 ASCII 字符。
func encodePGTextArray(ids []string) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, id := range ids {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.Quote(id))
	}
	b.WriteByte('}')
	return b.String()
}

// LockProductsTx 按 id **升序**锁定目标商品行，返回实际存在于该工程的 id（升序）。
//
// 为什么必须有这一步：批量删除是「先批量校验、再一条 DELETE」—— 校验与删除之间若没有行锁，
// 行可以被并发请求改掉（甚至删掉），于是「校验通过」与「真的删掉了」之间出现窗口。
// AGENTS.md 要求读-改-写必须有行锁，且按标识升序加锁避免死锁。
//
// project_id 谓词不能省：返回的 id 就是删除集合，只按 id 过滤会把**别的工程**的同名 id
// （请求里塞进来的跨工程 id）一起锁住并删掉 —— 实测过一次，测试里 4 项请求删掉 3 项，
// 跨工程那项被算成 Succeeded。tx 里的 RLS 作用域是防线之一，但它只在非超级角色下生效，
// 不能当唯一防线。
func (m *Model) LockProductsTx(ctx context.Context, tx *gorm.DB, ids []string, projectID string) (locked []string, err error) {
	if tx == nil || len(ids) == 0 || projectID == "" {
		return nil, nil
	}
	err = tx.WithContext(ctx).Model(&ProductEntity{}).
		Where("id IN ? AND project_id = ?", ids, projectID).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		Order("id ASC").
		Pluck("id", &locked).Error
	return locked, err
}

// DeleteProductsTx 批量删除商品行（变体、各仓库存行由外键级联），返回受影响行数。
//
// 返回行数是给 service 对账的：锁住的 id 与真正删掉的行数不一致，说明这批里有行在锁之后
// 消失了 —— 那是异常，service 据此回滚，而不是把差异静默吞掉。
func (m *Model) DeleteProductsTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductEntity{}, ids)
}

// DeleteCategoriesTx 批量删除分类行（子级由外键 ON DELETE SET NULL 兜底；
// 「有子级即拒绝」是业务规则，判定在 service）。
func (m *Model) DeleteCategoriesTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductCategoryEntity{}, ids)
}

// DeleteBrandsTx 批量删除品牌行（products.brand_id 的外键 ON DELETE SET NULL 只是兜底；
// 「被商品引用即拒绝」的判定在 service）。
func (m *Model) DeleteBrandsTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductBrandEntity{}, ids)
}

// DeleteTagsTx 批量删除标签行（service 在同一事务里先批量解绑商品引用再删）。
func (m *Model) DeleteTagsTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductTagEntity{}, ids)
}

// DeleteAttributesTx 批量删除属性组行（属性值由外键级联；引用的悬空守卫在 service）。
func (m *Model) DeleteAttributesTx(ctx context.Context, tx *gorm.DB, ids []string) (n int64, err error) {
	return m.deleteByIDsTx(ctx, tx, &ProductAttributeEntity{}, ids)
}

// deleteByIDsTx 五个批量删除的唯一实现（一条 DELETE，回受影响行数）。
func (m *Model) deleteByIDsTx(ctx context.Context, tx *gorm.DB, entity any, ids []string) (n int64, err error) {
	if tx == nil || len(ids) == 0 {
		return 0, nil
	}
	res := tx.WithContext(ctx).Model(entity).Where("id IN ?", ids).Delete(entity)
	return res.RowsAffected, res.Error
}

// CountCategoryChildrenByIDs 一次查回「这批分类里哪些还有子级」（父 id → 直接子级数）。
//
// 现状是逐条 CountCategoryChildren（N 条 COUNT）；批量版一条 GROUP BY。只返回有子级的那些
// 父 id —— 没出现的即 0，调用方不必先铺零值。
//
// 作用域必填（DB-009 切角色收口）：裸查在非超级角色下恒返回 0 行，于是「仍有子级即拒绝删除」
// 这条守卫会静默放行（分类树上的子级会被外键 SET NULL 提升成顶级，层级信息丢失）。
func (m *Model) CountCategoryChildrenByIDs(ctx context.Context, parentIDs []string, projectID string) (counts map[string]int64, err error) {
	counts = map[string]int64{}
	if len(parentIDs) == 0 {
		return counts, nil
	}
	type childCountRow struct {
		ParentID string `gorm:"column:parent_id"`
		N        int64  `gorm:"column:n"`
	}
	rows := make([]childCountRow, 0, len(parentIDs))
	err = rls.InProjectScope(ctx, m.db, projectID, func(tx *gorm.DB) error {
		return tx.WithContext(ctx).Model(&ProductCategoryEntity{}).
			Select("parent_id, COUNT(*) AS n").
			Where("parent_id IN ?", parentIDs).
			Group("parent_id").
			Find(&rows).Error
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		counts[r.ParentID] = r.N
	}
	return counts, nil
}

// RemoveTagsFromProductsTx 批量解绑：一条 UPDATE 把整批标签 id 从 products.tag_ids 里减去。
//
// 与单条版（RemoveTagFromProductsTx）语义完全相同：只作用在 projectID 那个工程，
// 用 jsonb 的减号算子摘元素、只碰 tag_ids 与 update_time 两列。差别只在 SQL 条数：
// 批量版 1 条，单条版每个标签 1 条。
//
// projectID 为空时不做工程谓词（与单条版一致：那是「调用方已自行限定」的形态）。
func (m *Model) RemoveTagsFromProductsTx(tx *gorm.DB, tagIDs []string, projectID string, now time.Time) (n int64, err error) {
	if tx == nil || len(tagIDs) == 0 {
		return 0, nil
	}
	upd := tx.Model(&ProductEntity{}).
		Where("jsonb_exists_any(tag_ids, ?::text[])", textArrayParam(tagIDs))
	if projectID != "" {
		upd = upd.Where("project_id = ?", projectID)
	}
	res := upd.UpdateColumns(map[string]any{
		"tag_ids":     gorm.Expr("tag_ids - ?::text[]", textArrayParam(tagIDs)),
		"update_time": now,
	})
	return res.RowsAffected, res.Error
}
