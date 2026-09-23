package product

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// testProjectID 单测用的站点工程 id。
//
// 组件单测本身不关心工程上下文，但两个片段位（实时价格核对 / 实时可用量）的地址里
// **必须**带工程 id —— 片段侧把它当必填参数，缺它直接回 500 而页面上看不出异常。
// 收敛成一个常量：每条用例各写一个字面量，改口径时必然漏改一处。
const testProjectID = "proj-1"

// buildViewFor 补上构建期工程 id 的 BuildView 包装（用例只关心组件本身时用它）；
// 专门断言片段地址的用例直接调 BuildView 并显式给出工程 id。
func buildViewFor(p *Props, content core.ContentResolver) (View, error) {
	return BuildView(p, content, testProjectID, "", nil)
}

// propsOf 把 props 键值编码为节点 props JSON（避免测试里手写 JSON 字符串）。
func propsOf(t *testing.T, kv map[string]any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(kv)
	if err != nil {
		t.Fatalf("props 编码失败: %v", err)
	}
	return b
}

// nodeOf 构造商品组件节点。
func nodeOf(t *testing.T, kv map[string]any) *core.Node {
	t.Helper()
	return &core.Node{ID: "p1", Type: Type, Props: propsOf(t, kv)}
}

// decodePropsOf 解码 props 为组件 Props。
func decodePropsOf(t *testing.T, kv map[string]any) Props {
	t.Helper()
	var p Props
	if err := json.Unmarshal(propsOf(t, kv), &p); err != nil {
		t.Fatalf("props 解码失败: %v", err)
	}
	return p
}

// stubResolver 固定字段值映射的解析器（替代构建期的商品解析器）。
type stubResolver struct {
	values map[string]string
	errOn  string
}

// ResolveString 实现 core.ContentResolver。
func (s stubResolver) ResolveString(field string) (string, error) {
	if field == s.errOn {
		return "", errors.New("越界字段")
	}
	return s.values[field], nil
}

// TestValidateRequiresField 未声明任何商品字段（全空槽位）必须被拒绝。
func TestValidateRequiresField(t *testing.T) {
	err := Widget.Validate(nodeOf(t, map[string]any{}), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "至少需要声明一个商品字段") {
		t.Fatalf("空槽位应被拒绝: %v", err)
	}
}

// TestValidateRejectsForeignSource 声明其它数据源的字段必须被拒绝。
func TestValidateRejectsForeignSource(t *testing.T) {
	err := Widget.Validate(nodeOf(t, map[string]any{"titleField": "article.title"}), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "不属于数据源") {
		t.Fatalf("跨数据源字段应被拒绝: %v", err)
	}
}

// TestValidateRejectsBadPath 非法字段路径必须被拒绝。
func TestValidateRejectsBadPath(t *testing.T) {
	err := Widget.Validate(nodeOf(t, map[string]any{"titleField": "product."}), map[string]bool{})
	if err == nil || !strings.Contains(err.Error(), "无效的字段路径") {
		t.Fatalf("非法字段路径应被拒绝: %v", err)
	}
}

// TestValidateAcceptsDeclaredFields 合法声明通过校验（槽位留空 = 不需要该字段）。
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

// TestFieldBindingsReportsDeclared 组件自报声明的字段绑定（供注册表白名单校验）。
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

// TestBuildViewRendersDeclaredFields 声明字段经解析器静态填入视图：价格带货币符号、
// 图集解析 JSON 数组、描述走富文本清洗。
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

// TestBuildViewRequiresResolver 声明了字段却没有解析器时必须报错（不静默出空块）。
func TestBuildViewRequiresResolver(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "product.name"})
	if _, err := buildViewFor(&p, nil); err == nil {
		t.Fatal("缺少内容解析器应报错")
	}
}

// optionProps 声明规格槽位的 props（规格选择器相关用例共用）。
func optionProps(t *testing.T) Props {
	t.Helper()
	return decodePropsOf(t, map[string]any{
		"titleField":    "product.name",
		"optionsField":  "product.options",
		"variantsField": "product.variants",
	})
}

// TestValidateAcceptsOptionSlots 规格槽位与其它槽位同规则：路径合法即通过校验。
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

// TestBuildViewOptionsOnlyForMultipleCombinations 验收（issue #8）：
// 规格选择器只在「有规格维度且可展示组合 ≥2」时输出 —— 单变体商品不输出。
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
		// 两个变体但都没有规格（商品占位 / 手工新增）→ 没有可选择的组合，不输出选择器。
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

