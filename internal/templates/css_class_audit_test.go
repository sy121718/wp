package templates

// css_class_audit_test.go — CSS 类对账门禁（「用了但没定义」/「定义了没人用」）。
//
// 为什么自建：code-review-graph 纳不进 CSS（`status` 的语言表写死
// bash/go/javascript/powershell/python/sql，没有 css/html/jet，也没有注入边的命令），
// PurgeCSS 只回答「哪些 CSS 没用到」且对运行时属性（[dir="rtl"]）有确定误报，
// 而项目零 Node 依赖、不为一次对账引工具链。这类漂移在本项目真实发生过两次、
// 且都是没人扫得到的静默缺陷：
//   · static/js/media-admin.js 动态生成的详情面板没带基座类 —— 控件在页面里凭空失去外观；
//   · ui.css 的 .admin-layout 容器兜底给控件外观 —— 容器一旦被删，控件样式静默消失。
// 本门禁不替代人看代码，只把「类名两边对不上」这件事变成红灯。
//
// 两个方向刻意用不同的严格度：
//   · 方向 A「用了但没定义」→ 严格提取（class="..." 的静态 token + JS 里的类名字面量）。
//     这里误报的代价是门禁被绕过（作者开始无视红灯），所以宁少勿滥：
//     拼接碎片、比较值、getAttribute 的属性名一律不当类名。
//   · 方向 B「定义了没人用」→ 宽松匹配（定义侧类名只要在使用侧文本里以 token 边界出现过就算有人用）。
//     这里误报的代价是死样式永远清不掉，所以宁滥勿缺：连注释里出现过都算「有人提过」。
//
// 豁免清单沿用本项目既有约定（带可核对理由 + 条目不再命中即失败）：
// 下一批把「待清理」的死样式删掉后，对应条目会失去命中点、测试随即变红，
// 逼清单跟着收缩 —— 清单只增不减就等于门禁失效。
//
// 使用侧包含三条扫描面：模板（.html/.jet）、控制面 JS（static/js/**）、
// 以及 **Go 代码里拼的 HTML class 字面量**（整棵 internal 树，排除 _test.go 与本包）。
// 第三条是清账批实锤出来的盲区：`wb-node-actions` / `wb-node-flag` 由
// internal/module/workbench/inbound/http/outline_handle.go 拼出，前两条扫描面都看不见，
// 于是那两条**在用**的规则被当死样式删掉，靠工作台浏览器抽查发现 11 个元素带类才还原。
// Go 侧只作「有人用」的证据（looseTokens），不进方向 A —— 它常与运行时值拼接，进 A 会误报。
//
// 已知让步（写在这里避免下一个人误判）：
//   · 动态类名放行必须**有据可查**：dynamicClassEvidence 里每条前缀都写明
//     拼接点（file:line + 表达式）与可枚举的取值域，放行条件 = 前缀匹配 **且** 后缀在取值域里。
//     只按前缀放行会让「同前缀的孤立变体」（没人会拼出来的 is-xxx）永远逃过 B 方向 ——
//     那正是收窄要治的病。证据表里的拼接点在本次扫描找不到时，TestDynamicClassEvidenceIsCurrent 会红。
//   · A 方向收窄为「属于定义侧已存在的类家族」才判红（见 auditFamilyDefined）：
//     完全新家族的无样式钩子（media-detail-form / customers-page-table 这类结构类）不判红，
//     因为「无外观需求」是它们的常态，判红只会制造噪声；
//     而「往既有家族里加了个没定义的变体」（form-input--x / btn-huge / wbd-month）仍然判红 ——
//     那才是「想加样式却漏了定义」的高发形态。
//     「JS 动态生成控件漏带基座类」那类真缺陷不靠本方向抓，它由 media_admin_js_base_test.go 的 JS 侧判据覆盖。
//   · 只扫 internal/templates/static/css + static/vendor 的 CSS 与模板内联 <style>。
//     前台片段（fragments/*.jet 的 sky-*、user/*.html 的 g-*）样式真源在
//     internal/builder/components/*/*.css 这类构建期组件样式里，不在本定义侧，
//     所以方向 A 的断言范围限定在后台与工作台宿主（它们才是 static/css 的消费者）。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var (
	reAuditClassAttr  = regexp.MustCompile(`class\s*=\s*"([^"]*)"`)
	reAuditJetExpr    = regexp.MustCompile(`(?s)\{\{.*?\}\}`)
	reAuditClassName  = regexp.MustCompile(`\.?className\s*=\s*([^;\n]+)`)
	reAuditJSLiteral  = regexp.MustCompile(`'([^']*)'|"([^"]*)"`)
	reAuditJSCompare  = regexp.MustCompile(`[=!]==?\s*'[^']*'|[=!]==?\s*"[^"]*"`)
	reAuditJSGetAttr  = regexp.MustCompile(`getAttribute\(\s*'[^']*'\s*\)|getAttribute\(\s*"[^"]*"\s*\)`)
	reAuditJSOrDeflt  = regexp.MustCompile(`\|\|\s*'[^']*'|\|\|\s*"[^"]*"`)
	reAuditCSSClass   = regexp.MustCompile(`\.([a-zA-Z][a-zA-Z0-9_-]*)`)
	reAuditCSSURL     = regexp.MustCompile(`(?i)url\([^)]*\)`)
	reAuditCSSDQuote  = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
	reAuditCSSSQuote  = regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)
	reAuditInlineCSS  = regexp.MustCompile(`(?s)<style>(.*?)</style>`)
	reAuditStaticTok  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
	reAuditDynPrefix  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*-$`)
	reAuditLooseToken = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9_-]*`)
	reAuditJSConcat   = regexp.MustCompile(`['"]\s*\+|\+\s*['"]|\$\{`)
)

