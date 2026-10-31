package productmodel_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

func seedRelatedChain(t *testing.T, db *gorm.DB, projectID string) (a, b, c string) {
	t.Helper()
	a, b, c = uuid.NewString(), uuid.NewString(), uuid.NewString()
	insert := func(id, slug string, related string) {
		t.Helper()
		if err := db.Exec(`INSERT INTO products (id, project_id, name, slug, sku_code, status, sort, related_ids, create_time, update_time)
			VALUES (?, ?, ?, ?, ?, 'published', 1, ?::jsonb, now(), now())`,
			id, projectID, "商品 "+slug, slug, "SKU-"+slug, related).Error; err != nil {
			t.Fatalf("夹具备料失败：%v", err)
		}
	}
	insert(a, "rel-a", "[]")
	insert(b, "rel-b", fmt.Sprintf(`[%q]`, a))
	insert(c, "rel-c", "[]")
	return a, b, c
}

func TestProductRefsByRelatedIDsFindsReferrers(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "相关商品反查")
	a, b, c := seedRelatedChain(t, db, projectID)
	m := productmodel.NewModel(db)
	ctx := context.Background()

	hits, err := m.ProductRefsByRelatedIDs(ctx, []string{a, b, c}, []string{projectID})
	if err != nil {
		t.Fatalf("反查失败：%v", err)
	}
	if hit := hits[a]; !hit.Referenced() || len(hit.ProjectIDs) != 1 || hit.ProjectIDs[0] != projectID {
		t.Fatalf("A 应被本工程的 B 引用，实际 %+v", hit)
	}
	if hit := hits[a]; len(hit.RefColumns) != 1 || hit.RefColumns[0] != "products.related_ids" {
		t.Fatalf("拒绝理由要能指向具体列，实际 %+v", hits[a].RefColumns)
	}
	if hits[b].Referenced() {
		t.Fatalf("B 没有人引用它，不应报被引用：%+v", hits[b])
	}
	if hits[c].Referenced() {
		t.Fatalf("C 没有人引用它，不应报被引用：%+v", hits[c])
	}

	other := uuid.NewString()
	support.SeedProjectRow(t, db, other, "另一工程")
	foreign, err := m.ProductRefsByRelatedIDs(ctx, []string{a}, []string{other})
	if err != nil {
		t.Fatalf("跨工程反查失败：%v", err)
	}
	if foreign[a].Referenced() {
		t.Fatalf("跨工程不应命中引用：%+v", foreign[a])
	}

	empty, err := m.ProductRefsByRelatedIDs(ctx, nil, []string{projectID})
	if err != nil || len(empty) != 0 {
		t.Fatalf("空清单应直接返回空结论：%v / %d 项", err, len(empty))
	}
}
