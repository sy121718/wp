package builder

// 控件资源来自公共 UI Kit；后台通过静态路径加载，访问产物只携带实际用到的闭包。

import (
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// 文件与触发属性在这里声明一次；装配层从 UIAssetFiles 获取源码清单。
// 属性按名称精确匹配，不将正文、注释、脚本中的示例当成控件。
type uiBlock struct {
	file  string
	attrs []string
	// prefixes 前缀匹配。htmx 的指令家族（hx-get / hx-post / hx-trigger / hx-target …）
	// 无法穷举：逐条列出的代价是「每个人加一条指令时都得记得回来改这张表」，
	// 漏掉的表现是那条指令在产物里静默失效。
	prefixes []string
	// noStyle 标记该资源不需要配套样式：ui.css 是控件外观，htmx 是行为库。
	noStyle bool
}

// uiBaseClasses 基座样式类名：只写 class 也应注入 ui.css（UIK-002：样式与行为解耦）。
//
// 口径＝「作者会手写的控件外观类」，唯一来源是 ui.css 里成段的控件外观
// （docs/02-F-ui-kit.md §10.2「类名清单（唯一来源：static/css/ui.css）」）。
// 三类类名**故意不收**，收了就是给产物白送字节：
//   - 工具类（w-full / flex / gap-* / text-sm / mt-* / mb-* …）：几乎每页都有，
//     命中即注入整份 ui.css，等于让所有产物一起膨胀；
//   - 脚本生成的内部结构类（wbs-trigger / wb-modal-* / wb-confirm-* / wbc-* …）：
//     由属性触发脚本后生成，作者不会手写，写了也没有对应的 DOM 结构；
//   - 状态类与后台页面结构类（is-* / sr-only / pages-form / action-row …）：
//     单独出现不表达「用了控件」，命中只会制造假阳性。
//
// 清单与 ui.css 的一致性由 TestUIBaseClassListMatchesUICSS 钉住（漏项＝静默无样式）。
var uiBaseClasses = []string{
	// 按钮
	"btn", "btn-primary", "btn-secondary", "btn-ghost", "btn-danger", "btn-sm", "btn-icon",
	// 工作台密度规格的同族按钮
	"wb-btn", "wb-btn-primary", "wb-btn-secondary", "wb-btn-ghost", "wb-icon-btn",
	// 表单字段
	"form-input", "form-select", "form-textarea", "form-label", "form-hint", "form-error",
	"form-group", "checkbox",
	// 自绘下拉容器（select.js 把原生 select 换成这一层结构）
	"wbs",
	// 成组排布
	"form-inline", "form-row",
	// 卡片
	"card", "card-header", "card-title", "card-body", "card-footer",
	// 表格（table-wrap 是窄屏横向滚动的外观容器，与 data-table 同段）
	"table-wrap", "data-table", "data-table-wide",
	// 徽标与状态点
	"badge", "badge-success", "badge-warning", "badge-danger", "badge-info", "badge-mute",
	"dot", "dot-success", "dot-warning", "dot-danger", "dot-mute",
}

var uiBlocks = []uiBlock{
	// htmx 是访问面所有 hx-* 属性的唯一执行者，也是控件入口 index.js 监听
	// htmx:afterSwap 的前提。产物此前从不携带它 —— 组件按 HTMX 约定写出的
	// hx-get / hx-post 在访问面全部是哑属性（组件自己降级成原生表单，功能
	// 「看着能用」但走的不是预想路径，这类静默降级最难发现）。
	{file: "htmx.min.js", prefixes: []string{"hx-"}, noStyle: true},
	{file: "select.js", attrs: []string{"data-ui-select"}},
	{file: "modal.js", attrs: []string{"data-modal", "data-modal-open", "data-modal-close"}},
	{file: "drawer.js", attrs: []string{"data-drawer-open", "data-drawer-close", "data-drawer", "data-drawer-mask"}},
	{file: "confirm.js", attrs: []string{"data-confirm"}},
	{file: "colorfield.js", attrs: []string{"data-color-field"}},
	{file: "iconfield.js", attrs: []string{"data-icon-field", "data-icon-name"}},
	{file: "themetoggle.js", attrs: []string{"data-theme-toggle"}},
}

// UIAssetFiles 是访问产物可用的公共控件资源清单，顺序为助手、控件、扫描入口。
// 每次返回独立切片，调用方不能修改编译器的注册表。
func UIAssetFiles() []string {
	files := make([]string, 0, len(uiBlocks)+2)
	files = append(files, "_util.js")
	for _, block := range uiBlocks {
		files = append(files, block.file)
	}
	return append(files, "index.js")
}

// htmlFeatures 是同一份最终 HTML 的能力属性集合，控件与组件增强共享。
type htmlFeatures map[string]struct{}

// htmlScan 是同一份 HTML 的能力扫描结果：属性驱动行为脚本，class 驱动基座样式，
// 片段能力（hx-* 的值）驱动片段基座样式（审计 UIK-005）。
type htmlScan struct {
	attrs   htmlFeatures
	classes htmlFeatures
	// frags 页面引用的运行时片段能力（小写能力名，如 cartview）。它是唯一**值敏感**的
	// 特征：属性名给不出「hx-get 指向哪个能力」，所以这一项始终由 fragmentCapsFromHTML
	// 从 HTML 里提取（登记路径与 tokenize 路径都跑同一段解析，结果必然一致）。
	frags htmlFeatures
}

// featureScan 返回产物组装要用的能力集合（审计 PERF-014）。
//
// 首选**渲染期登记结果**：Compile 路径总会带上一个 Features（可能为空集），
// 空集就是「这页确实什么都没用」—— 于是零字节注入，且不再对整页 HTML 跑 tokenizer。
//
// Features 为 nil 只出现在手工构造 CompiledPage 的调用方（单测、历史调用点），
// 此时回退到扫描 HTML。这条回退**不是遗留兼容**：它是 tokenize 实现保留的理由 ——
// 交叉验证测试（ui_feature_crosscheck_test.go）拿它和登记结果做双向比对，
// 两边结果必须逐字节一致。两条路径并存期间，回退保证「没接登记的调用方」行为不变。
func (c *CompiledPage) featureScan() htmlScan {
	if c.Features != nil {
		attrs := htmlFeatures(c.Features.Attrs())
		scan := htmlScan{
			attrs:   attrs,
			classes: htmlFeatures(c.Features.Classes()),
		}
		// 片段能力是值敏感特征（属性名给不出「hx-get 指向哪个能力」），只能从 HTML 提取。
		// 两个前置判据任一成立才付这次解析：
		//   · 登记表里有 hx-* 属性 —— 组件输出 htmx 请求的常规路径；
		//   · HTML 里出现 /_fragments/ 子串 —— 作者手写（或经 HTML 节点注入）的引用
		//     不一定进登记表，而漏判的表现是「片段刷新出来是裸 HTML」，静默且难查。
		// 第二条只是一次 strings.Contains（O(字节)，不做解析），纯内容页的成本可忽略；
		// 真正贵的那次 tokenize 只在命中时才发生，PERF-014 的收敛不被推翻。
		if hasHXAttr(attrs) || strings.Contains(c.HTML, fragmentPathPrefix) {
			scan.frags = fragmentCapsFromHTML(c.HTML)
		}
		return scan
	}
	return collectHTMLScan(c.HTML)
}

func collectHTMLFeatures(content string) htmlFeatures {
	return collectHTMLScan(content).attrs
}

func collectHTMLScan(content string) htmlScan {
	out := htmlScan{attrs: make(htmlFeatures), classes: make(htmlFeatures), frags: make(htmlFeatures)}
	z := html.NewTokenizer(strings.NewReader(content))
	for {
		kind := z.Next()
		if kind == html.ErrorToken {
			break // 内存字符串读取到 EOF；脚本、样式和原始文本不当成标签。
		}
		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}
		_, more := z.TagName()
		for more {
			var key, val []byte
			key, val, more = z.TagAttr()
			name := strings.ToLower(string(key))
			if strings.HasPrefix(name, "data-") || strings.HasPrefix(name, "hx-") {
				out.attrs[name] = struct{}{}
			}
			// 片段能力只看 hx-* 的值（htmx 是片段消费的唯一入口，组件在输出 hx-post 的
			// 同时也输出同一 URL 的原生降级路径，所以覆盖 hx-* 就覆盖了全部消费方式）。
			if strings.HasPrefix(name, "hx-") {
				for _, cap := range fragmentCapsIn(string(val)) {
					out.frags[cap] = struct{}{}
				}
			}
			if name == "class" {
				for _, token := range strings.Fields(string(val)) {
					if c := strings.ToLower(strings.TrimSpace(token)); c != "" {
						out.classes[c] = struct{}{}
					}
				}
			}
		}
	}
	return out
}