// cssAuditFacts 是一次扫描的全部事实。
type cssAuditFacts struct {
	defined     map[string][]string // 定义侧类名 → CSS 文件（含 vendor 与模板内联 style）
	definedOwn  map[string][]string // 只含本仓库自己的 CSS（static/css + 模板内联 style）：方向 B 的定义侧
	usedStrict  map[string][]string // 方向 A：静态类名 → "文件:行: 片段"
	looseTokens map[string]bool     // 方向 B：使用侧文本全集里的 token
	goClasses   map[string][]string // Go 侧拼 HTML 的 class 字面量 → 出处（弱证据，只进 looseTokens）
	dynPrefixes map[string][]string // 动态前缀 → 来源
}

// TestCSSClassesUsedButUndefined 方向 A：模板/JS 里写死的类名必须在定义侧存在。
// 防的就是 media-admin.js 那类缺陷 —— 生成控件时漏了（或写错）类名，页面不报错、只是没样式。
func TestCSSClassesUsedButUndefined(t *testing.T) {
	facts := collectCSSAuditFacts(t)

	missing := auditMissing(facts.usedStrict, func(cls string) bool {
		if len(facts.defined[cls]) > 0 {
			return false
		}
		if auditDynamicCovered(cls) {
			return false
		}
		return auditFamilyDefined(facts.defined, cls)
	}, cssUndefinedAllowed)

	if len(missing) > 0 {
		var b strings.Builder
		b.WriteString("这些类被模板/JS 写死使用，但定义侧（static/css、static/vendor、模板内联 <style>）里没有定义 —— 页面不会报错，只会静默失去样式：\n")
		for _, cls := range missing {
			b.WriteString("  · " + cls + "\n")
			for _, site := range head(facts.usedStrict[cls], 3) {
				b.WriteString("      使用点 " + site + "\n")
			}
		}
		b.WriteString("修法：补样式，或确认它是纯结构钩子后加进 cssUndefinedAllowed 并写明理由。")
		t.Error(b.String())
	}

	// 豁免条目不再命中即失败：使用点消失、类已被定义（或被动态前缀接管）时，清单必须同步收缩。
	for _, cls := range staleUndefinedAllowed(facts) {
		t.Errorf("cssUndefinedAllowed 里的 %q 已经不再是「用了但没定义」（类已被定义，或使用点已消失）：清单必须同步收缩，只增不减等于门禁失效", cls)
	}
}

