package feature

import (
	"context"
	json "go_wp/pkg/jsonx"
	"testing"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

func TestProductCollectionFilterByOption(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)

	color, err := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Key: "color", Name: "颜色",
		Values: []productdto.AttributeValueReq{{Key: "red", Label: "红"}, {Key: "blue", Label: "蓝"}},
	})
	if err != nil {
		t.Fatalf("建颜色属性组失败: %v", err)
	}
	size, serr := f.products.CreateAttribute(ctx, &productdto.CreateAttributeReq{
		ProjectID: f.projectID, Key: "size", Name: "尺码",
		Values: []productdto.AttributeValueReq{{Key: "m", Label: "M"}, {Key: "l", Label: "L"}},
	})
	if serr != nil {
		t.Fatalf("建尺码属性组失败: %v", serr)
	}

	redID := f.createProduct(t, "红衬衫", "red-shirt", "", 99, 99)
	blueID := f.createProduct(t, "蓝衬衫", "blue-shirt", "", 99, 99)

	attach := func(productID, sku, colorValue, sizeValue string) {
		t.Helper()
		if _, uerr := f.products.Update(ctx, &productdto.UpdateReq{
			ID: productID, AttributeIDs: []string{color.ID, size.ID},
		}); uerr != nil {
			t.Fatalf("挂属性组失败: %v", uerr)
		}
		price := 99.0
		options, merr := json.Marshal(map[string]string{"color": colorValue, "size": sizeValue})
		if merr != nil {
			t.Fatalf("组合编码失败: %v", merr)
		}
		if _, verr := f.products.CreateVariant(ctx, &productdto.CreateVariantReq{
			ProductID: productID, SKUCode: sku, Price: &price, OptionValues: options,
		}); verr != nil {
			t.Fatalf("建变体失败: %v", verr)
		}
	}
	attach(redID, "SKU-RED-M", "red", "m")
	attach(blueID, "SKU-BLUE-L", "blue", "l")

	names := func(filter map[string]string) []string {
		t.Helper()
		items, rerr := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, filter)
		if rerr != nil {
			t.Fatalf("解析集合失败（%v）: %v", filter, rerr)
		}
		out := []string{}
		for _, it := range items {
			if name, ok := it["name"].(string); ok {
				out = append(out, name)
			}
		}
		return out
	}
	assertOnly := func(filter map[string]string, want string) {
		t.Helper()
		got := names(filter)
		if len(got) != 1 || got[0] != want {
			t.Fatalf("筛选 %v 应只命中 %q，实际 %v", filter, want, got)
		}
	}

	assertOnly(map[string]string{"option.color": "red"}, "红衬衫")
	assertOnly(map[string]string{"option.size": "l"}, "蓝衬衫")

	assertOnly(map[string]string{"option.color": "red", "option.size": "m"}, "红衬衫")

	if got := names(map[string]string{"option.color": "red", "option.size": "l"}); len(got) != 0 {
		t.Fatalf("不成立的组合应为空集合，实际 %v", got)
	}

	if got := names(map[string]string{"option.color": "red", "status": "published"}); len(got) != 0 {
		t.Fatalf("与状态维度叠加后应为空集合，实际 %v", got)
	}

	if got := names(map[string]string{"option.color": ""}); len(got) != 2 {
		t.Fatalf("空值不该筛掉任何商品，实际 %v", got)
	}

	for _, bad := range []map[string]string{
		{"option.": "red"},
		{"option.color": "红"},
		{"option.color.sub": "red"},
	} {
		if _, rerr := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, bad); rerr == nil {
			t.Fatalf("非法维度 %v 应被拒绝", bad)
		}
	}
}

func TestProductCollectionOptionFilterUsesIndex(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}

	assertPlanUsesIndex(t, f,
		"SELECT id FROM products WHERE EXISTS (SELECT 1 FROM product_variants v "+
			"WHERE v.product_id = products.id AND v.enabled "+
			"AND v.option_values @> '{\"color\":\"red\"}'::jsonb)",
		nil, "idx_product_variants_option_values")
}
