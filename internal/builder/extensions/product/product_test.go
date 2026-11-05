package product

import (
	"errors"
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/pkg/builder/core"
	"go_wp/internal/templates"
)

const testProjectID = "proj-1"

func buildViewFor(p *Props, content core.ContentResolver) (View, error) {
	return BuildView(p, content, testProjectID, "", nil)
}

func propsOf(t *testing.T, kv map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(kv)
	if err != nil {
		t.Fatalf("props 编码失败: %v", err)
	}
	return b
}

func nodeOf(t *testing.T, kv map[string]any) *core.Node {
	t.Helper()
	return &core.Node{ID: "p1", Type: Type, Props: propsOf(t, kv)}
}

func decodePropsOf(t *testing.T, kv map[string]any) Props {
	t.Helper()
	var p Props
	if err := json.Unmarshal(propsOf(t, kv), &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	return p
}

type stubResolver struct {
	values map[string]string
	errOn  string
}

func (s stubResolver) ResolveString(field string) (string, error) {
	if field == s.errOn {
		return "", errors.New("越界字段")
	}
	return s.values[field], nil
}

func TestValidateRequiresField(t *testing.T) {
	err := Widget.Validate(nodeOf(t, map[string]any{}), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "至少需要声明一个商品字段") {
		t.Fatalf("空槽位应被拒绝: %v", err)
	}
}

func TestValidateRejectsForeignSource(t *testing.T) {
	err := Widget.Validate(nodeOf(t, map[string]any{"titleField": "article.title"}), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "不属于数据源") {
		t.Fatalf("跨数据源字段应被拒绝: %v", err)
	}
}

func TestValidateRejectsBadPath(t *testing.T) {
	err := Widget.Validate(nodeOf(t, map[string]any{"titleField": "product."}), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "无效的字段路径") {
		t.Fatalf("非法字段路径应被拒绝: %v", err)
	}
}

func TestValidateAcceptsDeclaredFields(t *testing.T) {
	node := nodeOf(t, map[string]any{
		"titleField":   "product.name",
		"priceField":   "product.priceRange",
		"galleryField": "product.images",
	})
	if err := Widget.Validate(node, map[string]bool{}); err != nil {
		t.Fatalf("合法声明应通过: %v", err)
	}
}

func TestFieldBindingsReportsDeclared(t *testing.T) {
	node := nodeOf(t, map[string]any{
		"titleField":       "product.name",
		"descriptionField": "product.description",
	})
	refs, err := Widget.FieldBindings(node)
	if err != nil {
		t.Fatalf("自报字段绑定失败: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("应自报 2 条绑定，实际 %d: %+v", len(refs), refs)
	}
	for _, ref := range refs {
		if ref.EntityType != "product" || ref.Field == "" {
			t.Fatalf("绑定应为 product.<字段>: %+v", ref)
		}
	}
}

func TestBuildViewRendersDeclaredFields(t *testing.T) {
	images, err := json.Marshal([]string{"/storage/a.jpg", "/storage/b.jpg"})
	if err != nil {
		t.Fatalf("图集编码失败: %v", err)
	}
	p := decodePropsOf(t, map[string]any{
		"titleField":        "product.name",
		"subtitleField":     "product.subtitle",
		"mediaField":        "product.defaultImage",
		"galleryField":      "product.images",
		"priceField":        "product.priceRange",
		"comparePriceField": "product.comparePrice",
		"descriptionField":  "product.description",
	})
	resolver := stubResolver{values: map[string]string{
		"product.name":         "夏季衬衫",
		"product.subtitle":     "轻薄透气",
		"product.defaultImage": "/storage/a.jpg",
		"product.images":       string(images),
		"product.priceRange":   "99 ~ 199",
		"product.comparePrice": "259",
		"product.description":  "<p>纯棉</p>",
	}}
	view, err := buildViewFor(&p, resolver)
	if err != nil {
		t.Fatalf("BuildView 失败: %v", err)
	}
	if !view.HasTitle || view.Title != "夏季衬衫" {
		t.Fatalf("标题未填入: %+v", view)
	}
	if view.Price != "¥99 ~ 199" || view.ComparePrice != "¥259" {
		t.Fatalf("价格未按货币符号填入: %q / %q", view.Price, view.ComparePrice)
	}
	if len(view.Gallery) != 2 || view.TitleTag != "h2" {
		t.Fatalf("图集 / 标题层级不符: %+v", view)
	}
	if !view.HasDescription || !strings.Contains(view.DescriptionHTML, "纯棉") {
		t.Fatalf("描述未填入: %+v", view)
	}
}

func TestBuildViewRequiresResolver(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "product.name"})
	if _, err := buildViewFor(&p, nil); err == nil {
		t.Fatal("缺少内容解析器应报错")
	}
}

func optionProps(t *testing.T) Props {
	t.Helper()
	return decodePropsOf(t, map[string]any{
		"titleField":    "product.name",
		"optionsField":  "product.options",
		"variantsField": "product.variants",
	})
}

