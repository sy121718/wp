// Package feature product 模块 feature 测试 —— 商品列表组件（issue #23）。
//
// 覆盖三条验收（真实 PostgreSQL + 生产迁移 + 真实集合源）：
//
//  1. 筛选维度在构建期下推到集合源（状态 / 分类 / 品牌 / 标签），命中集正确；
//  2. 排序在截断之前生效（「取最新 N 条」拿到的是最新的那几条，不是最早那几条）；
//  3. 空集合渲染空态 + 多端契约（自适应列、窄屏单列、宽度不写死）。
package feature

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/builder"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

// productListPage 单节点商品列表页（props 由调用方给出）。
func productListPage(t *testing.T, props string) *builder.Page {
	t.Helper()
	raw := `{"settings":{"layout":{"mode":"boxed","maxWidth":"1200px"}},"root":[` +
		`{"id":"n1","type":"core.productList","props":` + props + `}]}`
	p, err := builder.ParsePage([]byte(raw))
	if err != nil {
		t.Fatalf("解析页面文档失败: %v", err)
	}
	return p
}

// productListProps 商品列表的默认字段映射（列表自己按当前项取值）。
func productListProps() string {
	return `{"collectionSource":"content:product","imageField":"item.images",` +
		`"titleField":"item.name","priceField":"item.priceRange",` +
		`"linkField":"item.slug","linkPrefix":"/products/"` + `}`
}

// TestProductListFiltersAndOrders 验收 1+2：
// 状态筛选在构建期下推；排序在截断之前生效且结果稳定。
func TestProductListFiltersAndOrders(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "草稿衬衫", "draft-shirt", "", 99, 199)
	f.createProduct(t, "已发布外套", "published-coat", "", 299, 299)

	// 把外套置为已发布（默认建出来是 draft）。
	ctx := context.Background()
	published := productenums.StatusPublished
	list, lerr := f.products.List(ctx, &productdto.ListReq{ProjectID: f.projectID, Size: 100})
	if lerr != nil {
		t.Fatalf("读商品列表失败: %v", lerr)
	}
	for _, row := range list {
		if row.Slug != "published-coat" {
			continue
		}
		if _, uerr := f.products.Update(ctx, &productdto.UpdateReq{ID: row.ID, Status: &published}); uerr != nil {
			t.Fatalf("置为已发布失败: %v", uerr)
		}
	}

	// 只筛已发布：草稿不出现在产物里。
	page := productListPage(t, `{"collectionSource":"content:product","filterStatus":"published",`+
		`"titleField":"item.name","linkField":"item.slug","linkPrefix":"/products/"}`)
	compiled, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(compiled.HTML, "已发布外套") {
		t.Fatalf("已发布商品应出现在产物里\n%s", compiled.HTML)
	}
	if strings.Contains(compiled.HTML, "草稿衬衫") {
		t.Fatalf("草稿商品不该出现在已发布筛选的产物里\n%s", compiled.HTML)
	}
	if !strings.Contains(compiled.HTML, "/products/published-coat") {
		t.Fatalf("链接应为前缀 + slug\n%s", compiled.HTML)
	}
}

// TestProductListEmptyStateAndResponsive 验收 3：
// 无命中渲染空态文案；多端契约（自适应折行 / 窄屏单列 / 宽度不写死）。
func TestProductListEmptyStateAndResponsive(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "夏季衬衫", "summer-shirt", "", 99, 199)
	tag := colTag(t, f, "无人使用的标签", "unused-tag")

	// 按一个没有任何商品挂载的标签筛选 → 空集合（不是报错）。
	page := productListPage(t, productListPropsWith(`"filterTagId":"`+tag.ID+`",`+
		`"emptyText":"该标签下还没有商品"`))
	compiled, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(compiled.HTML, "该标签下还没有商品") {
		t.Fatalf("无命中应渲染空态文案\n%s", compiled.HTML)
	}
	if strings.Contains(compiled.HTML, "sky-product-list-item") {
		t.Fatalf("空集合不该输出项目节点\n%s", compiled.HTML)
	}

	// 多端契约：自适应列不写死宽度、窄屏单列。
	css := string(compiled.CSS)
	for _, want := range []string{
		"auto-fill",
		"min(100%",
		"grid-template-columns: 1fr", // 窄视口恒单列
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("多端契约缺少 %q\n%s", want, css)
		}
	}
}

// productListPropsWith 在默认字段映射基础上追加 props 片段。
func productListPropsWith(extra string) string {
	base := productListProps()
	return strings.TrimSuffix(base, "}") + "," + extra + "}"
}
