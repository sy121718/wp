package productmodel_test

// product_related_refs_test.go — 相关商品自引用的**反向扫描**判据（PRD-016）。
//
// related_ids 是 JSONB 自引用、没有外键：删掉 A 之后，B.related_ids 里仍然留着 A。
// DEC-04（阻止删除 / 同事务清理 / 容忍并在读取剔除）还没定，所以本轮不加删除侧行为；
// 先钉住「谁引用了它」这条只读能力，口径定了直接接上，不需要再补一次查询。

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

// seedRelatedChain 造 A ← B（B 把 A 当相关商品）与独立的 C。
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

	// 工程隔离：换个工程问同一批 id，必须一条都不命中（否则跨工程删除会被别处的引用挡住）。
	other := uuid.NewString()
	support.SeedProjectRow(t, db, other, "另一工程")
	foreign, err := m.ProductRefsByRelatedIDs(ctx, []string{a}, []string{other})
	if err != nil {
		t.Fatalf("跨工程反查失败：%v", err)
	}
	if foreign[a].Referenced() {
		t.Fatalf("跨工程不应命中引用：%+v", foreign[a])
	}

	// 空清单不该触发任何查询，也不该报错（调用方在「没有任何候选」时不该付一次往返）。
	empty, err := m.ProductRefsByRelatedIDs(ctx, nil, []string{projectID})
	if err != nil || len(empty) != 0 {
		t.Fatalf("空清单应直接返回空结论：%v / %d 项", err, len(empty))
	}
}