// TestCSSClassesDefinedButUnused 方向 B：定义侧里没人用的类进豁免清单，否则报错。
// 防的是死样式堆积 —— 以及更隐蔽的一种漂移：作者把类当成「有人在用」而不敢删，
// 实际模板与 JS 早已改用别的类名。
func TestCSSClassesDefinedButUnused(t *testing.T) {
	facts := collectCSSAuditFacts(t)

	unused, prefixCovered := auditUnused(facts.definedOwn, facts.usedStrict, facts.looseTokens, cssUnusedAllowed)
	if len(unused) > 0 {
		var b strings.Builder
		b.WriteString("这些类在定义侧存在，但使用侧（模板 + JS 文本）里以 token 边界找不到任何出现 —— 要么是死样式，要么使用点藏在扫描范围之外：\n")
		for _, cls := range unused {
			b.WriteString("  · " + cls + "  ← " + strings.Join(facts.defined[cls], ", ") + "\n")
		}
		b.WriteString("修法：确认为死样式则删掉 CSS（下一批统一清账），暂不清理则加进 cssUnusedAllowed 并写明理由（如「JS 运行时拼接生成，前缀 is-」）。")
		t.Error(b.String())
	}

	for _, cls := range staleUnusedAllowed(facts) {
		entry := cssUnusedAllowed[cls]
		if strings.Contains(entry, "待清理") {
			t.Errorf("cssUnusedAllowed 里的 %q 标着「待清理」，但它已经不再命中未使用集合（类已被删除，或已经有人用了）：该条目连同 CSS 一起收口", cls)
			continue
		}
		t.Errorf("cssUnusedAllowed 里的 %q 已经不再命中未使用集合（类已被定义处删除，或已经有人用了）：清单必须同步收缩", cls)
	}

	// Go 侧扫描的覆盖面必须可观测：空转就等于盲区还在。
	if len(facts.goClasses) < 10 {
		t.Fatalf("Go 侧只扫到 %d 个 class 字面量（预期 ≥10）：扫描口径或路径可能已失效", len(facts.goClasses))
	}
	var goUndefined []string
	for cls := range facts.goClasses {
		if len(facts.defined[cls]) == 0 {
			goUndefined = append(goUndefined, cls)
		}
	}
	sort.Strings(goUndefined)
	t.Logf("Go 侧 class 字面量扫出 %d 个类名（出处 %d 个文件）；其中 %d 个在定义侧没有对应规则（仅提示，不判失败）：%v",
		len(facts.goClasses), len(goFilesOf(facts)), len(goUndefined), goUndefined)

	// 被动态前缀放行的类：不失败，但要留痕 —— 前缀覆盖是让步，不是结论。
	if len(prefixCovered) > 0 {
		sorted := make([]string, 0, len(prefixCovered))
		for cls := range prefixCovered {
			sorted = append(sorted, cls)
		}
		sort.Strings(sorted)
		t.Logf("以下 %d 个类靠动态证据放行（前缀 %s；拼接点与取值域见 dynamicClassEvidence）：%s",
			len(sorted), strings.Join(sortedPrefixKeys(facts.dynPrefixes), ", "), strings.Join(sorted, ", "))
	}
}

// auditMissing 返回未命中定义侧、且不在豁免清单里的类名；keep 决定某个类是否需要继续判定。
func auditMissing(used map[string][]string, keep func(string) bool, allowed map[string]string) []string {
	var missing []string
	for cls := range used {
		if !keep(cls) {
			continue
		}
		if _, ok := allowed[cls]; ok {
			continue
		}
		missing = append(missing, cls)
	}
	sort.Strings(missing)
	return missing
}

// auditUnused 返回定义了却没人用的类名，以及被动态前缀放行的类名。
func auditUnused(defined map[string][]string, strict map[string][]string, loose map[string]bool, allowed map[string]string) ([]string, map[string]bool) {
	var unused []string
	prefixCovered := map[string]bool{}
	for cls := range defined {
		if loose[cls] {
			continue // 使用侧文本里出现过（连注释也算）→ 有人提过，不判死
		}
		if len(strict[cls]) > 0 {
			continue
		}
		if auditDynamicCovered(cls) {
			prefixCovered[cls] = true
			continue
		}
		if _, ok := allowed[cls]; ok {
			continue
		}
		unused = append(unused, cls)
	}
	sort.Strings(unused)
	return unused, prefixCovered
}

// goFilesOf 统计 Go 侧字面量来自多少个文件（t.Log 用）。
func goFilesOf(f cssAuditFacts) map[string]bool {
	files := map[string]bool{}
	for _, sites := range f.goClasses {
		for _, s := range sites {
			files[s] = true
		}
	}
	return files
}

// staleUndefinedAllowed 找出方向 A 清单里已经不该留的条目：
// 使用点消失（模板/JS 不再写这个类）、类已被补上定义、或已被动态前缀接管 —— 三种都该删条目。
func staleUndefinedAllowed(f cssAuditFacts) []string {
	var stale []string
	for cls := range cssUndefinedAllowed {
		switch {
		case len(f.usedStrict[cls]) == 0:
			stale = append(stale, cls) // 使用点消失 → 门禁已经守不到它
		case len(f.defined[cls]) > 0:
			stale = append(stale, cls) // 类已被定义 → 不再是「用了但没定义」
		case auditDynamicCovered(cls) || !auditFamilyDefined(f.defined, cls):
			stale = append(stale, cls) // 收窄后不再判红（动态有据可查 / 属完全新家族）→ 条目该收回
		}
	}
	sort.Strings(stale)
	return stale
}

// staleUnusedAllowed 找出方向 B 清单里已经不该留的条目：
// 类已从定义侧删除（清账完成）、或使用侧其实已经有人用、或已被动态前缀放行。
func staleUnusedAllowed(f cssAuditFacts) []string {
	var stale []string
	for cls := range cssUnusedAllowed {
		switch {
		case len(f.definedOwn[cls]) == 0:
			stale = append(stale, cls) // 类已删 → 清单条目连同 CSS 一起收口
		case f.looseTokens[cls] || len(f.usedStrict[cls]) > 0:
			stale = append(stale, cls) // 有人用了 → 不再是死样式
		case auditDynamicCovered(cls):
			stale = append(stale, cls) // 已被动态证据覆盖 → 不必逐条豁免
		}
	}
	sort.Strings(stale)
	return stale
}

