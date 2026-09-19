package feature

// product_new_page_parity_test.go — 新建整页与列表页抽屉的能力对等。
//
// 两者共用同一份建表单片段（internal/templates/admin/partials/product_create_form.html）：
// 整页少一块能力不会编译失败，只会在那一条入口上静默丢字段。本会话实测过这条线 ——
// 整页缺属性组与多仓字段时，product feature 的 4 个断言直接红（抽屉 markup 不是壳，
// 它承载真实能力；退役流程必须先对齐能力，再谈删除）。
//
// 判据取「列表页为基准、整页不得少于它」+ 显式钉住核心钩子：前者让该断言在
// 未来新增字段时自动生效（新增字段忘了搬过去就会红），后者不依赖 fixture 数据。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// getProductsNewPage 渲染商品新建整页（/admin/products/new）。
func getProductsNewPage(engine *gin.Engine, projectID string) string {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/products/new?project="+projectID, nil)
	engine.ServeHTTP(rec, req)
	return rec.Body.String()
}

// createFormHooks 建表单里「抽屉与整页都必须有」的钩子与字段（片段内的协议面）。
var createFormHooks = []string{
	"data-product-create-form",    // 增强脚本按它定位表单
	"data-sku-input",              // 主体 SKU 输入框
	"data-sku-regenerate",         // 「重新生成」按钮
	"data-sku-placeholder-bundle", // 捆绑态占位符
	"data-sku-hint",               // 系统建议文案钩子
	"data-sku-manual",             // 请手填提示行
	"name=\"attributeIds\"",       // 属性组勾选
	"name=\"warehouseIds\"",       // 多仓归属
	"name=\"skuSource\"",          // SKU 来源（自己创建 / 从仓库选）
	"value=\"bundle\"",            // 捆绑类型选项
}

func TestProductNewPageSharesCreateFormWithDrawer(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	// 造属性组：让「属性组勾选」这条原抽屉能力在两处都被真实渲染（fixture 默认没有属性）。
	mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})

	list := getProductsPage(engine, f.projectID)
	page := getProductsNewPage(engine, f.projectID)
	if len(page) == 0 {
		t.Fatalf("新建整页渲染为空")
	}
	// 1) 核心钩子必须出现在整页。
	for _, want := range []string{
		"data-product-create-form", "data-sku-input", "data-sku-regenerate",
		"data-sku-placeholder-bundle", "data-sku-hint", "data-sku-manual",
		"name=\"attributeIds\"", "颜色（color）",
		"/static/js/product-create-form.js", // 共享增强脚本（单一真源）
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("新建整页缺少 %q（与抽屉共用同一片段，缺项即能力不对等）", want)
		}
	}
	// 2) 基准判据：抽屉里出现的建表单钩子，整页必须一个不少。
	for _, hook := range createFormHooks {
		if strings.Contains(list, hook) && !strings.Contains(page, hook) {
			t.Fatalf("抽屉有 %q 而新建整页没有 —— 字段只在一处存在就是静默丢能力", hook)
		}
	}
	// 3) 形态差异：片段内的「取消」按钮属于抽屉（整页没有可关闭的抽屉）。
	// 判据必须带 class：布局里本来就有一个 drawer-close 关闭按钮（layout.html），
	// 只按 data-drawer-close 判会把它误当成本片段的取消按钮。
	const cancelBtn = `class="btn btn-ghost" data-drawer-close`
	if !strings.Contains(list, cancelBtn) {
		t.Fatalf("列表页抽屉应渲染取消按钮")
	}
	if strings.Contains(page, cancelBtn) {
		t.Fatalf("新建整页不应渲染抽屉取消按钮（InDrawer 判断失效）")
	}
	// 4) 单一真源：内联副本必须已消失，否则两处会各自漂移。
	if strings.Contains(list, "function codeSegment") {
		t.Fatalf("列表页仍有内联增强脚本副本，应只用共享文件")
	}
}
