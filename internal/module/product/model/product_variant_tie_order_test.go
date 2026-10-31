package productmodel_test

// product_variant_tie_order_test.go — 变体顺序的唯一尾键（PRD-006 同源，后果更重）。
//
// 变体顺序**会进构建产物**：集合源把 variants 序列化成 JSON，商品卡的 SKU 取
// variants[0].SKUCode。sort 与 create_time 都相同时，没有唯一尾键的 ORDER BY 由物理
// 读序决定 —— 同一份数据两次构建可能给出不同的首个 SKU（不变量 5：确定性构建）。
//
// 夹具故意**反序插入**：先插 id 大的，再插 id 小的，且 sort / create_time 完全相同。
// 依赖物理顺序的实现会先返回后插的那行，于是断言「按 id 升序」直接变红。

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

func TestVariantReadersOrderByUniqueTailKey(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "变体同序")

	productID := uuid.NewString()
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, create_time, update_time)
		VALUES (?, ?, '同序商品', 'tie-variant', 'SKU-TIE', 'published', 1, ?, ?)`,
		productID, projectID, at, at).Error; err != nil {
		t.Fatalf("夹具备料失败：%v", err)
	}
	want := make([]string, 0, 6)
	for i := 6; i >= 1; i-- {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
		want = append([]string{id}, want...)
		if err := db.Exec(`INSERT INTO product_variants (id, product_id, sku_code, price, enabled, sort, create_time, update_time)
			VALUES (?, ?, ?, 10, true, 0, ?, ?)`, id, productID, fmt.Sprintf("SKU-TIE-%d", i), at, at).Error; err != nil {
			t.Fatalf("夹具备料失败：%v", err)
		}
	}

	m := productmodel.NewModel(db)
	ctx := context.Background()
	check := func(name string, got []*productmodel.VariantEntity) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s 行数=%d，期望 %d", name, len(got), len(want))
		}
		for i, row := range got {
			if row.ID != want[i] {
				t.Fatalf("%s 第 %d 行=%s，期望 %s（同 sort、同时刻时必须按 id 决定先后）",
					name, i, row.ID, want[i])
			}
		}
	}

	rows, err := m.ListVariants(ctx, productID)
	if err != nil {
		t.Fatalf("ListVariants 失败：%v", err)
	}
	check("ListVariants", rows)

	batch, err := m.ListVariantsByProducts(ctx, []string{productID})
	if err != nil {
		t.Fatalf("ListVariantsByProducts 失败：%v", err)
	}
	check("ListVariantsByProducts", batch)

	facts, err := m.ListVariantPriceFacts(ctx, []string{productID})
	if err != nil {
		t.Fatalf("ListVariantPriceFacts 失败：%v", err)
	}
	check("ListVariantPriceFacts", facts)

	options, err := m.ListVariantOptionFacts(ctx, []string{productID})
	if err != nil {
		t.Fatalf("ListVariantOptionFacts 失败：%v", err)
	}
	check("ListVariantOptionFacts", options)

	display, err := m.ListVariantDisplayFacts(ctx, []string{productID})
	if err != nil {
		t.Fatalf("ListVariantDisplayFacts 失败：%v", err)
	}
	check("ListVariantDisplayFacts", display)
}
