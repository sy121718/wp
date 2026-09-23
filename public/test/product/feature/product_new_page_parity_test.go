package feature

// product_new_page_parity_test.go — 建表单片段的**唯一载体**（新建整页）必须钩子齐全。
//
// 建表单片段（internal/templates/admin/partials/product_create_form.html）现在只有一个调用点：
// 新建整页 `/admin/products/new`。列表页那份抽屉（`<template id="tpl-product-create">`）
// 没有任何 `data-drawer-open` 指向它，已随「写失败不丢输入」批 1 删除
// （见 docs/02-T-write-fail-echo-batch1.md §5 P1-2）。
//
// 本用例两个方向：
//   · 整页必须渲染片段里**全部**协议钩子 —— 少一个不会编译失败，只会让某个能力在某条入口上
//     静默失效（本会话实测过：整页缺属性组与多仓字段时，4 个断言直接红）；
//   · 列表页**不得**再出现建表单 —— 防死块复活（它每次渲染都白付一份完整表单的开销）。

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

func TestProductNewPageCarriesFullCreateForm(t *testing.T) {
	engine, f := newCreateFlowEngine(t)
	if engine == nil {
		return
	}
	// 造属性组：让「属性组勾选」这条能力被真实渲染（fixture 默认没有属性）。
	mkVariationAttr(t, f, "颜色", "color", []string{"red", "blue"})

	page := getProductsNewPage(engine, f.projectID)
	if len(page) == 0 {
		t.Fatalf("新建整页渲染为空")
	}
	// 1) 建表单的**全部**协议钩子必须在场。
	//    这里原先还有一个「抽屉里出现的钩子整页必须一个不少」的基准循环 —— 那个抽屉已退役
	//    （列表页的 template 没有任何 data-drawer-open 指向它），基准随之消失，循环变成
	//    **空转通过**（`Contains(列表页, hook)` 恒 false）。「测试还在、判据已死」比没有测试
	//    更危险，所以删掉基准、直接钉整页。
	//
	//    其中三项（多仓勾选 `name="warehouseIds"`、「从仓库选」`name="skuSource"` 与其候选
	//    `data-sku-candidates`）依赖「工程里有仓库 / 仓库里有货」才会渲染，而本用例的 fixture
	//    （newCreateFlowEngine → attrFixture）连库存契约都没接 —— 它们由
	//    product_warehouse_multi_stock_test.go 与 product_warehouse_sku_pick_test.go 覆盖
	//    （那两条的 fixture 会真造仓库），在这个 fixture 上强求只会得到一条假失败。
	needsWarehouse := map[string]bool{
		`name="warehouseIds"`: true, `name="skuSource"`: true, `data-sku-candidates`: true,
	}
	checkFormHooks := []string{"data-product-create-form",
		"name=\"attributeIds\"", "颜色（color）",
		"/static/js/product-create-form.js"} // 共享增强脚本（单一真源）
	for _, want := range append(append([]string{}, createFormHooks...), checkFormHooks...) {
		if needsWarehouse[want] {
			continue
		}
		if !strings.Contains(page, want) {
			t.Fatalf("新建整页缺少 %q —— 建表单片段是它唯一的载体，缺项即能力丢失", want)
		}
	}

	// 2) 列表页**不得**再渲染建表单。死块已删，这一条防它复活：那个 template 每次列表页渲染
	//    都要白付一份完整表单（含仓库 SKU 候选 datalist）的开销，而页面上没有任何入口能打开它。
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

	// 3) 整页不渲染抽屉的「取消」按钮（没有可关闭的抽屉）。
	//    判据必须带 class：布局里本来就有一个 drawer-close 关闭按钮（layout.html），
	//    只按 data-drawer-close 判会把它误当成本片段的取消按钮。
	const cancelBtn = `class="btn btn-ghost" data-drawer-close`
	if strings.Contains(page, cancelBtn) {
		t.Fatalf("新建整页不应渲染抽屉取消按钮（InDrawer 判断失效）")
	}
}
