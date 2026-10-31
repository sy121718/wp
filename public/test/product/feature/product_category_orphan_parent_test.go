package feature

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

	if !strings.Contains(body, `data-category-id="`+child.ID+`"`) {
		t.Fatalf("父级不在本工程的分类应作为列表行出现（修前整棵树漏掉它）：页面上没有 data-category-id=%q 的行", child.ID)
	}

	if !strings.Contains(body, "不属于本工程") {
		t.Fatalf("抽屉里的父级那一项应写明「不属于本工程」，否则操作者看不出这条记录有问题")
	}

	if strings.Contains(body, `<option value="`+parent.ID+`" >男装</option>`) {
		t.Errorf("跨工程分类仍被列为本工程的父级候选")
	}
}