// relevantAttrs 从属性集合里挑出**会影响注入判定**的那些（审计 PERF-014 交叉验证用）。
//
// 判据与真正做决策的 usedUIFiles / enhanceScriptFor 同源：命中任一控件资源的精确属性名
// 或前缀（hx- / data-ui-select / data-modal-open …），或命中任一增强块的触发特征
// （data-counter / data-slider / data-cart-icon-panel …）。
//
// 纯标记型 data-*（data-sky-product-list 这类既不触发控件也不触发增强的属性）不算相关：
// 它们不参与判定，要求组件登记它们只会让比对充满噪声、掩盖真正的漏报。
func relevantAttrs(attrs htmlFeatures) htmlFeatures {
	out := make(htmlFeatures)
	for name := range attrs {
		single := htmlFeatures{name: struct{}{}}
		if relevantAttr(single) {
			out[name] = struct{}{}
		}
	}
	return out
}

// relevantAttr 单个属性是否影响注入判定（单元素集合上复用两条既有判据）。
func relevantAttr(single htmlFeatures) bool {
	for _, block := range uiBlocks {
		if block.hit(single) {
			return true
		}
	}
	for _, block := range allEnhanceBlocks() {
		for _, feat := range block.feats {
			if _, ok := single[feat]; ok {
				return true
			}
		}
	}
	return false
}