// auditDynamicCovered 动态类名放行判据：前缀匹配 **且** 后缀落在该前缀的可枚举取值域里。
// 取值域为空（枚举不出来）时退回前缀放行，理由写在 dynamicClassEvidence 的 note 里。
func auditDynamicCovered(cls string) bool {
	for prefix, ev := range dynamicClassEvidence {
		if !strings.HasPrefix(cls, prefix) {
			continue
		}
		if len(ev.values) == 0 {
			return true
		}
		suffix := strings.TrimPrefix(cls, prefix)
		for _, v := range ev.values {
			if v == suffix {
				return true
			}
		}
	}
	return false
}

// auditFamilyDefined 判定「这个未定义的类是否属于定义侧已存在的某个类家族」。
// 形状：把类名逐级剥短 —— BEM 的 -- / __ 前缀各取一次，再逐段去掉尾部的 -segment；
// 任一候选父名在定义侧出现即算「既有家族」。
func auditFamilyDefined(defined map[string][]string, cls string) bool {
	for _, parent := range auditParentCandidates(cls) {
		if len(defined[parent]) > 0 {
			return true
		}
	}
	return false
}

func auditParentCandidates(cls string) []string {
	var out []string
	add := func(s string) {
		if s == "" || s == cls || contains(out, s) {
			return
		}
		out = append(out, s)
	}
	if i := strings.Index(cls, "--"); i > 0 {
		add(cls[:i])
	}
	if i := strings.Index(cls, "__"); i > 0 {
		add(cls[:i])
	}
	parts := strings.Split(cls, "-")
	for i := len(parts) - 1; i > 0; i-- {
		add(strings.Join(parts[:i], "-"))
	}
	return out
}

// dynPrefixEvidence 一条动态前缀的放行证据。
type dynPrefixEvidence struct {
	site   string   // 拼接点：file:line + 表达式（可核对）
	values []string // 可枚举的取值域（去前缀后的后缀）
	note   string   // 取值域来源；不可枚举时写明为何只能退回前缀放行
}

// dynamicClassEvidence 动态类名放行的唯一出口：没有条目的前缀一律不放行。
var dynamicClassEvidence = map[string]dynPrefixEvidence{
	"is-": {
		site: "ui/toast.js:61 `'wb-toast is-' + type`；media-admin.js:334 `'media-variant-badge is-' + (v.status || 'pending')`；workbench/methods/canvas.js `'wb-canvas-frame is-' + cfg.bp`；fragments/seo_score.html:33 `is-{{sec.Color}}`",
		values: []string{
			"info", "success", "error",
			"pending", "processing", "ready", "failed",
			"desktop", "tablet", "mobile",
			"green", "lightgreen", "yellow", "red", "red-blocking",
		},
		note: "四个拼接点各自的取值域 —— toast type {info,success,error}（toast.js:15 的注释）；媒体变体 status {pending,processing,ready,failed}（media-admin.js:331 的 texts 映射键，与 media-lib.css:213-216 的四个变体一一对应）；断点 bp {desktop,tablet,mobile}（workbench/methods/state.js:16-18）；SEO 维度色 {green,lightgreen,yellow,red,red-blocking}（workbench.css:790-794 的五个变体，另见 scoring_test.go:70 的 red-blocking）",
	},
	"is-drop-": {
		site:   "workbench/methods/tree.js:106 `classList.add('is-drop-target', 'is-drop-' + placement)`",
		values: []string{"before", "after", "inside"},
		note:   "placement 取值见同文件 111/120 行的 remove 列表",
	},
	"wb-drop-": {
		site:   "workbench/methods/canvas.js:563 `target.classList.add('wb-drop-' + placement)`",
		values: []string{"before", "after", "inside"},
		note:   "取值见 canvas.js:544-545 的 remove 列表",
	},
	"edge-": {
		site:   "automation/graph.js:205 `' edge-' + edges[e].kind`",
		values: []string{"yes", "no"},
		note:   "kind ∈ {'',yes,no}（graph.js:59 注释与 64-65 行的赋值），空值不拼类；对应 automation.css 的 .edge-yes / .edge-no",
	},
	"perm-type-": {
		site:   "admin/system/role_permissions.html:100 `class=\"perm-type perm-type-{{r.Type}}\"`",
		values: nil,
		note:   "取值域是权限类型枚举（由 DB 的 permissions.type 驱动，静态枚举不出来）→ 退回前缀放行",
	},
	"accent-": {
		site:   "admin/order/sales_overview.html:100 与 :110 `class=\"stat-card sales-card accent-{{c.Accent}}\"`",
		values: []string{"primary", "success", "info", "mute"},
		note:   "取值域是 orderhttp.salesCardViews 写死的 Accent 字面量（一排四张卡，只可能出现这四个值）；对应 theme.css 的 .accent-primary / .accent-success / .accent-info / .accent-mute —— 四者都是卡片左侧的维度色，不表达好坏",
	},
}