func TestValidateAcceptsOptionSlots(t *testing.T) {
	node := nodeOf(t, map[string]any{
		"titleField":    "product.name",
		"optionsField":  "product.options",
		"variantsField": "product.variants",
	})
	if err := Widget.Validate(node, map[string]bool{}); err != nil {
		t.Fatalf("规格槽位应通过校验: %v", err)
	}
	refs, err := Widget.FieldBindings(node)
	if err != nil {
		t.Fatalf("自报字段绑定失败: %v", err)
	}
	if len(refs) != 3 {
		t.Fatalf("应自报 3 条绑定（含规格两条），实际 %d: %+v", len(refs), refs)
	}
}

func TestBuildViewOptionsOnlyForMultipleCombinations(t *testing.T) {
	const options = `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红色"},{"key":"blue","label":"蓝色"}]}]`
	const oneCombo = `[{"sku":"a-1","price":"99","enabled":true,"options":{"color":"red"}}]`
	const twoCombos = `[{"sku":"a-1","price":"99","comparePrice":"199","enabled":true,"options":{"color":"red"}},
		{"sku":"a-2","price":"129","enabled":true,"options":{"color":"blue"}}]`

	t.Run("单变体不输出", func(t *testing.T) {
		p := optionProps(t)
		view, err := buildViewFor(&p, stubResolver{values: map[string]string{
			"product.options": options, "product.variants": oneCombo,
		}})
		if err != nil {
			t.Fatalf("BuildView 失败: %v", err)
		}
		if view.HasOptions {
			t.Fatalf("只有一个组合时不应输出规格选择器: %+v", view.VariantOptions)
		}
	})

	t.Run("多变体输出且组合可读", func(t *testing.T) {
		p := optionProps(t)
		view, err := buildViewFor(&p, stubResolver{values: map[string]string{
			"product.options": options, "product.variants": twoCombos,
		}})
		if err != nil {
			t.Fatalf("BuildView 失败: %v", err)
		}
		if !view.HasOptions {
			t.Fatal("两个组合应输出规格选择器")
		}
		if len(view.OptionGroups) != 1 || len(view.OptionGroups[0].Values) != 2 {
			t.Fatalf("规格维度解析不符: %+v", view.OptionGroups)
		}
		if len(view.VariantOptions) != 2 {
			t.Fatalf("组合行应有 2 条，实际 %d", len(view.VariantOptions))
		}
		first := view.VariantOptions[0]
		if first.Labels != "颜色 红色" || first.Price != "¥99" || first.ComparePrice != "¥199" {
			t.Fatalf("组合行文本/价格不符: %+v", first)
		}
		if view.VariantOptions[1].ComparePrice != "" {
			t.Fatalf("无划线价不应输出: %+v", view.VariantOptions[1])
		}
	})

	t.Run("无规格变体不进组合清单", func(t *testing.T) {

		p := optionProps(t)
		view, err := buildViewFor(&p, stubResolver{values: map[string]string{
			"product.options": options,
			"product.variants": `[{"sku":"a","price":"99","enabled":true,"options":{}},
				{"sku":"b","price":"99","enabled":true,"options":{}}]`,
		}})
		if err != nil {
			t.Fatalf("BuildView 失败: %v", err)
		}
		if view.HasOptions || len(view.VariantOptions) != 0 {
			t.Fatalf("无规格变体不应进入规格清单: %+v", view.VariantOptions)
		}
	})

	t.Run("未启用变体不参与", func(t *testing.T) {
		p := optionProps(t)
		view, err := buildViewFor(&p, stubResolver{values: map[string]string{
			"product.options": options,
			"product.variants": `[{"sku":"a","price":"99","enabled":true,"options":{"color":"red"}},
				{"sku":"b","price":"129","enabled":false,"options":{"color":"blue"}}]`,
		}})
		if err != nil {
			t.Fatalf("BuildView 失败: %v", err)
		}
		if view.HasOptions || len(view.VariantOptions) != 1 {
			t.Fatalf("未启用变体不应参与组合: %+v", view.VariantOptions)
		}
	})

	t.Run("缺少规格维度不输出", func(t *testing.T) {
		p := optionProps(t)
		view, err := buildViewFor(&p, stubResolver{values: map[string]string{
			"product.options": "[]", "product.variants": twoCombos,
		}})
		if err != nil {
			t.Fatalf("BuildView 失败: %v", err)
		}
		if view.HasOptions {
			t.Fatal("没有规格维度时不应输出选择器")
		}
	})

	t.Run("规格数据非法不阻断构建", func(t *testing.T) {
		p := optionProps(t)
		view, err := buildViewFor(&p, stubResolver{values: map[string]string{
			"product.options": "{不是 JSON", "product.variants": "也不是 JSON",
			"product.name": "夏季衬衫",
		}})
		if err != nil {
			t.Fatalf("规格数据形态不符不应让构建失败: %v", err)
		}
		if view.HasOptions || !view.HasTitle {
			t.Fatalf("非法规格数据应降级为不输出选择器，且其它字段照常: %+v", view)
		}
	})
}

func TestBuildViewPropagatesResolveError(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "product.bogus"})
	_, err := buildViewFor(&p, stubResolver{errOn: "product.bogus"})
	if err == nil || !strings.Contains(err.Error(), "product.bogus") {
		t.Fatalf("解析失败应上抛: %v", err)
	}
}

