package feature

import (
	"context"
	"strings"
	"testing"

	"go_wp/internal/builder"
	"go_wp/pkg/builder/core"
	productcontract "go_wp/internal/module/product/contract"
	productdto "go_wp/internal/module/product/dto"
)

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

func productCardProps() string {
	return `{"imageField":"item.images","titleField":"item.name",` +
		`"priceField":"item.priceRange","comparePriceField":"item.comparePrice",` +
		`"tagsField":"item.tags","linkField":"item.slug","linkPrefix":"/products/"}`
}

func TestProductCardRendersInCollection(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	shirtID := f.createProduct(t, "夏季衬衫", "summer-shirt", "", 99, 199)
	tag := colTag(t, f, "新品", "new-arrival")
	colAttach(t, f, shirtID, nil, nil, []string{tag.ID})

	detail, derr := f.products.Get(context.Background(), &productdto.GetReq{ProjectID: f.projectID, ID: shirtID})
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
		"夏季衬衫",
		"¥99 ~ 199",
		"¥259",
		"新品",
		"/products/summer-shirt",
		"sky-product-card",
	} {
		if !strings.Contains(compiled.HTML, want) {
			t.Fatalf("产物缺少 %q\n%s", want, compiled.HTML)
		}
	}
}

func TestProductCardRejectsOutOfWhitelistField(t *testing.T) {
	f := newDetailFixture(t)
	if f == nil {
		return
	}
	reg := f.collectionRegistry(t)
	f.createProduct(t, "夏季衬衫", "summer-shirt", "", 99, 199)

	page := productCardCollectionPage(t, `{"titleField":"item.status"}`)

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

	compiled, cerr := compileCollection(t, page, reg, f.projectID)
	if cerr != nil {
		t.Fatalf("集合项越界字段应按空值处理，不应编译失败: %v", cerr)
	}
	if strings.Contains(compiled.HTML, "sky-product-card-title") {
		t.Fatalf("该字段没有值，不该输出标题节点:\n%s", compiled.HTML)
	}
}

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

	css := string(first.CSS)
	for _, want := range []string{
		"min(100%",
		"@media (hover: hover)",
		"@media (hover: none)",
		"flex-wrap: wrap",
	} {
		if !strings.Contains(css, want) {
			t.Fatalf("多端契约缺少 %q\n%s", want, css)
		}
	}
}