// TestDynamicClassEvidenceIsCurrent 放行证据必须与代码同步：
// 表里的前缀在本次扫描找不到拼接点 → 证据失效（代码改了写法或删了功能），必须更新或删条目；
// 扫描到表里没有的前缀 → 有新的动态点被静默放过，必须补「拼接点 + 取值域」。
func TestDynamicClassEvidenceIsCurrent(t *testing.T) {
	facts := collectCSSAuditFacts(t)
	for prefix, ev := range dynamicClassEvidence {
		if len(facts.dynPrefixes[prefix]) == 0 {
			t.Errorf("dynamicClassEvidence 里的前缀 %q 在本次扫描里找不到任何拼接点（登记的证据：%s）：证据已失效，请更新或删除该条", prefix, ev.site)
		}
	}
	for prefix := range facts.dynPrefixes {
		if _, ok := dynamicClassEvidence[prefix]; !ok {
			t.Errorf("扫描到动态前缀 %q（来源 %v）却没有证据条目：请先写明拼接点与取值域，再决定放行范围",
				prefix, facts.dynPrefixes[prefix])
		}
	}
}

func sortedPrefixKeys(prefixes map[string][]string) []string {
	out := make([]string, 0, len(prefixes))
	for p := range prefixes {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func head(list []string, n int) []string {
	if len(list) <= n {
		return list
	}
	return list[:n]
}

// collectCSSAuditFacts 扫描定义侧与使用侧。
func collectCSSAuditFacts(t *testing.T) cssAuditFacts {
	t.Helper()
	f := cssAuditFacts{
		defined:     map[string][]string{},
		definedOwn:  map[string][]string{},
		usedStrict:  map[string][]string{},
		looseTokens: map[string]bool{},
		goClasses:   map[string][]string{},
		dynPrefixes: map[string][]string{},
	}
	addDefined := func(file, src string, own bool) {
		for _, cls := range auditCSSClasses(src) {
			if !contains(f.defined[cls], file) {
				f.defined[cls] = append(f.defined[cls], file)
			}
			if own && !contains(f.definedOwn[cls], file) {
				f.definedOwn[cls] = append(f.definedOwn[cls], file)
			}
		}
	}

	// 定义侧：static/css/*.css
	for _, p := range mustGlob(t, "static/css/*.css") {
		addDefined(rel(t, p), readFile(t, p), true)
	}
	// 定义侧：static/vendor/**/*.css（trix 等第三方基座，模板确实在用它）
	filepath.WalkDir("static/vendor", func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(p, ".css") {
			return nil
		}
		addDefined(rel(t, p), readFile(t, p), false)
		return nil
	})

	tplFiles := walkFiles(t, ".", func(p string) bool {
		return strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".jet")
	})
	jsFiles := walkFiles(t, "static/js", func(p string) bool {
		return strings.HasSuffix(p, ".js") && !strings.HasSuffix(p, ".min.js") && !strings.HasSuffix(p, ".test.cjs")
	})

	// 使用侧（宽松）：全部模板 + 全部 JS 文本 token
	for _, p := range append(append([]string{}, tplFiles...), jsFiles...) {
		src := readFile(t, p)
		for _, tok := range reAuditLooseToken.FindAllString(src, -1) {
			f.looseTokens[tok] = true
		}
	}

	// 定义侧：模板内联 <style>
	for _, p := range tplFiles {
		src := readFile(t, p)
		for _, m := range reAuditInlineCSS.FindAllStringSubmatch(src, -1) {
			addDefined(rel(t, p)+" (内联 <style>)", reAuditJetExpr.ReplaceAllString(m[1], " "), true)
		}
	}

	// 使用侧（弱证据）：Go 代码里拼的 HTML class 字面量。
	//
	// 防的是 wb-node-actions / wb-node-flag 那次实锤盲区：它们由
	// internal/module/workbench/inbound/http/outline_handle.go 用 sb.WriteString 拼出，
	// 模板与 JS 里都看不见 —— 清账批第一遍把这两条**在用**的规则当死样式删了，
	// 靠工作台浏览器抽查发现 11 个元素带类才还原。这条扫描补的就是这个缺口。
	// 范围取整棵 internal 树（排除本包与 _test.go）：会拼 HTML 的 Go 代码不止 module 一处
	// （builder 的组件 Go、pipeline 的守卫也在拼），只扫 module 等于留同类盲区。
	// 口径刻意弱：只认能静态确定的 token，拼接（+ / %s / 反引号变量 / {{…}}）整体跳过；
	// 并且**只当「有人用」的证据**（looseTokens），不进方向 A —— Go 侧常与运行时值拼接，
	// 进 A 会把变量名当类名报红。
	for _, p := range walkFiles(t, filepath.Join("..", "..", "internal"), func(p string) bool {
		slashed := filepath.ToSlash(p)
		return strings.HasSuffix(slashed, ".go") && !strings.HasSuffix(slashed, "_test.go") &&
			!strings.HasPrefix(slashed, "../../internal/templates/")
	}) {
		for _, cls := range auditGoClassLiterals(readFile(t, p)) {
			f.looseTokens[cls] = true
			if site := rel(t, p); !contains(f.goClasses[cls], site) {
				f.goClasses[cls] = append(f.goClasses[cls], site)
			}
		}
	}

	// 使用侧（严格）：后台 / 工作台模板的 class 属性 + 控制面 JS 的类名字面量
	for _, p := range tplFiles {
		r := rel(t, p)
		if !strings.HasPrefix(r, "admin/") && !strings.HasPrefix(r, "workbench/") {
			continue
		}
		lines := strings.Split(readFile(t, p), "\n")
		for i, line := range lines {
			for _, m := range reAuditClassAttr.FindAllStringSubmatch(line, -1) {
				auditClassValue(m[1], false, r, i+1, line, f)
			}
		}
	}
	for _, p := range jsFiles {
		r := rel(t, p)
		lines := strings.Split(readFile(t, p), "\n")
		for i, line := range lines {
			for _, m := range reAuditClassAttr.FindAllStringSubmatch(line, -1) {
				auditClassValue(m[1], true, r, i+1, line, f)
			}
			auditJSCalls(line, r, i+1, f)
			for _, m := range reAuditClassName.FindAllStringSubmatch(line, -1) {
				auditJSLiterals(m[1], r, i+1, line, f)
			}
		}
	}
	for cls := range f.defined {
		sort.Strings(f.defined[cls])
	}
	for cls := range f.definedOwn {
		sort.Strings(f.definedOwn[cls])
	}
	return f
}