type productRenderCtx struct {
	Classes  string
	CustomID string
	NodeID   string
	V        View
}

func TestProductTemplateRendersVariantStockFragment(t *testing.T) {
	p := &Props{
		TitleField:    "product.name",
		OptionsField:  "product.options",
		VariantsField: "product.variants",
	}
	resolver := stubResolver{values: map[string]string{
		"product.name": "夏季衬衫",
		"product.options": `[{"key":"color","name":"颜色","values":[` +
			`{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`,
		"product.variants": `[{"id":"var-1","sku":"SKU-1","price":"99","enabled":true,` +
			`"options":{"color":"red"}},` +
			`{"id":"var-2","sku":"SKU-2","price":"109","enabled":true,` +
			`"options":{"color":"blue"}}]`,
	}}
	view, err := buildViewFor(p, resolver)
	if err != nil {
		t.Fatalf("BuildView: %v", err)
	}
	if len(view.VariantOptions) != 2 {
		t.Fatalf("应有 2 个规格组合行，实际 %d（%+v）", len(view.VariantOptions), view.VariantOptions)
	}
	if view.VariantOptions[0].ID != "var-1" || view.VariantOptions[1].ID != "var-2" {
		t.Fatalf("组合行应带上变体 id（片段请求参数），实际 %+v", view.VariantOptions)
	}

	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("NewEmbeddedComponentSet: %v", err)
	}
	tpl, err := set.GetTemplate("product")
	if err != nil {
		t.Fatalf("GetTemplate(product): %v", err)
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, nil, productRenderCtx{
		Classes: "sky-c-p1", NodeID: "p1", V: view,
	}); err != nil {
		t.Fatalf("渲染 product 模板失败: %v", err)
	}
	html := buf.String()
	for _, want := range []string{

		"/api/site/product/variants?projectId=proj-1&amp;variantIds=var-1",
		"data-site-poll=\"60\"",
		`aria-live="polite"`,
		"以结算时库存为准",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q\n%s", want, html)
		}
	}
}

func TestBuildViewFragmentURLsCarryRenderProject(t *testing.T) {
	const (
		projectID = "proj-9"
		options   = `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`
		twoCombos = `[{"id":"var-1","sku":"a-1","price":"99","enabled":true,"options":{"color":"red"}},
			{"id":"var-2","sku":"a-2","price":"129","enabled":true,"options":{"color":"blue"}}]`
	)
	p := optionProps(t)
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options": options, "product.variants": twoCombos,
	}}, projectID, "", nil)
	if err != nil {
		t.Fatalf("BuildView 失败: %v", err)
	}
	if len(view.VariantOptions) != 2 {
		t.Fatalf("应有两个组合行，实际 %+v", view.VariantOptions)
	}
	for _, vo := range view.VariantOptions {
		if !strings.Contains(vo.StockFactsURL, "projectId="+projectID) {
			t.Fatalf("可用量片段地址缺本次渲染的工程 %s: %q", projectID, vo.StockFactsURL)
		}
		if !strings.Contains(vo.PriceFactsURL, "projectId="+projectID) {
			t.Fatalf("价格核对片段地址缺本次渲染的工程 %s: %q", projectID, vo.PriceFactsURL)
		}
	}

	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("NewEmbeddedComponentSet: %v", err)
	}
	tpl, err := set.GetTemplate("product")
	if err != nil {
		t.Fatalf("GetTemplate(product): %v", err)
	}
	var buf strings.Builder
	if err := tpl.Execute(&buf, nil, productRenderCtx{Classes: "sky-c-p1", NodeID: "p1", V: view}); err != nil {
		t.Fatalf("渲染 product 模板失败: %v", err)
	}

	if !strings.Contains(buf.String(), `data-site-read="/api/site/product/variants?projectId=proj-9&amp;variantIds=var-1"`) {
		t.Fatalf("产物里的可用量片段地址没带工程:\n%s", buf.String())
	}
}

func TestBuildViewWithoutProjectOmitsFragmentURLs(t *testing.T) {
	p := optionProps(t)
	view, err := BuildView(&p, stubResolver{values: map[string]string{
		"product.options": `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`,
		"product.variants": `[{"id":"var-1","sku":"a-1","price":"99","enabled":true,"options":{"color":"red"}},
			{"id":"var-2","sku":"a-2","price":"129","enabled":true,"options":{"color":"blue"}}]`,
	}}, "", "", nil)
	if err != nil {
		t.Fatalf("BuildView 失败: %v", err)
	}
	if len(view.VariantOptions) == 0 {
		t.Fatalf("应仍有组合行（缺工程只影响片段位）")
	}
	for _, vo := range view.VariantOptions {
		if vo.StockFactsURL != "" || vo.PriceFactsURL != "" {
			t.Fatalf("缺工程时不应烘片段地址: %+v", vo)
		}
	}
	if attrs, _ := view.DeclareFeatures(); len(attrs) != 0 {
		t.Fatalf("没有片段位时不该登记 hx-* 属性，实际 %+v", attrs)
	}
}
