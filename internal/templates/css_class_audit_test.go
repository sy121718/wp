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
// 已知让步（写在这里避免下一个人误判）：
//   · 动态前缀（JS 的 'is-' + x、模板的 is-{{…}}）按「前缀覆盖」处理，不逐条豁免。
//     代价是 is- 前缀会连同 is-success / is-green 这类真死样式一起放行 ——
//     被放行的类会以 t.Log 列出，供人工复核（见 TestCSSClassesDefinedButUnused）。
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
	dynPrefixes map[string][]string // 动态前缀 → 来源
}

// TestCSSClassesUsedButUndefined 方向 A：模板/JS 里写死的类名必须在定义侧存在。
// 防的就是 media-admin.js 那类缺陷 —— 生成控件时漏了（或写错）类名，页面不报错、只是没样式。
func TestCSSClassesUsedButUndefined(t *testing.T) {
	facts := collectCSSAuditFacts(t)

	missing := auditMissing(facts.usedStrict, func(cls string) bool {
		if _, ok := facts.defined[cls]; ok {
			return false
		}
		return !auditPrefixCovered(facts.dynPrefixes, cls)
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

	unused, prefixCovered := auditUnused(facts.definedOwn, facts.usedStrict, facts.looseTokens, facts.dynPrefixes, cssUnusedAllowed)
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

	// 被动态前缀放行的类：不失败，但要留痕 —— 前缀覆盖是让步，不是结论。
	if len(prefixCovered) > 0 {
		sorted := make([]string, 0, len(prefixCovered))
		for cls := range prefixCovered {
			sorted = append(sorted, cls)
		}
		sort.Strings(sorted)
		t.Logf("以下 %d 个类靠动态前缀被放行（前缀 %s），若要精确判定请人工复核：%s",
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
func auditUnused(defined map[string][]string, strict map[string][]string, loose map[string]bool, prefixes map[string][]string, allowed map[string]string) ([]string, map[string]bool) {
	var unused []string
	prefixCovered := map[string]bool{}
	for cls := range defined {
		if loose[cls] {
			continue // 使用侧文本里出现过（连注释也算）→ 有人提过，不判死
		}
		if len(strict[cls]) > 0 {
			continue
		}
		if auditPrefixCovered(prefixes, cls) {
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
		case auditPrefixCovered(f.dynPrefixes, cls):
			stale = append(stale, cls) // 已被动态前缀接管 → 不必逐条豁免
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
		case auditPrefixCovered(f.dynPrefixes, cls):
			stale = append(stale, cls) // 已被动态前缀放行 → 不必逐条豁免
		}
	}
	sort.Strings(stale)
	return stale
}

func auditPrefixCovered(prefixes map[string][]string, cls string) bool {
	for p := range prefixes {
		if strings.HasPrefix(cls, p) {
			return true
		}
	}
	return false
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

// cssUndefinedAllowed —— 方向 A 豁免：确认是纯结构/JS 钩子、不需要外观的类。
var cssUndefinedAllowed = map[string]string{
	"analytics-page":            "模板结构钩子（admin/analytics/analytics.html:64），定义侧零命中：无外观需求，待复核是否可删",
	"article-edit-grid":         "模板结构钩子（admin/content/article_new.html:29），定义侧零命中：无外观需求，待复核是否可删",
	"article-edit-main":         "模板结构钩子（admin/content/article_edit.html:108），定义侧零命中：无外观需求，待复核是否可删",
	"article-new-page":          "模板结构钩子（admin/content/article_new.html:14），定义侧零命中：无外观需求，待复核是否可删",
	"article-preview-frame":     "模板结构钩子（admin/content/article_new.html:78），定义侧零命中：无外观需求，待复核是否可删",
	"articles-page":             "模板结构钩子（admin/content/articles.html:19），定义侧零命中：无外观需求，待复核是否可删",
	"auto-stage":                "JS 运行时生成元素的类（static/js/automation/canvas.js:60），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"cell-body":                 "模板结构钩子（admin/comment/comments.html:119），定义侧零命中：无外观需求，待复核是否可删",
	"check-inline":              "模板结构钩子（admin/inventory/inventory.html:176），定义侧零命中：无外观需求，待复核是否可删",
	"coupons-page":              "模板结构钩子（admin/order/coupons.html:37），定义侧零命中：无外观需求，待复核是否可删",
	"coupons-page-table":        "模板结构钩子（admin/order/coupons.html:155），定义侧零命中：无外观需求，待复核是否可删",
	"customer-detail-page":      "模板结构钩子（admin/user/customer_detail.html:10），定义侧零命中：无外观需求，待复核是否可删",
	"customers-page":            "模板结构钩子（admin/user/customers.html:25），定义侧零命中：无外观需求，待复核是否可删",
	"customers-page-table":      "模板结构钩子（admin/user/customers.html:138），定义侧零命中：无外观需求，待复核是否可删",
	"datarule-logic":            "模板结构钩子（admin/system/datarule_config_editor.html:35），定义侧零命中：无外观需求，待复核是否可删",
	"field":                     "模板结构钩子（admin/content/article_translations.html:101），定义侧零命中：无外观需求，待复核是否可删",
	"has-icon":                  "JS 运行时生成元素的类（static/js/ui/iconfield.js:50），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"hits-head":                 "模板结构钩子（admin/product/product_tag_hits.html:23），定义侧零命中：无外观需求，待复核是否可删",
	"list-plain":                "模板结构钩子（admin/product/products_new.html:34），定义侧零命中：无外观需求，待复核是否可删",
	"media-detail-form":         "JS 运行时生成元素的类（static/js/media-admin.js:279），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"media-gallery":             "模板结构钩子（admin/partials/media_gallery.html:19），定义侧零命中：无外观需求，待复核是否可删",
	"media-gallery-hint":        "模板结构钩子（admin/partials/media_gallery.html:33），定义侧零命中：无外观需求，待复核是否可删",
	"media-pick-detail-body":    "JS 运行时生成元素的类（static/js/ui/mediafield.js:132），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"media-tree-item":           "JS 运行时生成元素的类（static/js/media-lib.js:53），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"media-tree-search":         "模板结构钩子（admin/media/media.html:17），定义侧零命中：无外观需求，待复核是否可删",
	"nav-source-title":          "模板结构钩子（admin/navigation/navigations.html:157），定义侧零命中：无外观需求，待复核是否可删",
	"order-new-page":            "模板结构钩子（admin/order/order_new.html:26），定义侧零命中：无外观需求，待复核是否可删",
	"orders-page":               "模板结构钩子（admin/order/orders.html:25），定义侧零命中：无外观需求，待复核是否可删",
	"orders-page-table":         "模板结构钩子（admin/order/orders.html:134），定义侧零命中：无外观需求，待复核是否可删",
	"receipt-head":              "模板结构钩子（admin/inventory/inventory_purchases.html:198），定义侧零命中：无外观需求，待复核是否可删",
	"receipt-row":               "模板结构钩子（admin/inventory/inventory_purchases.html:197），定义侧零命中：无外观需求，待复核是否可删",
	"returns-page":              "模板结构钩子（admin/order/returns.html:26），定义侧零命中：无外观需求，待复核是否可删",
	"sidebar-pinned":            "JS 运行时生成元素的类（static/js/admin.js:74），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"site-slots-page":           "模板结构钩子（admin/page/page_redirects.html:43），定义侧零命中：无外观需求，待复核是否可删",
	"site-slots-table":          "模板结构钩子（admin/page/site_slots.html:84），定义侧零命中：无外观需求，待复核是否可删",
	"sre-attachment--accordion": "JS 运行时生成元素的类（static/js/rich-editor/accordion.js:87），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"sre-attachment--rule":      "JS 运行时生成元素的类（static/js/rich-editor/horizontal-rule.js:50），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"sre-attachment--table":     "JS 运行时生成元素的类（static/js/rich-editor/table.js:208），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"sre-toolbar-btn":           "JS 生成的按钮同时带 vendor 的 trix-button（rich-editor/toolbar.js:28），基础外观来自 trix.css；本类只作 .sre-toolbar-btn--active 变体（rich-editor.css:167）的钩子 —— lead 已核实，非缺样式",
	"sre-toolbar-row":           "JS 生成的容器同时带 vendor 的 trix-button-row（rich-editor/toolbar.js:40），外观来自 trix.css —— lead 已核实，非缺样式",
	"stack-sm":                  "模板结构钩子（admin/content/article_edit.html:226），定义侧零命中：无外观需求，待复核是否可删",
	"stock-cell":                "模板结构钩子（admin/product/products.html:190），定义侧零命中：无外观需求，待复核是否可删",
	"stock-qty":                 "模板结构钩子（admin/inventory/inventory.html:182），定义侧零命中：无外观需求，待复核是否可删",
	"stock-rows":                "模板结构钩子（admin/product/products.html:200），定义侧零命中：无外观需求，待复核是否可删",
	"stock-wh":                  "模板结构钩子（admin/product/products.html:203），定义侧零命中：无外观需求，待复核是否可删",
	"tab":                       "有意无样式：按钮外观挂在 .tab-list > [role=\"tab\"]（ui.css 页签段的既定写法），.tab 只是标记类",
	"tr-component":              "模板结构钩子（admin/page/page_translations.html:141），定义侧零命中：无外观需求，待复核是否可删",
	"tr-group":                  "模板结构钩子（admin/page/page_translations.html:134），定义侧零命中：无外观需求，待复核是否可删",
	"tr-table-wrap":             "模板结构钩子（admin/product/product_translations.html:86），定义侧零命中：无外观需求，待复核是否可删",
	"wb-confirm-ok":             "JS 运行时生成元素的类（static/js/ui/confirm.js:39），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"wb-faq-answer-field":       "JS 运行时生成元素的类（static/js/workbench/methods/controls/repeater.js:92），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"wb-icon-page-info":         "JS 运行时生成元素的类（static/js/workbench/methods/controls/text.js:350），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"wb-input":                  "JS 运行时生成元素的类（static/js/workbench/methods/controls/misc.js:231），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"wb-preset-item":            "JS 运行时生成元素的类（static/js/workbench/methods/canvas.js:120），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"wb-preset-thumb":           "JS 运行时生成元素的类（static/js/workbench/methods/canvas.js:125），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"wb-repeater-acts":          "JS 运行时生成元素的类（static/js/workbench/methods/controls/repeater.js:242），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"wb-richtext-upload-hint":   "JS 运行时生成元素的类（static/js/workbench/methods/controls/text.js:54），定义侧零命中：状态/结构钩子，待复核是否缺样式",
	"wb-seo-panel":              "模板结构钩子（admin/product/entity_seo_drawer.html:30），定义侧零命中：无外观需求，待复核是否可删",
	"wbd-blank":                 "JS 生成的月初空位 span（ui/daterange.js:301），作为 .wbd-grid 的 grid item 由轨道定尺寸 —— lead 已核实，非缺样式",
	"wbd-month":                 "JS 生成的月份容器（ui/daterange.js:287），尺寸由父 .wbd-months 的 grid 轨道决定（ui.css:148），无需自身外观 —— lead 已核实，非缺样式",
	"wbd-title":                 "JS 生成的月份标题（ui/daterange.js:337），位于 .wbd-titles 网格内（ui.css:135），样式与文字靠继承 —— lead 已核实，非缺样式",
}

// cssUnusedAllowed —— 方向 B 豁免：定义侧存在但使用侧零出现的类。
var cssUnusedAllowed = map[string]string{
	"attr-fold":               "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"card-footer":             "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"dot-danger":              "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"dot-mute":                "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"dot-success":             "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"dot-warning":             "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"entity-row":              "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"field-counter":           "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"filter-more":             "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"filter-spacer":           "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"form-row-2":              "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"gap-lg":                  "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"gap-md":                  "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"inline-form":             "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"items-center":            "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"justify-between":         "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"mb-md":                   "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"mb-sm":                   "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"mb-xl":                   "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"media-flex-spacer":       "使用侧 token 零命中（定义在 static/css/media-lib.css）：疑似死样式，待复核后随下一批清理",
	"media-toolbar":           "使用侧 token 零命中（定义在 static/css/media-lib.css）：疑似死样式，待复核后随下一批清理",
	"mt-md":                   "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"mt-sm":                   "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"mt-xl":                   "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"nav-panel-preview":       "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"nav-panel-preview-frame": "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"nav-panel-preview-head":  "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"page-title":              "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"preview-thumb-empty":     "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"receipt-form":            "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"sky-image-missing":       "站点产物侧类名（static/css/workbench.css）：由前台构建期产物使用，本仓模板/JS 零命中",
	"sky-social-fallback":     "站点产物侧类名（static/css/workbench.css）：由前台构建期产物使用，本仓模板/JS 零命中",
	"sky-video-missing":       "站点产物侧类名（static/css/workbench.css）：由前台构建期产物使用，本仓模板/JS 零命中",
	"subnav-divider":          "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"text-right":              "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"toolbar-search":          "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"tr-toolbar":              "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"trix-active":             "Trix 运行时（vendor JS）给编辑器加的类，本仓模板/JS 零命中",
	"var-fold":                "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"var-form":                "使用侧 token 零命中（定义在 static/css/theme.css）：疑似死样式，待复核后随下一批清理",
	"w-full":                  "死样式（使用侧 token 零命中，lead 已用 PurgeCSS 复核），待清理批次处理",
	"wb-acc-arrow":            "使用侧 token 零命中（定义在 static/css/workbench-a11y.css, static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-acc-count":            "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-acc-head":             "使用侧 token 零命中（定义在 static/css/workbench-a11y.css, static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-acc-title":            "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-block-card":           "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-brand":                "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-dim-label":            "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-field-label":          "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-group-tabs":           "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-icon-inline":          "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-inspector-empty":      "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-media-pick":           "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-media-type":           "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-node-actions":         "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-node-flag":            "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-node-type":            "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-palette-grid":         "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-palette-head":         "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-rail-spacer":          "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
	"wb-state-tabs":           "使用侧 token 零命中（定义在 static/css/workbench.css）：疑似死样式，待复核后随下一批清理",
}
