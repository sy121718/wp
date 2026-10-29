package builder

import (
	"bytes"
	"go_wp/internal/builder/core"
	json "go_wp/pkg/jsonx"
	"testing"
)

func TestLegacySiteSlotConversionIsExplicitAtomicAndReplayable(t *testing.T) {
	raw := json.RawMessage(`{"settings":{},"root":[{"id":"list","type":"core.productList","props":{"collectionSource":"content:product"}},{"id":"buy","type":"core.addToCart","props":{"source":"product","buttonText":"购买","variantMode":"perVariant","showQuantity":true}}]}`)
	before := append([]byte(nil), raw...)
	result, err := ConvertLegacySiteSlots(raw, map[string]string{"shop": "shop-page"}, LegacyProductContext{ProductID: "current-product"})
	if err != nil || len(result.Conflicts) > 0 || len(result.Changes) != 2 {
		t.Fatalf("唯一已确认映射应转换: %+v %v", result, err)
	}
	if !bytes.Equal(raw, before) {
		t.Fatal("纯转换不得修改原输入")
	}
	doc, err := ParsePage(result.Document)
	if err != nil {
		t.Fatal(err)
	}
	if got := doc.Root[0].Actions["list"]; got.Kind != core.ActionPage || got.PageID != "shop-page" {
		t.Fatalf("应转换成页面引用而非路径: %+v", got)
	}
	var props map[string]any
	if err := json.Unmarshal(doc.Root[1].Props, &props); err != nil {
		t.Fatal(err)
	}
	if props["label"] != "购买" || props["showQuantity"] != true || props["variantMode"] != nil || props["source"] != nil {
		t.Fatalf("仅保留作者外观与数量，买家规格由当前商品提供: %v", props)
	}
	again, err := ConvertLegacySiteSlots(result.Document, map[string]string{"shop": "different"}, LegacyProductContext{ProductID: "current-product"})
	if err != nil || len(again.Changes) != 0 || !bytes.Equal(again.Document, result.Document) {
		t.Fatalf("重放不得改写显式动作或重复转换: %+v %v", again, err)
	}
	for _, tc := range []struct {
		name    string
		targets map[string]string
		product string
	}{
		{"缺少旧唯一目标", nil, "current-product"},
		{"普通页没有当前商品", map[string]string{"shop": "shop-page"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ConvertLegacySiteSlots(raw, tc.targets, LegacyProductContext{ProductID: tc.product})
			if err != nil || len(got.Conflicts) != 1 || len(got.Document) != 0 {
				t.Fatalf("冲突不得交出半转换文档: %+v %v", got, err)
			}
		})
	}
}

func TestLegacySpecifiedProductConflictRemainsVisible(t *testing.T) {
	raw := json.RawMessage(`{"root":[{"id":"old","type":"core.addToCart","props":{"productId":"another-product","source":"product"}}]}`)
	got, err := ConvertLegacySiteSlots(raw, nil, LegacyProductContext{ProductID: "current-product"})
	if err != nil || len(got.Conflicts) != 1 || got.Conflicts[0].NodeID != "old" || len(got.Document) != 0 {
		t.Fatalf("不能静默改变旧指定商品归属: %+v %v", got, err)
	}
}

func TestLegacyProductDetailTemplateRetainsBuyerVariantChoice(t *testing.T) {
	raw := json.RawMessage(`{"root":[{"id":"buy","type":"core.addToCart","props":{"source":"product","optionsField":"product.options","variantsField":"product.variants","variantMode":"perVariant","showQuantity":true}}]}`)
	got, err := ConvertLegacySiteSlots(raw, nil, LegacyProductContext{ProductDetailTemplate: true})
	if err != nil || len(got.Conflicts) != 0 || len(got.Changes) != 1 {
		t.Fatalf("商品详情模板应保留当前商品买家选规格语义: %+v %v", got, err)
	}
}