var (
	reGoClassRaw = regexp.MustCompile(`class="([^"]*)"`)       // 反引号字符串里的 HTML
	reGoClassEsc = regexp.MustCompile(`class=\\"([^\\"]*)\\"`) // 双引号字符串里转义的 HTML
)

// auditGoClassLiterals 从 Go 源码里取能静态确定的 class token。
// 含 `+` / 反引号 / `$` / `{{…}}`（拼接或模板变量）的值整体跳过 —— 那里面混的变量名不是类名。
func auditGoClassLiterals(src string) []string {
	var out []string
	seen := map[string]bool{}
	for _, re := range []*regexp.Regexp{reGoClassRaw, reGoClassEsc} {
		for _, m := range re.FindAllStringSubmatch(src, -1) {
			val := m[1]
			if strings.ContainsAny(val, "+$") || strings.Contains(val, "`") ||
				strings.Contains(val, "{{") || strings.Contains(val, "%") {
				continue
			}
			for _, tok := range strings.Fields(val) {
				if reAuditStaticTok.MatchString(tok) && !seen[tok] {
					seen[tok] = true
					out = append(out, tok)
				}
			}
		}
	}
	return out
}

// auditClassValue 处理 class="..." 的值。JS 里拼接出来的值（class="edge' + cls + '"）
// 只取静态前缀，避免把变量名当成类名。
func auditClassValue(val string, isJS bool, file string, line int, raw string, f cssAuditFacts) {
	if isJS && reAuditJSConcat.MatchString(val) {
		if m := reAuditStaticPrefix(val); m != "" {
			auditAddPrefix(f, m, file, line, raw)
		}
		return
	}
	if isJS {
		val = stripJSCodeNoise(val)
	}
	val = strings.ReplaceAll(reAuditJetExpr.ReplaceAllString(val, "\x00"), "\t", " ")
	for _, tok := range strings.Fields(val) {
		if strings.Contains(tok, "\x00") {
			if m := reAuditStaticPrefix(strings.SplitN(tok, "\x00", 2)[0]); m != "" {
				auditAddPrefix(f, m, file, line, raw)
			}
			continue
		}
		if reAuditStaticTok.MatchString(tok) {
			auditAddUse(f, tok, file, line, raw)
		}
	}
}

// auditJSCalls 处理 classList.add/remove/toggle(...)。参数用括号配对取值，
// 不用 [^)]* —— 参数里出现 getAttribute('x') 时正则会提前截断，把属性名当成类名。
func auditJSCalls(line, file string, lineNo int, f cssAuditFacts) {
	for _, kw := range []string{"classList.add(", "classList.remove(", "classList.toggle("} {
		idx := 0
		for {
			i := strings.Index(line[idx:], kw)
			if i < 0 {
				break
			}
			open := idx + i + len(kw) - 1
			auditJSLiterals(balancedArg(line[open:]), file, lineNo, line, f)
			idx = open + 1
		}
	}
}

// balancedArg 传入以 '(' 开头的串，返回与它配对的内容（不含两端括号）。
func balancedArg(s string) string {
	depth := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return s[1:i]
			}
		}
	}
	return s[1:]
}

