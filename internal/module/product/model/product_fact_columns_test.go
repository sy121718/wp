package productmodel_test

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

	if rows, err = productmodel.NewModel(db).ListStatusFactsByIDs(context.Background(), nil, projectID); err != nil || rows != nil {
		t.Fatalf("空清单应直接返回 nil：%v / %+v", err, rows)
	}
	other := uuid.NewString()
	support.SeedProjectRow(t, db, other, "别的工程")

	if rows, err = productmodel.NewModel(db).ListStatusFactsByIDs(context.Background(), []string{productID}, other); err != nil {
		t.Fatalf("跨工程读取不该报错：%v", err)
	} else if len(rows) != 0 {
		t.Fatalf("跨工程不该读到商品（超级角色下也必须为空）：%+v", rows)
	}

	if rows, err = productmodel.NewModel(db).ListStatusFactsByIDs(context.Background(), []string{productID}, projectID); err != nil {
		t.Fatalf("本工程读取失败：%v", err)
	} else if len(rows) != 1 || rows[0].ProjectID != projectID {
		t.Fatalf("本工程应读到 1 行，实际 %+v", rows)
	}
}

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

	if _, err := productmodel.NewModel(db).GetPublicOptionFacts(context.Background(), uuid.NewString(), projectID); err == nil {
		t.Fatal("不存在的商品应返回未命中错误")
	}
}

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
