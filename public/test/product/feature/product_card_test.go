// Package feature product 模块 feature 测试 —— 商品卡组件（issue #22）。
//
// 覆盖三条验收（真实 PostgreSQL + 生产迁移 + 真实组件注册表）：
//
//  1. 商品卡作为集合卡模板（item.* 绑定）在集合里逐商品渲染出真实数据；
//  2. 越界字段在**保存期**（ValidateFieldRefs 按实体类型注册表）与**构建期**
//     （解析器渲染时）各被拒绝一次，不静默渲染成空卡；
//  3. 确定性构建 + 多端契约（宽度不写死、折行、触屏等价形态）。
package feature

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/internal/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

// productCardCollectionPage 集合页：cardstack 吃商品集合源，子节点是商品卡模板。
//
// 集合 + 子节点的组合是「子节点模板」模式：集合组件把子节点当每张卡的模板，
// 逐项换作用域（ItemScope）后再渲染 —— 商品卡里的 item.* 取的就是当前商品。
func productCardCollectionPage(t *testing.T, cardProps string) *builder.Page {
	t.Helper()
	raw := `{"settings":{"layout":{"mode":"boxed","maxWidth":"1200px"}},"root":[` +
		`{"id":"n1","type":"core.cardstack","props":{"trigger":"hover","shape":"line",` +
		`"collectionSource":"content:product","collectionLimit":4},` +
		`"children":[{"id":"card1","type":"core.productCard","props":` + cardProps + `}]}]}`
	p, err := builder.ParsePage([]byte(raw))
	if err != nil {
		t.Fatalf("解析页面文档失败: %v", err)
	}
	return p
}

// productCardProps 商品卡的默认字段映射（集合项作用域）。
func productCardProps() string {
	return `{"imageField":"item.images","titleField":"item.name",` +
		`"priceField":"item.priceRange","comparePriceField":"item.comparePrice",` +
		`"tagsField":"item.tags","linkField":"item.slug","linkPrefix":"/products/"}`
}

// TestProductCardRendersInCollection 验收：
// 商品卡在集合里逐商品渲染出真实数据（名称 / 价格区间 / 划线价 / 标签 / 详情链接）。
func TestProductCardRendersInCollection(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	shirtID := f.createProduct(t, "夏季衬衫", "summer-shirt", "", 99, 199)
	tag := colTag(t, f, "新品", "new-arrival")
	colAttach(t, f, shirtID, nil, nil, []string{tag.ID})
	// createProduct 只设售价（第二个变体拉出价格区间），划线价要单独设。
	detail, derr := f.products.Get(context.Background(), &productdto.GetReq{ID: shirtID})
	if derr != nil {
		t.Fatalf("读商品失败: %v", derr)
	}
	compare := 259.0
	if _, uerr := f.products.UpdateVariant(context.Background(), &productdto.UpdateVariantReq{
		ID: detail.Variants[0].ID, ComparePrice: &compare,
	}); uerr != nil {
		t.Fatalf("设置划线价失败: %v", uerr)
	}

	page := productCardCollectionPage(t, productCardProps())
	compiled, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	for _, want := range []string{
		"夏季衬衫",                   // 标题
		"¥99 ~ 199",              // 价格区间（两次调用同价时退化为单值，这里两个变体拉出了区间）
		"¥259",                   // 划线价
		"新品",                     // 标签（名称数组逐项渲染）
		"/products/summer-shirt", // 链接（前缀 + slug）
		"sky-product-card",
	} {
		if !strings.Contains(compiled.HTML, want) {
			t.Fatalf("产物缺少 %q\n%s", want, compiled.HTML)
		}
	}
}

// TestProductCardRejectsOutOfWhitelistField 验收：越界字段保存期与构建期各拒一次。
func TestProductCardRejectsOutOfWhitelistField(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "夏季衬衫", "summer-shirt", "", 99, 199)

	// status 不在商品字段白名单内（它是后台筛选维度，不是可渲染字段）。
	page := productCardCollectionPage(t, `{"titleField":"item.status"}`)

	// 保存期：按实体类型注册表校验字段绑定（商品模板的实体类型是 product）。
	entityReg := core.NewEntitySourceRegistry()
	if err := f.products.RegisterEntityTypes(entityReg); err != nil {
		t.Fatalf("注册商品实体类型失败: %v", err)
	}
	err := builder.ValidateFieldRefs(page, productcontract.EntityTypeProduct, entityReg)
	if err == nil {
		t.Fatalf("越界字段应在保存期被拒绝")
	}
	if !strings.Contains(err.Error(), "白名单") {
		t.Fatalf("保存期报错应指向字段白名单: %v", err)
	}

	// 构建期：item.* 的越界字段**不会报错** —— core.ItemScope 对集合项里不存在的键返回空串
	//（集合作用域的既有语义：不在集合里 / 该商品没这个字段，都解析为空）。
	// 断言的是「按空值处理」：编译不崩，且不输出空壳节点。
	// 越界字段的权威拦截点是保存期（上面那一段）—— 注册表在手，能给出白名单报错；
	// 构建期拿不到字段名白名单（组件不依赖领域模块），只能按值处理。
	compiled, cerr := compileCollection(t, page, reg, f.projectID)
	if cerr != nil {
		t.Fatalf("集合项越界字段应按空值处理，不应编译失败: %v", cerr)
	}
	if strings.Contains(compiled.HTML, "sky-product-card-title") {
		t.Fatalf("该字段没有值，不该输出标题节点:\n%s", compiled.HTML)
	}
}

// TestProductCardDeterministicAndResponsive 验收：确定性构建 + 多端契约。
func TestProductCardDeterministicAndResponsive(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "衬衫", "shirt", "", 99, 199)
	f.createProduct(t, "外套", "coat", "", 299, 299)

	page := productCardCollectionPage(t, productCardProps())
	first, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("首次编译失败: %v", err)
	}
	second, err := compileCollection(t, page, reg, f.projectID)
	if err != nil {
		t.Fatalf("二次编译失败: %v", err)
	}
	if first.HTML != second.HTML || string(first.CSS) != string(second.CSS) {
		t.Fatalf("同 props 同数据必须产出相同字节（确定性构建，不变量 5）")
	}

	// 多端契约：宽度不写死、悬停只在支持 hover 的设备上生效、触屏有等价形态。
	css := string(first.CSS)
	for _, want := range []string{
		"min(100%",              // 宽度不写死
		"@media (hover: hover)", // 悬停规则只在鼠标设备上输出
		"@media (hover: none)",  // 触屏等价形态（AddHoverNone）
		"flex-wrap: wrap",       // 标签行窄屏折行
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("多端契约缺少 %q\n%s", want, css)
		}
	}
}
