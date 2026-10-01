package productservice

import (
	"testing"
	"time"

	productmodel "go_wp/internal/module/product/model"
)

// 分类列表按树根分页后的建树逻辑：定序、嵌套、层级都由这里定，
// 它错了不会报错 —— 只会让某一层分类悄悄消失或排到别处。

func pageRow(id, parent, name string, sort int, createdAt time.Time) *productmodel.CategoryPageRow {
	row := &productmodel.CategoryPageRow{}
	row.ID, row.Name, row.Sort, row.CreatedAt = id, name, sort, createdAt
	row.UpdatedAt = createdAt
	if parent != "" {
		p := parent
		row.ParentID = &p
	}
	return row
}

func TestBuildCategoryPageTreeNestsAndOrders(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []*productmodel.CategoryPageRow{
		pageRow("c2", "a1", "子二", 2, base),
		pageRow("b1", "", "第二根", 2, base),
		pageRow("a1", "", "第一根", 1, base),
		pageRow("c1", "a1", "子一", 1, base),
	}
	rows[3].HasChildren = true

	roots := buildCategoryPageTree(rows)
	if len(roots) != 2 {
		t.Fatalf("应有 2 个根，实际 %d", len(roots))
	}
	if roots[0].ID != "a1" || roots[1].ID != "b1" {
		t.Fatalf("根应按 sort 排序，实际 %s / %s", roots[0].ID, roots[1].ID)
	}
	if len(roots[0].Children) != 2 {
		t.Fatalf("a1 应有 2 个子级，实际 %d", len(roots[0].Children))
	}
	if roots[0].Children[0].ID != "c1" || roots[0].Children[1].ID != "c2" {
		t.Fatalf("子级应按 sort 排序，实际 %s / %s", roots[0].Children[0].ID, roots[0].Children[1].ID)
	}
	if roots[0].Depth != 0 || roots[0].Children[0].Depth != 1 {
		t.Fatalf("层级应由服务端算好：根 %d、子 %d", roots[0].Depth, roots[0].Children[0].Depth)
	}
	if !roots[0].Children[0].HasChildren || roots[0].Children[1].HasChildren {
		t.Fatal("has_children 应逐行透传")
	}
}

func TestBuildCategoryPageTreeKeepsOrphansAndCycles(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := []*productmodel.CategoryPageRow{
		pageRow("x", "missing", "孤儿", 1, base),
		pageRow("y", "y", "自环", 1, base),
		pageRow("z", "x", "孤儿的子级", 1, base),
	}
	roots := buildCategoryPageTree(rows)
	if len(roots) != 2 {
		t.Fatalf("父级缺失 / 自环都按顶级处理，节点不丢：实际 %d 个根", len(roots))
	}
	if roots[0].ID != "x" || len(roots[0].Children) != 1 || roots[0].Children[0].ID != "z" {
		t.Fatalf("孤儿的下级仍应挂在自己名下：%+v", roots[0])
	}
	if roots[1].ID != "y" || len(roots[1].Children) != 0 {
		t.Fatalf("自环节点应自成顶级且不挂到自己名下：%+v", roots[1])
	}
}
