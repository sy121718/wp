package productservice

// product_type_test.go — 商品类型（迁移 238）与价格区间派生。
//
// 这两条是「捆绑商品只有容器价」的最小护栏：类型归一化决定创建路径的分支，
// 价格区间决定商品列表/详情显示什么价。两者都有失败能力 ——
// 把 bundle 分支去掉，下面第二条用例立刻红（区间会退回变体派生，也就是 0.00）。

import (
	"testing"

	productdto "go_wp/internal/module/product/dto"
	productenums "go_wp/internal/module/product/enums"
	productmodel "go_wp/internal/module/product/model"
)

func TestNormalizeProductType(t *testing.T) {
	ok := []struct {
		raw  string
		want string
	}{
		{"", productmodel.TypeVariant},
		{"  ", productmodel.TypeVariant},
		{"variant", productmodel.TypeVariant},
		{" bundle ", productmodel.TypeBundle},
	}
	for _, tc := range ok {
		got, err := normalizeProductType(tc.raw)
		if err != nil {
			t.Fatalf("normalizeProductType(%q) 不应报错: %v", tc.raw, err)
		}
		if got != tc.want {
			t.Fatalf("normalizeProductType(%q) = %q，期望 %q", tc.raw, got, tc.want)
		}
	}
	// 未知类型必须拒绝，而不是静默当普通商品 —— 静默会让「捆绑」在创建那一刻消失。
	if _, err := normalizeProductType("combo"); err == nil || err.Error() != productenums.ErrProductTypeInvalid {
		t.Fatalf("未知类型应报 %s，实际 %v", productenums.ErrProductTypeInvalid, err)
	}
}

func TestApplyPriceRange(t *testing.T) {
	price := func(v float64) *float64 { return &v }
	variant := func(p float64) *productmodel.VariantEntity {
		return &productmodel.VariantEntity{Price: p}
	}

	t.Run("变体商品取变体区间", func(t *testing.T) {
		resp := &productdto.ProductResp{Type: productmodel.TypeVariant}
		applyPriceRange(resp, []*productmodel.VariantEntity{variant(30), variant(10), variant(20)})
		if resp.PriceMin != 10 || resp.PriceMax != 30 || resp.VariantCount != 3 {
			t.Fatalf("区间应为 10~30（3 个变体），实际 %.2f~%.2f（%d 个）", resp.PriceMin, resp.PriceMax, resp.VariantCount)
		}
	})

	t.Run("捆绑容器取容器价且忽略变体", func(t *testing.T) {
		resp := &productdto.ProductResp{Type: productmodel.TypeBundle, DefaultPrice: price(88.5)}
		// 传一个价 0 的变体：捆绑容器不该有变体，即便传进来也不能影响对外价格。
		applyPriceRange(resp, []*productmodel.VariantEntity{variant(0)})
		if resp.PriceMin != 88.5 || resp.PriceMax != 88.5 {
			t.Fatalf("捆绑商品价格应取容器价 88.5，实际 %.2f~%.2f", resp.PriceMin, resp.PriceMax)
		}
	})

	t.Run("捆绑容器无价时保持零值", func(t *testing.T) {
		resp := &productdto.ProductResp{Type: productmodel.TypeBundle}
		applyPriceRange(resp, nil)
		if resp.PriceMin != 0 || resp.PriceMax != 0 {
			t.Fatalf("容器价缺失应保持 0（创建/保存路径已用 ErrBundlePriceRequired 拦住），实际 %.2f~%.2f", resp.PriceMin, resp.PriceMax)
		}
	})
}
