package feature

import (
	"context"
	"testing"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

func TestCollectionFilterOptions(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()

	parent := colCategory(t, f, "衣着", "apparel")
	child, cerr := f.products.CreateCategory(ctx, &productdto.CreateCategoryReq{
		ProjectID: f.projectID, Name: "上衣", Slug: "tops", ParentID: parent.ID,
	})
	if cerr != nil {
		t.Fatalf("建子分类失败: %v", cerr)
	}
	if _, berr := f.products.CreateBrand(ctx, &productdto.CreateBrandReq{
		ProjectID: f.projectID, Name: "Alibarbar", Slug: "alibarbar",
	}); berr != nil {
		t.Fatalf("建品牌失败: %v", berr)
	}
	colTag(t, f, "热卖", "hot-sale")

	variation := true
	if _, aerr := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Key: "color", Name: "颜色", IsVariation: &variation,
		Values: []productdto.AttributeValueReq{{Key: "red", Label: "红"}, {Key: "blue", Label: "蓝"}},
	}); aerr != nil {
		t.Fatalf("建属性组失败: %v", aerr)
	}
	plain := false
	if _, aerr := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Key: "material", Name: "材质", IsVariation: &plain,
		Values: []productdto.AttributeValueReq{{Key: "cotton", Label: "棉"}},
	}); aerr != nil {
		t.Fatalf("建非变体属性组失败: %v", aerr)
	}

	options, oerr := f.products.CollectionFilterOptions(ctx, productcontract.CollectionSourceProduct, f.projectID)
	if oerr != nil {
		t.Fatalf("取筛选选项失败: %v", oerr)
	}

	var foundParent, foundChild bool
	for _, item := range options.Categories {
		switch item.ID {
		case parent.ID:
			foundParent = true
			if item.ParentID != "" {
				t.Fatalf("根分类不该有父节点: %+v", item)
			}
		case child.ID:
			foundChild = true
			if item.ParentID != parent.ID {
				t.Fatalf("子分类应指回父分类: %+v", item)
			}
		}
	}
	if !foundParent || !foundChild {
		t.Fatalf("分类选项应含父子两个: %+v", options.Categories)
	}
	if len(options.Brands) != 1 || options.Brands[0].Name != "Alibarbar" {
		t.Fatalf("品牌选项不符: %+v", options.Brands)
	}
	if len(options.Tags) != 1 || options.Tags[0].Name != "热卖" {
		t.Fatalf("标签选项不符: %+v", options.Tags)
	}

	if len(options.Attributes) != 1 {
		t.Fatalf("只该给参与变体的属性组（实际 %+v）", options.Attributes)
	}
	group := options.Attributes[0]
	if group.Key != "color" || group.Name != "颜色" {
		t.Fatalf("属性组不符: %+v", group)
	}
	if len(group.Values) != 2 || group.Values[0].Key != "red" || group.Values[0].Name != "红" {
		t.Fatalf("属性值应带 key 与展示名: %+v", group.Values)
	}

	if _, err := f.products.CollectionFilterOptions(ctx, productcontract.CollectionSourceProduct, ""); err == nil {
		t.Fatalf("缺工程 ID 应报错")
	}

	var _ core.CollectionFilterOptionsProvider = f.products
}
