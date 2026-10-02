package feature

// product_category_orphan_parent_test.go — 父级不在本工程的分类必须在后台列表里可见（lead 授权的 ②）。
//
// 真实可达的坏数据形态（不需要绕过外键）：分类 C 属工程 A，父级 P 属工程 B。
// 分类页的根查询是 `project_id = A AND parent_id IS NULL`，再从根沿 parent_id 递归 ——
// C 既不是根、也不在任何可见父节点的子树里，**整行消失**：用户既看不到也改不了它。
//
// 修法要求（lead）：不能把这类节点静默当成普通根就算完 —— 操作者必须能看出这条记录有问题。
// 因此这里同时断言两件事：① C 出现在列表里；② 它的编辑抽屉里，父级那一项写明「不属于本工程」。

import (
	"net/http"
	"strings"
	"testing"

	productdto "go_wp/internal/module/product/dto"
	projectdto "go_wp/internal/module/project/dto"
)

func TestCategoryOrphanParentVisibleInList(t *testing.T) {
	engine, f := newTaxonomyPageEngine(t)
	if engine == nil {
		return
	}
	ctx := t.Context()
	parent, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "男装", Slug: "men",
	})
	if err != nil {
		t.Fatalf("创建父分类失败: %v", err)
	}
	child, err := f.svc.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "夹克", Slug: "jackets", ParentID: parent.ID,
	})
	if err != nil {
		t.Fatalf("创建子分类失败: %v", err)
	}

	// 把父级挪到另一个工程（真实两工程数据；不动外键、不用 session_replication_role）。
	other, err := f.projects.Create(ctx, &projectdto.CreateReq{Name: "另一个工程"})
	if err != nil {
		t.Fatalf("创建第二个工程失败: %v", err)
	}
	if err = f.db.Exec("UPDATE product_categories SET project_id = ? WHERE id = ?", other.ID, parent.ID).Error; err != nil {
		t.Fatalf("挪动父分类失败: %v", err)
	}

	rec := httptestGet(engine, "/admin/product-categories?project="+f.projectID)
	if rec.Code != http.StatusOK {
		t.Fatalf("分类页应 200，实际 %d：%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// ① 不再丢行：子分类必须作为**列表行**出现。
	// 只断言名字出现在页面里是不够的 —— 候选下拉里的「夹克」同样是这个名字，
	// 那正是修前「整行消失但下拉里看得见」的假阳性形态。所以钉行的锚点。
	if !strings.Contains(body, `data-category-id="`+child.ID+`"`) {
		t.Fatalf("父级不在本工程的分类应作为列表行出现（修前整棵树漏掉它）：页面上没有 data-category-id=%q 的行", child.ID)
	}
	// ② 可见性提示：抽屉里父级那一项要说明它不属于本工程。
	if !strings.Contains(body, "不属于本工程") {
		t.Fatalf("抽屉里的父级那一项应写明「不属于本工程」，否则操作者看不出这条记录有问题")
	}
	// ③ 父级不再作为本工程的候选项出现（跨工程分类不属于本工程的下拉）。
	if strings.Contains(body, `<option value="`+parent.ID+`" >男装</option>`) {
		t.Errorf("跨工程分类仍被列为本工程的父级候选")
	}
}
