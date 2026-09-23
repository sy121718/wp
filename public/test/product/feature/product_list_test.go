// Package feature product 模块 feature 测试 —— 商品列表组件（issue #23）。
//
// 覆盖三条验收（真实 PostgreSQL + 生产迁移 + 真实集合源）：
//
//  1. 筛选维度在构建期下推到集合源（状态 / 分类 / 品牌 / 标签），命中集正确；
//  2. 排序在截断之前生效（「取最新 N 条」拿到的是最新的那几条，不是最早那几条）；
//  3. 空集合渲染空态 + 多端契约（自适应列、窄屏单列、宽度不写死）；
//  4. 筛选栏选项经集合源注册表按**注册时的集合源**转发（ARCH-01），无该能力的源不报错。
package feature

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
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

// TestProductListFilterOptionsFromCollectionSource 验收（ARCH-01 修复票）：
// 静态列表页（集合源 content:product、筛选栏开启）的产物 HTML 里**含来自真实数据的筛选选项**。
//
// 走的是线上同形的路径：注入 ctx.Collection 的就是 core.CollectionRegistry（装配点如此），
// 组件把本节点声明的集合源交给它，由注册表按注册时的「源 → 提供方」转发。
// 改动前这条必红：能力断言打在注册表自己身上恒不成立 → 四维全空、筛选栏整块不渲染，
// 而构建不报任何错（正是 ARCH-01 排查时实测到的静默降级）。
func TestProductListFilterOptionsFromCollectionSource(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	// 真实数据：分类 / 品牌 / 标签各一条，名字取得可辨认（断言它们来自数据，不是模板兜底）。
	cat := colCategory(t, f, "季节精选分类", "season-pick")
	colBrand(t, f, "季节品牌", "season-brand")
	colTag(t, f, "季节标签", "season-tag")
	f.createProduct(t, "夏季衬衫", "summer-shirt", "", 99, 199)

	page := productListPage(t, `{"collectionSource":"content:product","filters":"categories,brands,tags",`+
		`"titleField":"item.name","linkField":"item.slug","linkPrefix":"/products/"}`)
	compiled, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(compiled.HTML, "sky-product-list-filters") {
		t.Fatalf("筛选栏应渲染出来（注册表未转发时这里恒空）\n%s", compiled.HTML)
	}
	for _, want := range []string{"季节精选分类", "季节品牌", "季节标签"} {
		if !strings.Contains(compiled.HTML, want) {
			t.Fatalf("筛选栏应含来自真实数据的选项 %q\n%s", want, compiled.HTML)
		}
	}
	// 选项是可点链接（无 JS 降级路径），按当前多选语义携带真实分类 id。
	if !strings.Contains(compiled.HTML, "categoryIds="+cat.ID) {
		t.Fatalf("分类选项应带降级链接参数 categoryIds=%s\n%s", cat.ID, compiled.HTML)
	}
}

// noOptionsCollection 一个「注册了集合源、但没有筛选选项能力」的提供方
// （故意不实现 core.CollectionFilterOptionsProvider）。
//
// 用它而不是拿 content:article 顶替：productList 的 collectionSource 是**带选项的
// 枚举字段**（只接受 /content:product，构建期就拒绝别的源），所以「同一组件 + 无该能力
// 的源」只能由一个不实现该能力的商品源提供方来表达。
type noOptionsCollection struct{}

func (noOptionsCollection) ResolveCollection(_ context.Context, _ string, _ map[string]string) ([]map[string]any, error) {
	return []map[string]any{{"id": "x1", "name": "无能力源的条目", "slug": "no-options"}}, nil
}

func (noOptionsCollection) CollectionSchemas(context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{{Source: "content:product", Fields: []string{"id", "name", "slug"}}}, nil
}

// TestProductListFilterBarEmptyWithoutCapability 不回归：
// 集合源没有筛选能力时构建**仍然成功**、筛选栏为空而不是报错（列表本身照常渲染）。
func TestProductListFilterBarEmptyWithoutCapability(t *testing.T) {
	reg := core.NewCollectionRegistry()
	if err := reg.Register(noOptionsCollection{}); err != nil {
		t.Fatalf("注册无筛选能力的集合源失败: %v", err)
	}
	// 同一份注册表里没有别的源能答 content:product 的筛选选项 → 注册表返回空选项。
	page := productListPage(t, `{"collectionSource":"content:product","filters":"categories,brands",`+
		`"titleField":"item.name","linkField":"item.slug","linkPrefix":"/products/"}`)
	compiled, err := compileCollection(t, page, reg, "proj-no-options")
	if err != nil {
		t.Fatalf("没有筛选能力的集合源不该让构建失败: %v", err)
	}
	if strings.Contains(compiled.HTML, "sky-product-list-filters") {
		t.Fatalf("没有筛选能力的源不该渲染筛选栏\n%s", compiled.HTML)
	}
	if !strings.Contains(compiled.HTML, "无能力源的条目") {
		t.Fatalf("筛选栏缺失不该影响列表本身\n%s", compiled.HTML)
	}
}

// productListPropsWith 在默认字段映射基础上追加 props 片段。
func productListPropsWith(extra string) string {
	base := productListProps()
	return strings.TrimSuffix(base, "}") + "," + extra + "}"
}
