package productmodel_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

func seedCategory(t *testing.T, db *gorm.DB, projectID, name, slug string, parentID *string, sort int) string {
	t.Helper()
	id := uuid.NewString()
	if err := db.Exec(`
		INSERT INTO product_categories
			(id, project_id, parent_id, name, slug, description, image,
			 seo_title, seo_description, sort, metadata, create_time, update_time)
		VALUES (?, ?, ?, ?, ?, '', '', '', '', ?, '{}'::jsonb, now(), now())`,
		id, projectID, parentID, name, slug, sort).Error; err != nil {
		t.Fatalf("插入分类 %s 失败: %v", name, err)
	}
	return id
}

func strPtr(s string) *string { return &s }

func rowsByID(rows []*productmodel.CategoryPageRow) map[string]*productmodel.CategoryPageRow {
	out := make(map[string]*productmodel.CategoryPageRow, len(rows))
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

func TestCategoryRootsPageWalksWholeSubtree(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "分类树用例")

	root := seedCategory(t, db, projectID, "根", "root", nil, 1)
	child := seedCategory(t, db, projectID, "子", "child", strPtr(root), 2)
	grand := seedCategory(t, db, projectID, "孙", "grand", strPtr(child), 3)
	root2 := seedCategory(t, db, projectID, "根二", "root2", nil, 9)

	m := productmodel.NewModel(db)
	rows, total, err := m.ListCategoryRootsPage(context.Background(), projectID, 10, 0)
	if err != nil {
		t.Fatalf("ListCategoryRootsPage: %v", err)
	}
	if total != 2 {
		t.Fatalf("根分类条数应为 2，实际 %d", total)
	}
	if len(rows) != 4 {
		t.Fatalf("一页应带回两棵树的全部 4 行，实际 %d 行", len(rows))
	}
	byID := rowsByID(rows)
	for _, id := range []string{root, child, grand, root2} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("子树展开漏了行 %s（少一层不会报错，只是树上少一截）", id)
		}
	}

	if !byID[root].HasChildren || !byID[child].HasChildren {
		t.Fatalf("root/child 应 HasChildren=true，实际 %v / %v",
			byID[root].HasChildren, byID[child].HasChildren)
	}
	if byID[grand].HasChildren || byID[root2].HasChildren {
		t.Fatalf("grand/root2 应 HasChildren=false，实际 %v / %v",
			byID[grand].HasChildren, byID[root2].HasChildren)
	}

	for _, r := range rows {
		if r.Matched {
			t.Fatalf("ListCategoryRootsPage 不应标 Matched（原 SQL 写死 FALSE），行 %s 为 true", r.ID)
		}
	}
}

func TestCategoryRootsPageKeepsForeignParentAsRoot(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	otherProject := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "本工程")
	support.SeedProjectRow(t, db, otherProject, "别的工程")

	foreignParent := seedCategory(t, db, otherProject, "外工程父", "foreign", nil, 1)
	orphan := seedCategory(t, db, projectID, "遗留子", "orphan", strPtr(foreignParent), 1)

	m := productmodel.NewModel(db)
	rows, total, err := m.ListCategoryRootsPage(context.Background(), projectID, 10, 0)
	if err != nil {
		t.Fatalf("ListCategoryRootsPage: %v", err)
	}
	if total != 1 {
		t.Fatalf("父级不在本工程的行应计为根，根数应为 1，实际 %d", total)
	}
	if len(rows) != 1 || rows[0].ID != orphan {
		t.Fatalf("跨工程父级的行应作为根出现（否则它在列表上整行消失），实际 %d 行", len(rows))
	}
}

func TestCategoryTreeSurvivesCycle(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "环用例")

	a := seedCategory(t, db, projectID, "甲", "a", nil, 1)
	b := seedCategory(t, db, projectID, "乙", "b", strPtr(a), 2)
	if err := db.Exec(`UPDATE product_categories SET parent_id = ? WHERE id = ?`, b, a).Error; err != nil {
		t.Fatalf("造环失败: %v", err)
	}

	m := productmodel.NewModel(db)
	rows, _, err := m.ListCategoryRootsPage(context.Background(), projectID, 10, 0)
	if err != nil {
		t.Fatalf("ListCategoryRootsPage（有环）: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("互为父级的两行都不是根（父级都在本工程），应返回 0 行，实际 %d 行", len(rows))
	}
}

func TestCategorySearchForestKeepsAncestorPath(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "搜索森林用例")

	root := seedCategory(t, db, projectID, "服饰", "apparel", nil, 1)
	mid := seedCategory(t, db, projectID, "男装", "mens", strPtr(root), 2)
	hit := seedCategory(t, db, projectID, "男装外套", "mens-coat", strPtr(mid), 3)
	unrelated := seedCategory(t, db, projectID, "数码", "digital", nil, 9)

	m := productmodel.NewModel(db)
	rows, matchTotal, err := m.ListCategorySearchForest(context.Background(), projectID, "外套")
	if err != nil {
		t.Fatalf("ListCategorySearchForest: %v", err)
	}
	if matchTotal != 1 {
		t.Fatalf("命中条数应为 1，实际 %d", matchTotal)
	}
	byID := rowsByID(rows)

	for _, id := range []string{root, mid, hit} {
		if _, ok := byID[id]; !ok {
			t.Fatalf("搜索森林应带回祖先链，漏了 %s（链断了树就挂不上根）", id)
		}
	}
	if _, ok := byID[unrelated]; ok {
		t.Fatalf("搜索森林不该带回无关分类 %s", unrelated)
	}
	if !byID[hit].Matched {
		t.Fatalf("命中项应 Matched=true")
	}
	if byID[root].Matched || byID[mid].Matched {
		t.Fatalf("祖先应是 Matched=false（只用于定位），实际 root=%v mid=%v",
			byID[root].Matched, byID[mid].Matched)
	}

	if !byID[root].HasChildren || !byID[mid].HasChildren || byID[hit].HasChildren {
		t.Fatalf("HasChildren 不正确：root=%v mid=%v hit=%v",
			byID[root].HasChildren, byID[mid].HasChildren, byID[hit].HasChildren)
	}
}

func TestCategorySearchForestMatchesSlug(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "slug 命中用例")

	id := seedCategory(t, db, projectID, "配件", "accessory-zone", nil, 1)

	m := productmodel.NewModel(db)
	rows, matchTotal, err := m.ListCategorySearchForest(context.Background(), projectID, "accessory")
	if err != nil {
		t.Fatalf("ListCategorySearchForest: %v", err)
	}
	if matchTotal != 1 || len(rows) != 1 || rows[0].ID != id || !rows[0].Matched {
		t.Fatalf("slug 命中未生效：matchTotal=%d rows=%d", matchTotal, len(rows))
	}
}
