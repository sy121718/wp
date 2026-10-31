package feature

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func getProductsNewPage(engine *gin.Engine, projectID string) string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/products/new?project="+projectID, nil)
	engine.ServeHTTP(rec, req)
	return rec.Body.String()
}

var createFormHooks = []string{
	"data-product-create-form",
	"data-sku-input",
	"name=\"attributeIds\"",
	"name=\"warehouseIds\"",
	"name=\"skuSource\"",
}

func TestProductNewPageCarriesFullCreateForm(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}

	mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})

	page := getProductsNewPage(engine, f.projectID)
	if len(page) == 0 {
		t.Fatalf("新建整页渲染为空")
	}

	needsWarehouse := map[string]bool{
		`name="warehouseIds"`: true, `name="skuSource"`: true, `data-sku-candidates`: true,
	}
	checkFormHooks := []string{"data-product-create-form",
		"name=\"attributeIds\"", "颜色（color）",
		"/static/js/product-create-form.js"}
	for _, want := range append(append([]string{}, createFormHooks...), checkFormHooks...) {
		if needsWarehouse[want] {
			continue
		}
		if !strings.Contains(page, want) {
			t.Fatalf("新建整页缺少 %q —— 建表单片段是它唯一的载体，缺项即能力丢失", want)
		}
	}

	list := getProductsPage(engine, f.projectID)
	for _, forbidden := range []string{
		"data-product-create-form", "data-sku-candidates", `name="attributeIds"`,
	} {
		if strings.Contains(list, forbidden) {
			t.Fatalf("列表页不应再渲染建表单（%q 命中）—— 抽屉已退役，死块复活会白付渲染开销", forbidden)
		}
	}
	if strings.Contains(list, "function codeSegment") {
		t.Fatalf("列表页仍有内联增强脚本副本，应只用共享文件")
	}

	const cancelBtn = `class="btn btn-ghost" data-drawer-close`
	if strings.Contains(page, cancelBtn) {
		t.Fatalf("新建整页不应渲染抽屉取消按钮（InDrawer 判断失效）")
	}
}