// auditJSLiterals 从 JS 表达式里取类名字面量（先剔除比较值、属性名、|| 兜底值，
// 这三类都是「长得像类名但不是类名」的常见来源）。
func auditJSLiterals(expr string, file string, line int, raw string, f cssAuditFacts) {
	expr = stripJSCodeNoise(expr)
	if reAuditJSConcat.MatchString(expr) {
		// 拼接表达式：只认静态串里以 '-' 结尾的前缀（'is-drop-' + placement 之类）
		for _, m := range reAuditJSLiteral.FindAllStringSubmatch(expr, -1) {
			lit := m[1]
			if m[2] != "" {
				lit = m[2]
			}
			for _, tok := range strings.Fields(lit) {
				if reAuditDynPrefix.MatchString(tok) {
					auditAddPrefix(f, tok, file, line, raw)
				} else if reAuditStaticTok.MatchString(tok) && strings.HasPrefix(tok, "is-") {
					auditAddUse(f, tok, file, line, raw)
				}
			}
		}
		return
	}
	for _, m := range reAuditJSLiteral.FindAllStringSubmatch(expr, -1) {
		lit := m[1]
		if m[2] != "" {
			lit = m[2]
		}
		for _, tok := range strings.Fields(lit) {
			if reAuditStaticTok.MatchString(tok) {
				auditAddUse(f, tok, file, line, raw)
			}
		}
	}
}

func stripJSCodeNoise(s string) string {
	s = reAuditJSGetAttr.ReplaceAllString(s, " ")
	s = reAuditJSCompare.ReplaceAllString(s, " ")
	return reAuditJSOrDeflt.ReplaceAllString(s, " ")
}

// reAuditStaticPrefix 从拼接表达式里取动态前缀。
// 只有「以 '-' 结尾的片段」或「单个 token 后面直接跟 +」才算前缀：
//
//	'is-drop-' + placement        → is-drop-
//	class="edge' + cls + '"        → edge-
//	'media-variant-badge is-' + x  → is-
//	'btn btn-primary' + (…)        → 无（多 token 是完整类名列表；
//	                                  硬加前缀会把 btn-* 全族放行，方向 B 就废了）
func reAuditStaticPrefix(val string) string {
	head := val
	if i := strings.Index(val, "+"); i >= 0 {
		head = val[:i]
	}
	tokens := strings.Fields(head)
	if len(tokens) == 0 {
		return ""
	}
	last := tokens[len(tokens)-1]
	if i := strings.LastIndexAny(last, "'\""); i >= 0 {
		if i+1 < len(last) {
			last = last[i+1:]
		} else if j := strings.IndexAny(last, "'\""); j > 0 {
			last = last[:j] // 'edge\'' 这种：引号在末尾，静态片段在它前面
		}
	}
	if last == "" {
		return ""
	}
	if strings.HasSuffix(last, "-") {
		if reAuditDynPrefix.MatchString(last) {
			return last
		}
		return ""
	}
	if len(tokens) == 1 && reAuditStaticTok.MatchString(last) && strings.Contains(val, "+") {
		return last + "-"
	}
	return ""
}

func auditAddUse(f cssAuditFacts, cls, file string, line int, raw string) {
	site := file + ":" + strconv.Itoa(line) + ": " + strings.TrimSpace(raw)
	if !contains(f.usedStrict[cls], site) && len(f.usedStrict[cls]) < 8 {
		f.usedStrict[cls] = append(f.usedStrict[cls], site)
	}
}

func auditAddPrefix(f cssAuditFacts, prefix, file string, line int, raw string) {
	site := file + ":" + strconv.Itoa(line) + ": " + strings.TrimSpace(raw)
	if !contains(f.dynPrefixes[prefix], site) {
		f.dynPrefixes[prefix] = append(f.dynPrefixes[prefix], site)
	}
}

// auditCSSClasses 提取 CSS 里的类名。先剥注释 / url() / 字符串，
// 否则 .png、.5em 这类「长得像类名」的片段会混进来。
func auditCSSClasses(src string) []string {
	s := stripCSSComments(src)
	s = reAuditCSSURL.ReplaceAllString(s, " ")
	s = reAuditCSSDQuote.ReplaceAllString(s, " ")
	s = reAuditCSSSQuote.ReplaceAllString(s, " ")
	seen := map[string]bool{}
	var out []string
	for _, m := range reAuditCSSClass.FindAllStringSubmatch(s, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			out = append(out, m[1])
		}
	}
	return out
}

func mustGlob(t *testing.T, pattern string) []string {
	t.Helper()
	paths, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("glob %s 失败: %v", pattern, err)
	}
	if len(paths) == 0 {
		t.Fatalf("glob %s 没有任何文件：扫描范围写错了会让本门禁静默空转", pattern)
	}
	return paths
}

func walkFiles(t *testing.T, root string, keep func(string) bool) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == "static/vendor" || p == "static/js/vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if keep(p) {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历 %s 失败: %v", root, err)
	}
	sort.Strings(out)
	return out
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(b)
}

