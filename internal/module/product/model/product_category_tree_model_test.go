package productmodel_test

// product_category_tree_model_test.go — 分类树的真库行为验证。
//
// 为什么必须有这一条：这两个方法原来是两条 `WITH RECURSIVE`，GORM 没有 CTE API
// （v1.31.1 实测 clause.With 是空结构体、gorm 内部零使用），所以改成了 Go 侧逐层展开。
// 改写的正确性全在几条**不报错、只是结果不同**的语义上：
//
//   - 子孙集合完整（少一层不会报错，只是树上少一截）
//   - 环不死循环（有环时旧 SQL 靠 path 数组停住，新实现靠 seen 集合）
//   - 跨工程父级仍算根（categoryRootFilter 的后半句：父级不在本工程时该行**是**根，
//     漏掉它会让「数据坏了」表现为「在列表上看不见」）
//   - has_children（原 SQL 的 EXISTS 子查询；新实现按已读到的集合聚合）
//   - 搜索森林的 matched 标记与祖先链方向（向上走，不是向下）
//
// 表结构来自**生产迁移**（support.NewMigratedPGTestDB 复制的模板库），不手抄 CREATE TABLE。
// 连接用超级用户，RLS 在这里不生效，所以「跨工程互不可见」由 SQL 里显式的 project_id
// 谓词验证；策略本身属 public/test/rls 的范围。

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	productmodel "go_wp/internal/module/product/model"
	"go_wp/public/test/support"
)

// seedCategory 插一行分类（只补 NOT NULL 列，其余走表默认）。
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

// idsOf 把页面行投影成 id → 行 的 map（断言按 id 取，避免依赖返回顺序）。
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

	// 主干：root → child → grandchild；另一棵独立树 root2（无子）
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
	// has_children：有子的为 true，叶子为 false（grand 是叶子）
	if !byID[root].HasChildren || !byID[child].HasChildren {
		t.Fatalf("root/child 应 HasChildren=true，实际 %v / %v",
			byID[root].HasChildren, byID[child].HasChildren)
	}
	if byID[grand].HasChildren || byID[root2].HasChildren {
		t.Fatalf("grand/root2 应 HasChildren=false，实际 %v / %v",
			byID[grand].HasChildren, byID[root2].HasChildren)
	}
	// roots 页不标命中
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

	// 父级在别的工程（跨工程遗留）：按 categoryRootFilter 它必须**仍作为根**读出来
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

	// 人为造一个环：a → b → a（两行互为父级）。
	//
	// 断言的重点是「**不死循环**」而不是行数：环里的两行父级都在本工程内，按
	// categoryRootFilter 判定**都不是根**，所以它们本来就进不了递归（原 SQL 也是 0 行）。
	// seen 集合是为「将来某天根集合里出现了带环的子树」准备的历史坏数据防御 ——
	// 这条测试至少钉住「有环时不会转不出来」。
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
	// 命中项 + 它到根的整条祖先链（root → mid → hit），不含无关枝
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
	// 祖先链上 root 与 mid 都有子，hit 是叶子
	if !byID[root].HasChildren || !byID[mid].HasChildren || byID[hit].HasChildren {
		t.Fatalf("HasChildren 不正确：root=%v mid=%v hit=%v",
			byID[root].HasChildren, byID[mid].HasChildren, byID[hit].HasChildren)
	}
}

func TestCategorySearchForestMatchesSlug(t *testing.T) {
	db := support.NewMigratedPGTestDB(t)
	projectID := uuid.NewString()
	support.SeedProjectRow(t, db, projectID, "slug 命中用例")

	// 名字不含关键词、slug 含 —— 原 SQL 是 name ILIKE ? OR slug ILIKE ?，只跑 name 分支会漏
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
