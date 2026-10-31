package productmodel_test

// product_fact_columns_test.go — 商品行两条窄投影的**列白名单**判据。
//
// 公开变体接口只判「商品是否已发布」，两条变体快照只补「商品名 + 工程」。
// 这三处过去一律走 ListByIDs 全行读取，于是 description / metadata / images 这些
// 大 JSONB 列被搬进访客请求与下单快照进程。响应不打印不等于没搬。
//
// 判据是生成的 SQL 列清单，不是命中行数。

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

func seedFactColumnsProduct(t *testing.T, db *gorm.DB, projectID string) (productID string) {
	t.Helper()
	productID = uuid.NewString()
	if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, description, metadata, images, sort, create_time, update_time)
		VALUES (?, ?, '投影商品', 'fact-columns', 'SKU-FACT', 'published', '{"blocks":[1,2,3]}'::jsonb, '{"k":"v"}'::jsonb, '[]'::jsonb, 1, now(), now())`,
		productID, projectID).Error; err != nil {
		t.Fatalf("夹具备料失败：%v", err)
	}
	return productID
}

// TestProductStatusFactsSkipBigColumns 公开变体接口只判「是否已发布」。
func TestProductStatusFactsSkipBigColumns(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "状态投影")
	productID := seedFactColumnsProduct(t, db, projectID)

	var captured string
	if err := db.Callback().Query().After("gorm:query").Register("product:status-capture",
		func(tx *gorm.DB) {
			if sql := tx.Statement.SQL.String(); strings.Contains(sql, "products") && strings.Contains(sql, "SELECT") {
				captured = sql
			}
		}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}

	rows, err := productmodel.NewModel(db).ListStatusFactsByIDs(context.Background(), []string{productID}, projectID)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	if captured == "" {
		t.Fatal("没抓到读 products 的 SQL（回调没生效）")
	}
	for _, banned := range []string{"description", "metadata", "images", "default_price", "related_ids", "create_time"} {
		if strings.Contains(captured, banned) {
			t.Fatalf("发布状态投影不该读 %q：%s", banned, captured)
		}
	}
	if len(rows) != 1 || rows[0].ID != productID || rows[0].ProjectID != projectID || rows[0].Status != "published" {
		t.Fatalf("状态事实读错：%+v", rows)
	}
	if len(rows[0].Description) != 0 || len(rows[0].Metadata) != 0 {
		t.Fatalf("大字段不该被读出来：%+v", rows[0])
	}

	// 空清单不查库；跨工程点名也读不到。
	if rows, err = productmodel.NewModel(db).ListStatusFactsByIDs(context.Background(), nil, projectID); err != nil || rows != nil {
		t.Fatalf("空清单应直接返回 nil：%v / %+v", err, rows)
	}
	other := uuid.NewString()
	support.SeedProjectRow(t, db, other, "别的工程")
	// 这条隔离必须**不依赖连接角色**：本文件的连接是超级用户（直接绕过 RLS），
	// 所以只包 rls.InProjectScope 的结果在这里就是「读到别的工程的商品」。
	// 隔离来自查询里的 project_id 谓词，策略只是第二层。
	if rows, err = productmodel.NewModel(db).ListStatusFactsByIDs(context.Background(), []string{productID}, other); err != nil {
		t.Fatalf("跨工程读取不该报错：%v", err)
	} else if len(rows) != 0 {
		t.Fatalf("跨工程不该读到商品（超级角色下也必须为空）：%+v", rows)
	}
	// 反向对照：同一行用本工程作用域读得到 —— 证明上面的空不是「夹具没写进去」。
	if rows, err = productmodel.NewModel(db).ListStatusFactsByIDs(context.Background(), []string{productID}, projectID); err != nil {
		t.Fatalf("本工程读取失败：%v", err)
	} else if len(rows) != 1 || rows[0].ProjectID != projectID {
		t.Fatalf("本工程应读到 1 行，实际 %+v", rows)
	}
}

// TestProductNarrowFactsRejectForeignProject 六条窄读在**超级角色**下也只出本工程的行。
//
// 为什么单开一条：上面各条的主断言是「投影不含大字段」，跨工程那一句只是顺带；
// 而这里的判据是隔离本身 —— 连接是超级用户，策略谓词完全不生效（FORCE 只约束表属主），
// 所以唯一起作用的是各方法查询里显式写的 project_id。
// 「只按 id 过滤」时这条在超级角色下立刻红（读得到别的工程的商品），
// 在非超级角色下却是绿的（策略兜住了）—— 正是上面报红的那种假红。
func TestProductNarrowFactsRejectForeignProject(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "本工程")
	other := uuid.NewString()
	support.SeedProjectRow(t, db, other, "别的工程")
	productID := seedFactColumnsProduct(t, db, projectID)
	ctx := context.Background()
	m := productmodel.NewModel(db)

	singles := []struct {
		name string
		read func(projectID string) (*productmodel.ProductEntity, error)
	}{
		{"GetRefFacts", func(pid string) (*productmodel.ProductEntity, error) {
			return m.GetRefFacts(ctx, productID, pid)
		}},
		{"GetSummaryFacts", func(pid string) (*productmodel.ProductEntity, error) {
			return m.GetSummaryFacts(ctx, productID, pid)
		}},
		{"GetPublicOptionFacts", func(pid string) (*productmodel.ProductEntity, error) {
			return m.GetPublicOptionFacts(ctx, productID, pid)
		}},
	}
	for _, s := range singles {
		row, err := s.read(projectID)
		if err != nil || row == nil || row.ProjectID != projectID {
			t.Fatalf("%s 本工程应读得到，实际 %+v / %v", s.name, row, err)
		}
		// 未命中的语义保持不变：仍是 ErrRecordNotFound（调用方据此转「不存在」）。
		if _, err := s.read(other); !errors.Is(err, gorm.ErrRecordNotFound) {
			t.Fatalf("%s 拿别的工程的作用域读这一行应 ErrRecordNotFound，实际 %v", s.name, err)
		}
	}

	batches := []struct {
		name string
		read func(pid string) ([]*productmodel.ProductEntity, error)
	}{
		{"ListStatusFactsByIDs", func(pid string) ([]*productmodel.ProductEntity, error) {
			return m.ListStatusFactsByIDs(ctx, []string{productID}, pid)
		}},
		{"ListNameFactsByIDs", func(pid string) ([]*productmodel.ProductEntity, error) {
			return m.ListNameFactsByIDs(ctx, []string{productID}, pid)
		}},
		{"ListIDFactsByIDs", func(pid string) ([]*productmodel.ProductEntity, error) {
			return m.ListIDFactsByIDs(ctx, []string{productID}, pid)
		}},
	}
	for _, b := range batches {
		rows, err := b.read(projectID)
		if err != nil || len(rows) != 1 || rows[0].ID != productID || rows[0].ProjectID != projectID {
			t.Fatalf("%s 本工程应恰好读到这一行，实际 %+v / %v", b.name, rows, err)
		}
		rows, err = b.read(other)
		if err != nil {
			t.Fatalf("%s 点名外工程 id 不该报错，实际 %v", b.name, err)
		}
		if len(rows) != 0 {
			t.Fatalf("%s 超级角色下也不该读到别的工程的行，实际 %+v", b.name, rows)
		}
	}
}

// TestProductNameFactsSkipBigColumns 变体快照只补「商品名 + 工程」。
func TestProductNameFactsSkipBigColumns(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "名称投影")
	productID := seedFactColumnsProduct(t, db, projectID)

	var captured string
	if err := db.Callback().Query().After("gorm:query").Register("product:name-capture",
		func(tx *gorm.DB) {
			if sql := tx.Statement.SQL.String(); strings.Contains(sql, "products") && strings.Contains(sql, "SELECT") {
				captured = sql
			}
		}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}

	rows, err := productmodel.NewModel(db).ListNameFactsByIDs(context.Background(), []string{productID}, projectID)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	for _, banned := range []string{"description", "metadata", "images", "status", "sort"} {
		if strings.Contains(captured, banned) {
			t.Fatalf("名称投影不该读 %q：%s", banned, captured)
		}
	}
	if len(rows) != 1 || rows[0].Name != "投影商品" || rows[0].ProjectID != projectID {
		t.Fatalf("名称事实读错：%+v", rows)
	}
}

// TestPublicOptionFactsSkipBigColumns 公开规格接口只读五列：id / 工程 / 名 / 状态 / attribute_ids。
// 这条路径曾走 Get 全行读取，于是描述、图集、定价、关联与元数据一并进了访客请求。
func TestPublicOptionFactsSkipBigColumns(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "公开规格投影")
	productID := seedFactColumnsProduct(t, db, projectID)

	var captured string
	if err := db.Callback().Query().After("gorm:query").Register("product:option-capture",
		func(tx *gorm.DB) {
			if sql := tx.Statement.SQL.String(); strings.Contains(sql, "products") && strings.Contains(sql, "SELECT") {
				captured = sql
			}
		}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}

	row, err := productmodel.NewModel(db).GetPublicOptionFacts(context.Background(), productID, projectID)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	for _, banned := range []string{"description", "metadata", "images", "default_price", "related_ids", "tag_ids", "sort"} {
		if strings.Contains(captured, banned) {
			t.Fatalf("公开规格投影不该读 %q：%s", banned, captured)
		}
	}
	if row.ID != productID || row.ProjectID != projectID || row.Name != "投影商品" || row.Status != "published" {
		t.Fatalf("五列事实读错：%+v", row)
	}
	if len(row.Description) != 0 || len(row.Metadata) != 0 {
		t.Fatalf("大字段不该被读出来：%+v", row)
	}

	// 未命中的语义与 Get 一致：ErrRecordNotFound（调用方据此转「不存在」）。
	if _, err := productmodel.NewModel(db).GetPublicOptionFacts(context.Background(), uuid.NewString(), projectID); err == nil {
		t.Fatal("不存在的商品应返回未命中错误")
	}
}

// TestRefFactsSkipBigColumns 评分写入 / 评分列表 / 变体留痕归属反查只要「身份 + 工程」。
func TestRefFactsSkipBigColumns(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "引用投影")
	productID := seedFactColumnsProduct(t, db, projectID)

	var captured string
	if err := db.Callback().Query().After("gorm:query").Register("product:ref-capture",
		func(tx *gorm.DB) {
			if sql := tx.Statement.SQL.String(); strings.Contains(sql, "products") && strings.Contains(sql, "SELECT") {
				captured = sql
			}
		}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}

	row, err := productmodel.NewModel(db).GetRefFacts(context.Background(), productID, projectID)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	for _, banned := range []string{"description", "metadata", "images", "default_price", "name", "slug", "sort"} {
		if strings.Contains(captured, banned) {
			t.Fatalf("引用投影不该读 %q：%s", banned, captured)
		}
	}
	if row.ID != productID || row.ProjectID != projectID {
		t.Fatalf("身份与工程读错：%+v", row)
	}
}

// TestSummaryFactsSkipBigColumns 调价目标行只画商品名与链接。
func TestSummaryFactsSkipBigColumns(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "摘要投影")
	productID := seedFactColumnsProduct(t, db, projectID)

	var captured string
	if err := db.Callback().Query().After("gorm:query").Register("product:summary-capture",
		func(tx *gorm.DB) {
			if sql := tx.Statement.SQL.String(); strings.Contains(sql, "products") && strings.Contains(sql, "SELECT") {
				captured = sql
			}
		}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}

	row, err := productmodel.NewModel(db).GetSummaryFacts(context.Background(), productID, projectID)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	for _, banned := range []string{"description", "metadata", "images", "default_price", "category_ids", "tag_ids"} {
		if strings.Contains(captured, banned) {
			t.Fatalf("摘要投影不该读 %q：%s", banned, captured)
		}
	}
	if row.ID != productID || row.Name != "投影商品" || row.Slug != "fact-columns" {
		t.Fatalf("摘要事实读错：%+v", row)
	}
}

// TestIDFactsSkipContentColumns 关联引用校验与标签规则只判「在不在本工程」。
func TestIDFactsSkipContentColumns(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "身份投影")
	productID := seedFactColumnsProduct(t, db, projectID)

	var captured string
	if err := db.Callback().Query().After("gorm:query").Register("product:id-facts-capture",
		func(tx *gorm.DB) {
			if sql := tx.Statement.SQL.String(); strings.Contains(sql, "products") && strings.Contains(sql, "SELECT") {
				captured = sql
			}
		}); err != nil {
		t.Fatalf("注册 SQL 抓取回调失败：%v", err)
	}

	rows, err := productmodel.NewModel(db).ListIDFactsByIDs(context.Background(), []string{productID}, projectID)
	if err != nil {
		t.Fatalf("查询失败：%v", err)
	}
	for _, banned := range []string{"description", "metadata", "images", "name", "slug", "status", "default_price"} {
		if strings.Contains(captured, banned) {
			t.Fatalf("身份投影不该读 %q：%s", banned, captured)
		}
	}
	if len(rows) != 1 || rows[0].ID != productID || rows[0].ProjectID != projectID || rows[0].Name != "" {
		t.Fatalf("身份事实读错：%+v", rows)
	}
}