func rel(t *testing.T, path string) string {
	t.Helper()
	r, err := filepath.Rel(".", path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(r)
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// 收窄后不再判红、且已逐个核实过的钩子（登记在此备查；它们不进未命中集合，
// 写进豁免清单会立刻因「不再命中」变红 —— 那是机制的正确行为，不是漏登记）：
//
//   · 工作台 JS 生成的 7 个 wb-*（lead 要求逐条核实，结论：全部非缺样式，无需补 CSS）
//     wb-input              workbench/methods/controls/misc.js:231 —— 容器 div.wb-hint 在
//                           div.wb-field 内（misc.js:187），外观由 workbench.css:62 的
//                           `.wb-field input:not([type=checkbox]):not([type=radio])` 后代选择器提供；
//     wb-preset-item        workbench/methods/canvas.js:120 —— 与 .wb-palette-item 配对写，
//                           外观来自 workbench.css:51；
//     wb-preset-thumb       workbench/methods/canvas.js:125 —— 尺寸 / 圆角 / object-fit / 背景全由
//                           内联 style 给（该处代码注释明写「内联尺寸约束（不改 CSS 文件）」）；
//     wb-repeater-acts      workbench/methods/controls/repeater.js:242 —— 布局由父 .wb-repeater-row
//                           决定（workbench.css:317 display:flex; gap:6px; align-items:center）；
//     wb-faq-answer-field   workbench/methods/controls/repeater.js:92 —— 富文本挂载槽（ctx.panel=ansBox），
//                           内容由 richTextField 生成、外观来自 .wb-field-richtext（workbench.css:227）；
//     wb-icon-page-info     workbench/methods/controls/text.js:350 —— 纯文字 span（只 set textContent），
//                           无尺寸需求、字号靠继承；
//     wb-richtext-upload-hint workbench/methods/controls/text.js:54 —— 外观全由内联 style 给
//                           （marginTop / fontSize / lineHeight / color）。
//
//   · 模板结构钩子（收窄前 38 条，如 analytics-page / customers-page-table / stock-qty / list-plain）：
//     全是「完全新家族」的无样式钩子，无外观需求是常态。
//
//   · sre-toolbar-btn / sre-toolbar-row（原结论保留）：JS 里与 vendor 的 trix-button /
//     trix-button-row 配对写（rich-editor/toolbar.js:28、:40），基础外观来自 trix.css。
//     收窄后 sre 家族在定义侧不存在 → 不再判红，故不留在清单里。
//   · sre-attachment--accordion / --rule / --table：父名 sre-attachment 已定义（rich-editor.css:219
//     的卡片外观）→ 仍判红，故留在下面的清单里（理由已写成核实结论）。

// cssUndefinedAllowed —— 方向 A 豁免：收尾清理后只剩 1 条「无样式但被 JS 查询」的功能钩子。
var cssUndefinedAllowed = map[string]string{
	"wb-confirm-ok": "功能钩子（**不能只看类名有没有样式**）：static/js/ui/confirm.js:45 用 `dlg.querySelector('.wb-confirm-ok')` 查询它来接管确认按钮的提交；同元素另有 btn / btn-primary 基座类给外观，但真正让确认框能用的是这个查询 —— 属「无样式但有人在查」的类，**勿删**",
}

// cssUnusedAllowed —— 方向 B 豁免：清账批收口后只剩 4 条「不是死样式」的类（站点产物侧 3 条 + vendor 运行时 1 条）。
var cssUnusedAllowed = map[string]string{
	"sky-image-missing":   "站点产物侧：workbench.css:536 注释「媒体缺失占位（媒体库已删除时渲染，不阻塞编译）」—— 消费者是构建期产出的占位片段，本仓模板/JS 静态扫不到是正常的（同族实例见 internal/builder/components/socialbuttons/socialbuttons.jet 渲染的 .sky-social-fallback），**不是死样式，勿删**",
	"sky-social-fallback": "站点产物侧：由构建期组件 internal/builder/components/socialbuttons/socialbuttons.jet:1 渲染（`<span class=\"sky-social-fallback\">`），消费者在前台产物里，本仓模板/JS 零命中是正常的，**不是死样式，勿删**",
	"sky-video-missing":   "站点产物侧：workbench.css:536 注释「媒体缺失占位（媒体库已删除时渲染，不阻塞编译）」—— 消费者是构建期产出的占位片段，本仓模板/JS 静态扫不到是正常的，**不是死样式，勿删**",
	"trix-active":         "运行时触发：Trix（vendor static/vendor/trix/trix.umd.js）在工具栏按钮激活时自行加该类；rich-editor.css:165/173 的 `trix-toolbar .trix-button.trix-active` 与 vendor 行为配对，本仓静态零命中是正常的，**不是死样式，勿删**",
}