// hasUIBaseClass 判据：集合里是否含任一控件外观类。
func hasUIBaseClass(classes htmlFeatures) bool {
	for _, c := range uiBaseClasses {
		if _, ok := classes[c]; ok {
			return true
		}
	}
	return false
}

// hit 判断这个资源是否被页面实际用到（精确属性名或前缀任一命中）。
func (b uiBlock) hit(attrs htmlFeatures) bool {
	for _, attr := range b.attrs {
		if _, ok := attrs[attr]; ok {
			return true
		}
	}
	for _, prefix := range b.prefixes {
		for name := range attrs {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		}
	}
	return false
}

// usedUIFiles 返回命中的资源与「是否需要控件基座」。
//
// 基座（_util.js 助手 + index.js 扫描入口 + ui.css 样式）只服务控件类资源：
// 一个只用 htmx 属性的页面不该因此带上整套控件基座与样式。
func usedUIFiles(attrs htmlFeatures) (files []string, needBase bool) {
	for _, block := range uiBlocks {
		if !block.hit(attrs) {
			continue
		}
		files = append(files, block.file)
		if !block.noStyle {
			needBase = true
		}
	}
	return files, needBase
}

// uiAssetsFor 一次识别能力并同时组装 CSS/JS，防止两次扫描的规则漂移。
//
// CSS 由两段构成：控件基座（ui.css）与片段基座（按页面引用到的片段族选，审计 UIK-005）。
// 两段判据彼此独立 —— 控件看属性与外观 class，片段看 hx-* 的值。
// sources=nil 表示调用方选择无脚本输出；非 nil（含空 map）表示已启用控件增强，
// 命中的控件、基座、入口或样式缺失都返回构建错误，不能生成残缺产物。
func uiAssetsFor(attrs htmlFeatures, css string, sources map[string]string) (string, string, error) {
	return uiAssetsForScan(htmlScan{attrs: attrs}, css, sources)
}

func uiAssetsForScan(scan htmlScan, css string, sources map[string]string) (string, string, error) {
	files, needStyleFromAttrs := usedUIFiles(scan.attrs)
	needStyle := needStyleFromAttrs || hasUIBaseClass(scan.classes)
	// 片段基座样式与控件基座**彼此独立**（审计 UIK-005）：页面只写 hx-post="/_fragments/cartAdd"
	// 而没有用任何控件类时，它仍需要购物车片段的样式 —— 不能因为「没有控件」而整页零注入。
	fragCSS := fragmentBaseCSSFor(scan.frags)
	if len(files) == 0 && !needStyle && fragCSS == "" {
		return "", "", nil
	}
	if sources == nil {
		return joinCSS(pickCSS(needStyle, css), fragCSS), "", nil
	}
	if len(files) > 0 {
		if needStyleFromAttrs {
			// 基座助手在控件之前（控件靠它注册），入口在最后（它负责扫描已注册的控件）。
			files = append(append([]string{"_util.js"}, files...), "index.js")
		}
	}
	parts := make([]string, 0, len(files))
	for _, file := range files {
		src := sources[file]
		if strings.TrimSpace(src) == "" {
			return "", "", fmt.Errorf("控件资源缺失: %s", file)
		}
		parts = append(parts, src)
	}
	if needStyle && strings.TrimSpace(css) == "" {
		return "", "", fmt.Errorf("控件资源缺失: ui.css")
	}
	return joinCSS(pickCSS(needStyle, css), fragCSS), strings.Join(parts, "\n"), nil
}

// pickCSS 按需取用控件基座样式（不需要时返回空串，不产生多余字节）。
func pickCSS(need bool, css string) string {
	if need {
		return css
	}
	return ""
}

// joinCSS 拼接两段 CSS（某段为空时不留下多余空行）。
func joinCSS(first, second string) string {
	switch {
	case first == "":
		return second
	case second == "":
		return first
	}
	return first + "\n\n" + second
}
