// Package feature product 模块 feature 测试 —— 属性值筛选维度（issue #25）。
//
// 属性值不在商品行上：products.attribute_ids 只存属性组引用，值在变体的 option_values（JSONB）里。
// 所以这条链路要验证的是「按属性值筛 = 存在启用变体在该属性上取该值」这件事真的成立，
// 而且能与既有维度叠加、形状错误能拒绝。
package feature

import (
	"context"
	"encoding/json"
	"testing"

	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

// TestProductCollectionFilterByOption 验收（issue #25）：
// 单属性筛选、多属性 AND、与状态维度叠加、非法形状拒绝、空值不筛。
func TestProductCollectionFilterByOption(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	ctx := core.WithBuildProjectID(context.Background(), f.projectID)

	// 两个属性组：颜色（参与变体）、尺码。
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

	// 挂属性组 + 给变体写 option_values（值在变体上，这正是本票的核心事实）。
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

	// 单属性。
	assertOnly(map[string]string{"option.color": "red"}, "红衬衫")
	assertOnly(map[string]string{"option.size": "l"}, "蓝衬衫")
	// 多属性 AND。
	assertOnly(map[string]string{"option.color": "red", "option.size": "m"}, "红衬衫")
	// 组合不成立 → 空集合（不是报错）。
	if got := names(map[string]string{"option.color": "red", "option.size": "l"}); len(got) != 0 {
		t.Fatalf("不成立的组合应为空集合，实际 %v", got)
	}
	// 与状态维度叠加：全部商品是草稿 → 筛已发布为空。
	if got := names(map[string]string{"option.color": "red", "status": "published"}); len(got) != 0 {
		t.Fatalf("与状态维度叠加后应为空集合，实际 %v", got)
	}
	// 空值 = 该维度不参与（与既有维度同一口径）。
	if got := names(map[string]string{"option.color": ""}); len(got) != 2 {
		t.Fatalf("空值不该筛掉任何商品，实际 %v", got)
	}

	// 形状错误必须报错（配置错误不该被伪装成空集合）。
	for _, bad := range []map[string]string{
		{"option.": "red"},          // 空属性 key
		{"option.color": "红"},       // 值含非法字符（属性值 key 是标识不是展示文本）
		{"option.color.sub": "red"}, // 子键里再带点（属性 key 本身不含点）
	} {
		if _, rerr := reg.ResolveCollection(ctx, productcontract.CollectionSourceProduct, bad); rerr == nil {
			t.Fatalf("非法维度 %v 应被拒绝", bad)
		}
	}
}

// TestProductCollectionOptionFilterUsesIndex 索引走查（issue #25）：
// 属性值筛选的 EXISTS 谓词要能吃上迁移 118 的 GIN(jsonb_path_ops) ——
// 谓词形态是 `option_values @> jsonb_build_object(...)`，正是这类索引支持的操作符。
func TestProductCollectionOptionFilterUsesIndex(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	// 用字面量常量代替参数：EXPLAIN 只看计划形状（参数化版本是同一个谓词）。
	assertPlanUsesIndex(t, f,
		"SELECT id FROM products WHERE EXISTS (SELECT 1 FROM product_variants v "+
			"WHERE v.product_id = products.id AND v.enabled "+
			"AND v.option_values @> '{\"color\":\"red\"}'::jsonb)",
		nil, "idx_product_variants_option_values")
}
