package builder

// ui_feature_crosscheck_test.go — 两条特征来源的交叉验证（审计 PERF-014 第 4 步）。
//
// 背景：产物要注入哪些脚本（htmx / 控件基座 / 组件增强块），此前是渲染完成后对整页
// HTML 跑一遍 tokenizer 得出的；现在改为组件在渲染期登记（core.ViewFeatureDeclarer）。
// 两条路径并存期间必须证明**结果一致** —— 否则漏登记的组件会静默失去交互
//（页面照常打开、点了没反应），而这正是登记方案最危险的失效形态。
//
// 三条断言，各覆盖一种失效：
//   A 注入结果逐字节一致 —— 端到端等价（remediation 的 verification 原文）；
//   B 登记集合 ⊆ 实际输出集合 —— 抓「登记了却没输出」（白送字节 / 判定漂移）；
//   C 影响判定的实际属性必须全登记 —— 抓「输出了却没登记」（交互静默失效）。
//
// 覆盖两批输入：① 组件库每个条目插入后的真实节点（与浏览器点一下同源，见
// defaults_contract_test.go 的 palette.js 探针）；② 显式构造的条件分支场景
//（hx-* / data-* 只在某些配置下才输出，只跑默认 props 覆盖不到）。

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder/core"
	"go_wp/internal/templates"
)

// crosscheckUI 交叉验证用的控件资源与样式：与组件真实产物无关，只为了让两条路径
// 都走到「组装脚本」这条分支上，从而能逐字节比对。
var crosscheckUI = struct {
	sources map[string]string
	css     string
}{sources: crosscheckUISources(), css: "/* ui css */"}

// crosscheckUISources 控件资源全集（清单与 ui_script.go 的 uiBlocks 一致）。
//
// 不直接用 uiSrcForTest：那份桩只含 5 个资源，于是「作者在容器上写 data-drawer →
// 注入 drawer.js」这条**正确**判定会以「资源缺失」的构建错误报出来 —— 那是桩的缺口，
// 不是特征判定的问题，留着会让交叉验证的失败信息指向错误的方向。
func crosscheckUISources() map[string]string {
	sources := uiSrcForTest()
	for _, name := range []string{"drawer.js", "confirm.js", "colorfield.js", "iconfield.js", "themetoggle.js"} {
		sources[name] = "/* " + name + " */ WBUI.register(function(){});"
	}
	return sources
}

// assertFeatureCrosscheck 对一份编译结果做三条交叉验证断言。
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

	// B 登记 ⊆ 实际输出：登记了产物里根本没有的属性 / class，说明上报的判定条件
	// 与模板分叉了 —— 轻则白送字节，重则让「零字节注入」这条收敛失效。
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

	// C 影响判定的实际属性必须全登记：漏一个就少注入一个脚本，
	// 而症状是「页面正常、交互不动」，没有任何错误日志。
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

	// A 注入结果逐字节一致（CSS 与 JS 分别比对）。
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

	// 增强块单独一层（enhanceScriptFor 按增强特征挑块，用的是同一份能力集合）。
	if cp.EnhanceSource != "" {
		regEnh := enhanceScriptFor(reg.attrs, cp.EnhanceSource)
		tokEnh := enhanceScriptFor(tok.attrs, cp.EnhanceSource)
		if regEnh != tokEnh {
			t.Errorf("增强块选择不一致\n登记:     %q\ntokenize: %q\n登记集合: %v\ntokenize 集合: %v",
				regEnh, tokEnh, sortedFeatureKeys(reg.attrs), sortedFeatureKeys(tok.attrs))
		}
	}
}

// sortedFeatureKeys 集合的确定性输出（失败信息里要能一眼比对）。
func sortedFeatureKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// crosscheckSet 组件模板集。
func crosscheckSet(t testing.TB) *jet.Set {
	t.Helper()
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	return set
}

// crosscheckEnhanceSource 真实增强框架源码（裁剪逻辑依赖它的结构：onReady 尾段）。
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

// crosscheckCompile 编译一份页面文档（带真实的控件资源与增强源码）。
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

// crosscheck 编译 + 断言。
func crosscheck(t *testing.T, set *jet.Set, nodes []*core.Node, opts ...CompileOption) {
	t.Helper()
	assertFeatureCrosscheck(t, crosscheckCompile(t, set, nodes, opts...))
}

// TestUIFeatureCrosscheckPaletteComponents 组件库每个条目插入后的产物都过交叉验证。
// 这一批的价值在覆盖面：组件库是「用户能拖出来的全部东西」，漏登记的组件必然在其中现形。
func TestUIFeatureCrosscheckPaletteComponents(t *testing.T) {
	set := crosscheckSet(t)
	for _, n := range paletteInsertNodes(t) {
		n := n
		t.Run(n.Type, func(t *testing.T) {
			crosscheck(t, set, []*core.Node{n})
		})
	}
}

// TestUIFeatureCrosscheckScenarios 显式覆盖条件分支：hx-* 与增强特征并非每个配置都输出，
// 只跑默认 props 会漏掉「该出没出 / 不该出却出了」这两类错误。
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