// TestBuildViewPropagatesResolveError 解析器报错（越界字段）必须上抛终止构建。
func TestBuildViewPropagatesResolveError(t *testing.T) {
	p := decodePropsOf(t, map[string]any{"titleField": "product.bogus"})
	_, err := buildViewFor(&p, stubResolver{errOn: "product.bogus"})
	if err == nil || !strings.Contains(err.Error(), "product.bogus") {
		t.Fatalf("解析失败应上抛: %v", err)
	}
}

// productRenderCtx 模板渲染上下文（与装配层的 nodeView 字段同名，本包测试自带一份）。
type productRenderCtx struct {
	Classes  string
	CustomID string
	NodeID   string
	V        View
}

// TestProductTemplateRendersVariantStockFragment 验收（issue #24）：
// 规格组合行携带「实时可用量片段」的请求参数（变体 id）与构建期兜底文案。
//
// 库存是运行期真源，构建期只能把变体 id 烘进产物（可用量每次请求现取）；
// 无 JS / HTMX 不可用时页面留下的是兜底文案，选择器本身仍是原生 radio（键盘可达）。
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
		// 工程 id 必须在地址里（片段侧当必填参数）：Jet 把属性值里的 & 转义成 &amp;，
		// htmx 读到的仍是 & —— 断言按产物字节写，才能同时钉住「参数没被丢掉」。
		"/_fragments/productVariantAvailability?projectId=proj-1&amp;variantIds=var-1",
		"hx-trigger=\"load, every 60s\"",
		`aria-live="polite"`,
		"以结算时库存为准", // 无脚本时的兜底文案（降级策略）
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("产物缺少 %q\n%s", want, html)
		}
	}
}

// TestBuildViewFragmentURLsCarryRenderProject 两个片段位的地址都必须带**本次渲染所用**的工程 id。
//
// 与 orderlist 组件同一条不变量：runtimefragment 的 productVariantAvailability / productLivePrice
// 都把 projectId 当**必填**参数（缺它无法定位变体所属工程），而 runtimefragment.Request 里
// 没有工程字段 —— 工程只能由构建期烘进 URL。缺它时片段端报「缺少参数」回 500，
// htmx 换不动目标节点：页面上看不出任何异常，库存位永远停在构建期兜底文案、价格核对位永远空白。
//
// 断言两件事：地址里有工程参数，且**值等于本次渲染传入的工程**。只断言「有 projectId 字样」
// 是不够的 —— 把工程写死成一个常量也能过，而那样每张页面都会去核对别的工程的价。
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
		if !strings.Contains(vo.StockAvailabilityGet, "projectId="+projectID) {
			t.Fatalf("可用量片段地址缺本次渲染的工程 %s: %q", projectID, vo.StockAvailabilityGet)
		}
		if !strings.Contains(vo.LivePriceGet, "projectId="+projectID) {
			t.Fatalf("价格核对片段地址缺本次渲染的工程 %s: %q", projectID, vo.LivePriceGet)
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
	// 产物里是 HTML 转义后的 &amp;（Jet 默认转义属性值，htmx 读到的仍是 &）。
	if !strings.Contains(buf.String(), `hx-get="/_fragments/productVariantAvailability?projectId=proj-9&amp;variantIds=var-1"`) {
		t.Fatalf("产物里的可用量片段地址没带工程:\n%s", buf.String())
	}
}

// TestBuildViewWithoutProjectOmitsFragmentURLs 缺工程 id 时不烘片段地址。
//
// pipeline.DefaultCompile 这类「不解析站点级资源」的编译路径拿不到工程。此时若照旧拼 URL，
// 片段端会判「缺少参数」回 500，每次页面加载都是一个失败请求 —— 所以两个片段位都退化为空 URL
// （模板不输出 hx-get），库存位只剩构建期兜底文案。
//
// 同时钉住登记：没有 hx-get 就不该声明 hx-* 属性，否则产物会白白多注入一份 htmx 脚本。
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
		if vo.StockAvailabilityGet != "" || vo.LivePriceGet != "" {
			t.Fatalf("缺工程时不应烘片段地址: %+v", vo)
		}
	}
	if attrs, _ := view.DeclareFeatures(); len(attrs) != 0 {
		t.Fatalf("没有片段位时不该登记 hx-* 属性，实际 %+v", attrs)
	}
}
