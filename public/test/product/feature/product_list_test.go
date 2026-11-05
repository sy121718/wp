package feature

import (
	"context"
	"strings"
	"testing"

	"go_wp/pkg/builder"
	"go_wp/pkg/builder/core"
	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
)

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

func productListProps() string {
	return `{"collectionSource":"content:product","imageField":"item.images",` +
		`"titleField":"item.name","priceField":"item.priceRange",` +
		`"linkField":"item.slug","linkPrefix":"/products/"` + `}`
}

func TestProductListFiltersAndOrders(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "草稿衬衫", "draft-shirt", "", 99, 199)
	f.createProduct(t, "已发布外套", "published-coat", "", 299, 299)

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

func TestProductListEmptyStateAndResponsive(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "夏季衬衫", "summer-shirt", "", 99, 199)
	tag := colTag(t, f, "无人使用的标签", "unused-tag")

	page := productListPage(t, productListPropsWith(`"filterTagId":"`+tag.ID+`",`+
		`"emptyText":"该标签下还没有商品"`))
	compiled, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	if !strings.Contains(compiled.HTML, "该标签下还没有商品") {
		t.Fatalf("无命中应渲染空态文案\n%s", compiled.HTML)
	}
	cardEnd := strings.Index(compiled.HTML, "</template>")
	if cardEnd < 0 || !strings.Contains(compiled.HTML[:cardEnd], "data-catalog-card") {
		t.Fatal("JSON目录必须保留受控卡片模板")
	}
	if strings.Contains(compiled.HTML[cardEnd:], `class="sky-product-list-item"`) {
		t.Fatalf("空集合不该输出已实例化的项目节点\n%s", compiled.HTML)
	}

	css := string(compiled.CSS)
	for _, want := range []string{
		"auto-fill",
		"min(100%",
		"grid-template-columns: 1fr",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("多端契约缺少 %q\n%s", want, css)
		}
	}
}

func TestProductListFilterOptionsFromCollectionSource(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)

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

	if !strings.Contains(compiled.HTML, "categoryIds="+cat.ID) {
		t.Fatalf("分类选项应带降级链接参数 categoryIds=%s\n%s", cat.ID, compiled.HTML)
	}
}

type noOptionsCollection struct{}

func (noOptionsCollection) ResolveCollection(_ context.Context, _ string, _ map[string]string) ([]map[string]any, error) {
	return []map[string]any{{"id": "x1", "name": "无能力源的条目", "slug": "no-options"}}, nil
}

func (noOptionsCollection) CollectionSchemas(context.Context) ([]core.CollectionSchema, error) {
	return []core.CollectionSchema{{Source: "content:product", Fields: []string{"id", "name", "slug"}}}, nil
}

func TestProductListFilterBarEmptyWithoutCapability(t *testing.T) {
	reg := core.NewCollectionRegistry()
	if err := reg.Register(noOptionsCollection{}); err != nil {
		t.Fatalf("注册无筛选能力的集合源失败: %v", err)
	}

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

func productListPropsWith(extra string) string {
	base := productListProps()
	return strings.TrimSuffix(base, "}") + "," + extra + "}"
}
