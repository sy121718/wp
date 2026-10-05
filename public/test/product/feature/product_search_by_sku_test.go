// Package feature product 模块 feature 测试 —— 列表关键词按 SKU 搜索。
//
// 为什么单独钉这一条：`product_find`（给 AI 用的工具）的说明写着「关键词匹配
// 商品名称与 SKU」，而底层 `ProductModel.List` 当时只有 `name ILIKE ?` ——
// **工具对模型承诺了它做不到的事**。用户说「SKU 是 W1_CUP001 那个，查一下」时
// 模型查不到，而说明让它以为自己查过了，于是它要么编一个答案、要么换个词反复试。
//
// 同一处还有第二个缺陷：`List` 的 Select 没取 `sku_code`，所以即便按名字搜到了，
// 工具返回的「SKU=」也永远是空串 —— 用户从回答里看不出哪个才是他要的那个商品。
//
// 两个都钉住：能搜到（过滤生效）、能看到（字段取出来）。
package feature

import (
	"context"
	"testing"

	productdto "go_wp/internal/module/product/dto"
)

// createProductWithSKU 建一个带指定主体 SKU 的商品，返回 id。
//
// 不走 detailFixture.createProduct：那个助手只给变体 SKU，而这里要验的是
// **商品主体**的 sku_code（products.sku_code，迁移 246）。
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

	// 用户报的是 SKU，不是名字：关键词用 SKU 里的一段。
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
	// 搜到了还不够 —— 返回里必须带上 SKU，否则模型无法把「这条就是用户说的那个」
	// 说清楚，用户也无从核对。
	if list[0].SKUCode != "W1_CUP001" {
		t.Fatalf("列表返回里 SKU 为空/不对：%q（List 的 Select 漏了 sku_code 就会这样）", list[0].SKUCode)
	}
	// 类型同理：它决定「这件商品是不是捆绑容器」，模型据此才知道要不要去找成员。
	// 少取一列时这里会静默变空串 —— product_find 的输出格式里写着「类型=…」。
	if list[0].Type != "variant" {
		t.Fatalf("列表返回里类型为空/不对：%q（Select 漏了 type 就会这样）", list[0].Type)
	}
}

// 按名字仍然能搜（原有能力不能因为加了 SKU 而退化）。
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

// 计数与列表用同一条过滤：两者分叉时，分页徽章的总数会与实际能翻到的条数对不上
// （看起来只是「数字不准」，很难联想到是过滤条件写了两份）。
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