// probeNode 构造一个组件节点（propsJSON 空则给空对象）。
func probeNode(id, typeName, propsJSON string) *core.Node {
	n := &core.Node{ID: id, Type: typeName}
	if strings.TrimSpace(propsJSON) == "" {
		propsJSON = "{}"
	}
	n.Props = json.RawMessage(propsJSON)
	return n
}

// withChildren 构造带子节点的节点。
func withChildren(id, typeName, propsJSON string, children ...*core.Node) *core.Node {
	n := probeNode(id, typeName, propsJSON)
	n.Children = children
	return n
}

// variantFixtureResolver 商品规格 / 变体的桩解析器（交叉验证用）。
//
// 为什么不能沿用 paletteContentResolver：它对任何字段都返回同一个占位串，而规格维度与
// 变体组合要的是**结构完整**的 JSON —— 喂不进去时 product / productSelector / addToCart
// 一律走空态，那几处 hx-get 条件分支就永远覆盖不到（覆盖面比断言本身更要紧）。
type variantFixtureResolver struct{}

const (
	// fixtureOptionsJSON 规格维度（一个颜色维度、两个值）。
	fixtureOptionsJSON = `[{"key":"color","name":"颜色","values":[{"key":"red","label":"红"},{"key":"blue","label":"蓝"}]}]`
	// fixtureVariantsJSON 变体组合（两条启用变体：规格选择器要求组合 ≥2 才输出）。
	fixtureVariantsJSON = `[{"id":"v1","price":"199.00","enabled":true,"options":{"color":"red"}},{"id":"v2","price":"219.00","enabled":true,"options":{"color":"blue"}}]`
)

// ResolveString 实现 core.ContentResolver：只对规格字段给结构，其余给占位串。
func (variantFixtureResolver) ResolveString(field string) (string, error) {
	switch field {
	case "product.options":
		return fixtureOptionsJSON, nil
	case "product.variants":
		return fixtureVariantsJSON, nil
	}
	return "示例值", nil
}

// BenchmarkFeatureSource 量化两条特征来源的开销差（审计 PERF-014 的 verification 第二条）。
//
// 这正是条目要回答的问题：换登记路径省下的是哪一段、值多少。基准里的 HTML 是**真实产物**
// （容器 / 商品列表 / 按钮 / 表单 / 倒计时拼出的页面）重复放大到常见体量，
// tokenize 那一路就是此前每次 RenderDocument 都要付的一遍整页解析。
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

// featureBenchNodes 基准用的页面组件（覆盖 hx-* 与增强特征两类属性）。
func featureBenchNodes() []*core.Node {
	return []*core.Node{
		probeNode("bench-sec", "core.container", `{"tag":"section","layout":{"engine":"flex","flex":{}}}`),
		probeNode("bench-pl", "core.productList", `{"collectionLimit":8,"layout":"grid","filterStatus":"published","imageField":"item.images","titleField":"item.name","priceField":"item.priceRange","linkField":"item.url","currency":"¥","titleTag":"h3","emptyText":"暂无商品"}`),
		probeNode("bench-btn", "core.button", `{"text":"打开","action":"modal","value":"f1"}`),
		probeNode("bench-form", "core.form", `{"fields":[{"name":"city","label":"城市","type":"select","options":["北京","上海"]}]}`),
		probeNode("bench-cd", "core.countdown", `{"targetDate":"2030-01-01T00:00:00Z"}`),
	}
}

// crosscheckScenarios 条件分支场景集。
func crosscheckScenarios() []crosscheckScenario {
	return []crosscheckScenario{
		{
			name:  "按钮-弹窗动作",
			nodes: []*core.Node{probeNode("btn-modal", "core.button", `{"text":"打开","action":"modal","value":"f1"}`)},
		},
		{
			// 动态链接动作需要绑定字段（缺了会编译失败），补上以覆盖「不出特征的按钮」这一路。
			name:  "按钮-普通链接",
			nodes: []*core.Node{probeNode("btn-link", "core.button", `{"text":"关于","action":"link","binding":{"field":"article.url"}}`)},
		},
		{
			name: "容器-抽屉",
			nodes: []*core.Node{probeNode("c-drawer", "core.container",
				`{"tag":"section","layout":{"engine":"flex","flex":{}},"position":{"type":"drawer","drawerSide":"right","drawerOverlay":true,"drawerTriggerId":"drawer-trigger-1"}}`)},
		},
		{
			// 作者在容器上写的 data-* 自定义属性也会进产物（白名单允许 data-*），
			// 所以它们同样能触发控件脚本 —— 判定必须跟着属性串一起走。
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
			nodes: []*core.Node{probeNode("atc-1", "core.addToCart", `{"optionsField":"product.options","variantsField":"product.variants"}`)},
			opts:  []CompileOption{WithProjectID("proj-crosscheck"), WithContentResolver(variantFixtureResolver{})},
		},
	}
}
