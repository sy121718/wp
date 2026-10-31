package builder

import (
	json "go_wp/pkg/jsonx"
	"sort"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

var crosscheckUI = struct {
	sources map[string]string
	css     string
}{sources: crosscheckUISources(), css: "/* ui css */"}

func crosscheckUISources() map[string]string {
	sources := uiSrcForTest()
	for _, name := range []string{"drawer.js", "confirm.js", "colorfield.js", "iconfield.js", "themetoggle.js"} {
		sources[name] = "/* " + name + " */ WBUI.register(function(){});"
	}
	return sources
}

func assertFeatureCrosscheck(t *testing.T, cp *CompiledPage) {
	t.Helper()
	if cp == nil {
		t.Fatal("编译结果为空")
	}
	if cp.Features == nil {
		t.Fatal("编译结果没有特征登记表：Compile 应总是带上 Features（即使为空集）")
	}
	reg := cp.featureScan()
	tok := collectHTMLScan(cp.HTML)

	for _, name := range sortedFeatureKeys(reg.attrs) {
		if _, ok := tok.attrs[name]; !ok {
			t.Errorf("登记了产物里不存在的属性 %q（上报条件与模板输出分叉）\n登记集合: %v", name, sortedFeatureKeys(reg.attrs))
		}
	}
	for _, name := range sortedFeatureKeys(reg.classes) {
		if _, ok := tok.classes[name]; !ok {
			t.Errorf("登记了产物里不存在的 class %q（上报条件与模板输出分叉）\n登记集合: %v", name, sortedFeatureKeys(reg.classes))
		}
	}

	for _, name := range sortedFeatureKeys(relevantAttrs(tok.attrs)) {
		if _, ok := reg.attrs[name]; !ok {
			t.Errorf("漏登记属性 %q：tokenize 看到它（会触发脚本注入），登记路径没有\n产物 HTML:\n%s", name, cp.HTML)
		}
	}
	baseClasses := uiBaseClassSet()
	for _, name := range sortedFeatureKeys(tok.classes) {
		if baseClasses[name] {
			if _, ok := reg.classes[name]; !ok {
				t.Errorf("漏登记控件外观 class %q：tokenize 看到它（会触发 ui.css 注入），登记路径没有\n产物 HTML:\n%s", name, cp.HTML)
			}
		}
	}

	probe := *cp
	probe.UISources = crosscheckUI.sources
	probe.UIStyle = crosscheckUI.css
	cssReg, jsReg, errReg := uiAssetsForScan(cp.featureScan(), probe.UIStyle, probe.UISources)
	cssTok, jsTok, errTok := uiAssetsForScan(collectHTMLScan(cp.HTML), probe.UIStyle, probe.UISources)
	if (errReg == nil) != (errTok == nil) {
		t.Fatalf("两条路径的组装结果不一致：登记 err=%v，tokenize err=%v\n产物 HTML:\n%s", errReg, errTok, cp.HTML)
	}
	if errReg != nil {
		t.Fatalf("组装控件资源失败: %v", errReg)
	}
	if cssReg != cssTok {
		t.Errorf("CSS 注入结果不一致\n登记:     %q\ntokenize: %q\n登记集合: %v\ntokenize 集合: %v",
			cssReg, cssTok, sortedFeatureKeys(reg.attrs), sortedFeatureKeys(tok.attrs))
	}
	if jsReg != jsTok {
		t.Errorf("脚本注入结果不一致\n登记:     %q\ntokenize: %q\n登记集合: %v\ntokenize 集合: %v",
			jsReg, jsTok, sortedFeatureKeys(reg.attrs), sortedFeatureKeys(tok.attrs))
	}

	if cp.EnhanceSource != "" {
		regEnh := enhanceScriptFor(reg.attrs, cp.EnhanceSource)
		tokEnh := enhanceScriptFor(tok.attrs, cp.EnhanceSource)
		if regEnh != tokEnh {
			t.Errorf("增强块选择不一致\n登记:     %q\ntokenize: %q\n登记集合: %v\ntokenize 集合: %v",
				regEnh, tokEnh, sortedFeatureKeys(reg.attrs), sortedFeatureKeys(tok.attrs))
		}
	}
}

func sortedFeatureKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func crosscheckSet(t testing.TB) *jet.Set {
	t.Helper()
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	return set
}

func crosscheckEnhanceSource(t testing.TB) string {
	t.Helper()
	js, err := templates.StaticJS("enhance.js")
	if err != nil {
		t.Fatalf("读取 enhance.js 失败: %v", err)
	}
	if strings.TrimSpace(js) == "" {
		t.Fatal("enhance.js 为空")
	}
	return js
}

func crosscheckCompile(t testing.TB, set *jet.Set, nodes []*core.Node, opts ...CompileOption) *CompiledPage {
	t.Helper()
	doc, err := json.Marshal(map[string]any{
		"settings": map[string]any{"layout": map[string]any{"mode": "full"}},
		"root":     nodes,
	})
	if err != nil {
		t.Fatalf("序列化文档失败: %v", err)
	}
	page, err := ParsePage(doc)
	if err != nil {
		t.Fatalf("文档解析失败: %v", err)
	}
	all := append([]CompileOption{
		WithComponentSet(set),
		WithContentResolver(paletteContentResolver{}),
		WithUISources(crosscheckUI.sources),
		WithUIStyle(crosscheckUI.css),
		WithEnhanceSource(crosscheckEnhanceSource(t)),
	}, opts...)
	cp, err := Compile(page, all...)
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	return cp
}

func crosscheck(t *testing.T, set *jet.Set, nodes []*core.Node, opts ...CompileOption) {
	t.Helper()
	assertFeatureCrosscheck(t, crosscheckCompile(t, set, nodes, opts...))
}

func TestUIFeatureCrosscheckPaletteComponents(t *testing.T) {
	set := crosscheckSet(t)
	for _, n := range paletteInsertNodes(t) {
		n := n
		t.Run(n.Type, func(t *testing.T) {
			crosscheck(t, set, []*core.Node{n})
		})
	}
}

func TestUIFeatureCrosscheckScenarios(t *testing.T) {
	set := crosscheckSet(t)
	for _, sc := range crosscheckScenarios() {
		sc := sc
		t.Run(sc.name, func(t *testing.T) {
			crosscheck(t, set, sc.nodes, sc.opts...)
		})
	}
}

type crosscheckScenario struct {
	name  string
	nodes []*core.Node
	opts  []CompileOption
}

func probeNode(id, typeName, propsJSON string) *core.Node {
	n := &core.Node{ID: id, Type: typeName}
	if strings.TrimSpace(propsJSON) == "" {
		propsJSON = "{}"
	}
	n.Props = json.RawMessage(propsJSON)
	return n
}

func withChildren(id, typeName, propsJSON string, children ...*core.Node) *core.Node {
	n := probeNode(id, typeName, propsJSON)
	n.Children = children
	return n
}

type variantFixtureResolver struct{}

const (
	fixtureOptionsJSON = `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`

	fixtureVariantsJSON = `[{"id":"v1","price":"199.00","enabled":true,"options":{"color":"red"}},{"id":"v2","price":"219.00","enabled":true,"options":{"color":"blue"}}]`
)

func (variantFixtureResolver) ResolveString(field string) (string, error) {
	switch field {
	case "product.options":
		return fixtureOptionsJSON, nil
	case "product.variants":
		return fixtureVariantsJSON, nil
	}
	return "示例值", nil
}

func BenchmarkFeatureSource(b *testing.B) {
	set := crosscheckSet(b)
	cp := crosscheckCompile(b, set, featureBenchNodes(), WithProjectID("proj-bench"))
	html := strings.Repeat(cp.HTML, 400)
	scan := collectHTMLScan(html)
	features := core.NewFeatureSet()
	features.UseAttr(sortedFeatureKeys(scan.attrs)...)
	features.UseClass(sortedFeatureKeys(scan.classes)...)
	page := &CompiledPage{
		HTML:      html,
		UISources: crosscheckUI.sources,
		UIStyle:   crosscheckUI.css,
		Features:  features,
	}
	b.SetBytes(int64(len(html)))
	b.Logf("基准产物 HTML = %d 字节（真实页面产物 ×400）", len(html))
	b.Run("tokenize-html", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := uiAssetsForScan(collectHTMLScan(page.HTML), page.UIStyle, page.UISources); err != nil {
				b.Fatal(err)
			}
		}
	})
	b.Run("registered", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			if _, _, err := uiAssetsForScan(page.featureScan(), page.UIStyle, page.UISources); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func featureBenchNodes() []*core.Node {
	return []*core.Node{
		probeNode("bench-sec", "core.container", `{"tag":"section","layout":{"engine":"flex","flex":{}}}`),
		probeNode("bench-pl", "core.productList", `{"collectionLimit":8,"layout":"grid","filterStatus":"published","imageField":"item.images","titleField":"item.name","priceField":"item.priceRange","linkField":"item.url","currency":"¥","titleTag":"h3","emptyText":"暂无商品"}`),
		probeNode("bench-btn", "core.button", `{"text":"打开","action":"modal","value":"f1"}`),
		probeNode("bench-form", "core.form", `{"fields":[{"name":"city","label":"城市","type":"select","options":["北京","上海"]}]}`),
		probeNode("bench-cd", "core.countdown", `{"targetDate":"2030-01-01T00:00:00Z"}`),
	}
}

func crosscheckScenarios() []crosscheckScenario {
	return []crosscheckScenario{
		{
			name:  "按钮-弹窗动作",
			nodes: []*core.Node{probeNode("btn-modal", "core.button", `{"text":"打开","action":"modal","value":"f1"}`)},
		},
		{

			name:  "按钮-普通链接",
			nodes: []*core.Node{probeNode("btn-link", "core.button", `{"text":"关于","action":"link","binding":{"field":"article.url"}}`)},
		},
		{
			name: "容器-抽屉",
			nodes: []*core.Node{probeNode("c-drawer", "core.container",
				`{"tag":"section","layout":{"engine":"flex","flex":{}},"position":{"type":"drawer","drawerSide":"right","drawerOverlay":true,"drawerTriggerId":"drawer-trigger-1"}}`)},
		},
		{

			name: "容器-自定义data属性",
			nodes: []*core.Node{probeNode("c-attr", "core.container",
				`{"tag":"section","layout":{"engine":"flex","flex":{}},"styleEx":{"attributes":[{"key":"data-modal-open","value":"f9"},{"key":"data-drawer","value":"1"},{"key":"aria-label","value":"装饰"}]}}`)},
		},
		{
			name: "表单-下拉字段",
			nodes: []*core.Node{probeNode("form-select", "core.form",
				`{"fields":[{"name":"city","label":"城市","type":"select","options":["北京","上海"]}]}`)},
		},
		{
			name: "表单-纯文本字段",
			nodes: []*core.Node{probeNode("form-text", "core.form",
				`{"fields":[{"name":"mail","label":"邮箱","type":"email"}]}`)},
		},
		{
			name:  "购物车图标-下拉带角标",
			nodes: []*core.Node{probeNode("cart-icon", "core.cartIcon", `{"mode":"dropdown","showCount":true}`)},
			opts:  []CompileOption{WithProjectID("proj-crosscheck")},
		},
		{
			name:  "购物车图标-悬停形态",
			nodes: []*core.Node{probeNode("cart-icon-hover", "core.cartIcon", `{"mode":"hover","showCount":true}`)},
			opts:  []CompileOption{WithProjectID("proj-crosscheck")},
		},
		{
			name:  "购物车图标-无工程",
			nodes: []*core.Node{probeNode("cart-icon-noproj", "core.cartIcon", `{"mode":"dropdown","showCount":true}`)},
		},
		{
			name:  "计数器-增强模式",
			nodes: []*core.Node{probeNode("counter-1", "core.counter", `{"start":0,"end":99.5,"decimals":1}`)},
		},
		{
			name:  "轮播-带箭头与循环",
			nodes: []*core.Node{withChildren("slider-1", "core.slider", `{"loop":true,"showArrows":true}`, probeNode("slide-1", "core.text", `{"text":"甲"}`))},
		},
		{
			name: "图集-轮播灯箱",
			nodes: []*core.Node{probeNode("gallery-1", "core.gallery",
				`{"items":[{"url":"/storage/a.jpg","alt":"甲"},{"url":"/storage/b.jpg","alt":"乙"}],"mode":"carousel","clickAction":"lightbox"}`)},
		},
		{
			name:  "倒计时",
			nodes: []*core.Node{probeNode("countdown-1", "core.countdown", `{"targetDate":"2030-01-01T00:00:00Z","showDays":true}`)},
		},
		{
			name:  "卡片堆叠-堆叠轮播",
			nodes: []*core.Node{probeNode("cs-deck", "core.cardstack", `{"trigger":"deck","count":4,"deckArrows":true,"deckDirection":"vertical"}`)},
		},
		{
			name:  "卡片堆叠-拖拽旋转",
			nodes: []*core.Node{probeNode("cs-drag", "core.cardstack", `{"trigger":"drag","count":4}`)},
		},
		{
			name:  "卡片堆叠-全屏分页",
			nodes: []*core.Node{probeNode("cs-slide", "core.cardstack", `{"trigger":"slide","count":4}`)},
		},
		{
			name:  "站内搜索",
			nodes: []*core.Node{probeNode("search-1", "core.searchResults", "{}")},
			opts:  []CompileOption{WithProjectID("proj-crosscheck")},
		},
		{
			name:  "账号表单",
			nodes: []*core.Node{probeNode("uf-1", "core.userForms", `{"mode":"login"}`)},
			opts:  []CompileOption{WithProjectID("proj-crosscheck")},
		},
		{
			name:  "订单列表",
			nodes: []*core.Node{probeNode("ol-1", "core.orderList", "{}")},
			opts:  []CompileOption{WithProjectID("proj-crosscheck")},
		},
		{
			name:  "商品详情-有规格组合",
			nodes: []*core.Node{probeNode("p-1", "core.product", `{"optionsField":"product.options","variantsField":"product.variants"}`)},
			opts:  []CompileOption{WithProjectID("proj-crosscheck"), WithContentResolver(variantFixtureResolver{})},
		},
		{
			name:  "规格选择器-带实时库存位",
			nodes: []*core.Node{probeNode("ps-1", "core.productSelector", `{"showStock":true}`)},
			opts:  []CompileOption{WithProjectID("proj-crosscheck"), WithContentResolver(variantFixtureResolver{})},
		},
		{
			name:  "加购-有变体",
			nodes: []*core.Node{probeNode("atc-1", "core.addToCart", `{}`)},
			opts:  []CompileOption{WithProjectID("proj-crosscheck"), WithContentResolver(variantFixtureResolver{}), WithArchiveEntity("product", "current-product")},
		},
	}
}
