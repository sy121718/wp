package feature

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

func createProductWithSKU(t *testing.T, f *detailFixture, name, slug, sku string) string {
	t.Helper()
	res, err := f.products.Create(context.Background(), &productdto.CreateReq{
		ProjectID: f.projectID,
		Name:      name,
		Slug:      slug,
		SKUCode:   sku,
	})
	if err != nil {
		t.Fatalf("创建商品失败: %v", err)
	}
	return res.ID
}

func TestProductListSearchMatchesSKUCode(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	target := createProductWithSKU(t, f, "磨砂保温杯", "frosted-cup", "W1_CUP001")
	createProductWithSKU(t, f, "另一件商品", "other-thing", "W1_ZZZ999")

	list, err := f.products.List(ctx, &productdto.ListReq{
		ProjectID: f.projectID, Keyword: "CUP001", Size: 50,
	})
	if err != nil {
		t.Fatalf("按 SKU 搜索失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("按 SKU 搜索应命中 1 条，实得 %d 条", len(list))
	}
	if list[0].ID != target {
		t.Fatalf("命中的不是目标商品：%s（期望 %s）", list[0].ID, target)
	}

	if list[0].SKUCode != "W1_CUP001" {
		t.Fatalf("列表返回里 SKU 为空/不对：%q（List 的 Select 漏了 sku_code 就会这样）", list[0].SKUCode)
	}
}

func TestProductListSearchStillMatchesName(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	createProductWithSKU(t, f, "磨砂保温杯", "frosted-cup", "W1_CUP001")
	createProductWithSKU(t, f, "另一件商品", "other-thing", "W1_ZZZ999")

	list, err := f.products.List(ctx, &productdto.ListReq{
		ProjectID: f.projectID, Keyword: "保温杯", Size: 50,
	})
	if err != nil {
		t.Fatalf("按名称搜索失败: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("按名称搜索应命中 1 条，实得 %d 条", len(list))
	}
}

func TestProductListCountAgreesWithSKUSearch(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	ctx := context.Background()
	createProductWithSKU(t, f, "磨砂保温杯", "frosted-cup", "W1_CUP001")
	createProductWithSKU(t, f, "另一件商品", "other-thing", "W1_ZZZ999")

	list, err := f.products.List(ctx, &productdto.ListReq{
		ProjectID: f.projectID, Keyword: "W1_", Size: 50,
	})
	if err != nil {
		t.Fatalf("按 SKU 前缀搜索失败: %v", err)
	}
	total, err := f.products.CountProducts(ctx, &productdto.ListReq{
		ProjectID: f.projectID, Keyword: "W1_",
	})
	if err != nil {
		t.Fatalf("计数失败: %v", err)
	}
	if int(total) != len(list) {
		t.Fatalf("计数与列表不一致：Count=%d，List=%d（两者必须共用同一条过滤）", total, len(list))
	}
	if total != 2 {
		t.Fatalf("两个商品都以 W1_ 开头，应数出 2，实得 %d", total)
	}
}
