package producthttp

import (
	"github.com/gin-gonic/gin"
	productdto "go_wp/internal/module/product/dto"
	"strings"
	"testing"
)

func TestCategoryDrawerRetainsMissingParent(t *testing.T) {
	for _, parent := range []string{"p-missing", "c-parent", ""} {
		body := renderAdminTemplate(t, "admin/product/product_category_form.html", gin.H{"Entity": &productdto.CategoryResp{ID: "c-child", Name: "衬衫", ParentID: parent}, "Project": "p1", "Categories": []*productdto.CategoryResp{{ID: "c-parent", Name: "服装", ProjectID: "p1"}}, "t": testTranslateFunc()})
		if !strings.Contains(body, `value="`+parent+`" selected`) {
			t.Fatalf("当前父级丢失: %s", body)
		}
		if parent == "p-missing" && !strings.Contains(body, "不属于本工程") {
			t.Fatal("缺失父级未提示")
		}
		if !strings.Contains(body, `data-write-builder="product-category"`) {
			t.Fatal("分类父级的空串语义必须由显式对象提交承载")
		}
		if strings.Contains(body, `name="keyword"`) || !strings.Contains(body, `data-category-parent-search`) {
			t.Fatal("父级搜索词只能进入查询请求，不应混入分类写表单")
		}
	}
}
