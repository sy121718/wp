package builder

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"slices"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

func realUISources(t *testing.T) map[string]string {
	t.Helper()
	out := make(map[string]string, len(UIAssetFiles()))
	for _, name := range UIAssetFiles() {
		js, err := templates.StaticJS("ui/" + name)
		if err != nil {
			t.Fatalf("读取控件资源 %s 失败: %v", name, err)
		}
		out[name] = js
	}
	return out
}

func gzipLen(s string) int {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		return -1
	}
	if err := zw.Close(); err != nil {
		return -1
	}
	return buf.Len()
}

func perfCompile(t *testing.T, docJSON string) *CompiledPage {
	t.Helper()
	set, err := templates.NewComponentSet("../../internal/templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page, err := ParsePage([]byte(docJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	res, err := Compile(page,
		WithComponentSet(set),
		WithUISources(realUISources(t)),
		WithUIStyle(templates.UICSS()),
	)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	return res
}

func TestPerfQuantHtmxInlineBytes(t *testing.T) {
	htmx, err := templates.StaticJS("ui/htmx.min.js")
	if err != nil {
		t.Fatalf("读取 htmx.min.js: %v", err)
	}
	ver, _ := templates.StaticJS("ui/htmx.VERSION")
	t.Logf("[PERF-015] htmx 源文件字节 = %d（version=%q, gzip=%d）", len(htmx), strings.TrimSpace(ver), gzipLen(htmx))

	doc := rootDoc("[" +
		"{\"id\":\"b1\",\"type\":\"core.button\",\"props\":{\"text\":\"按钮一\",\"action\":\"internal\",\"value\":\"/a\"}}," +
		"{\"id\":\"b2\",\"type\":\"core.button\",\"props\":{\"text\":\"按钮二\",\"action\":\"internal\",\"value\":\"/b\"}}" +
		"]")
	res := perfCompile(t, doc)

	base := renderOrFatal(t, res)

	withHx := *res
	withHx.Features = nil
	withHx.HTML = res.HTML + "<span class=\"sky-x-live\" hx-get=\"/_fragments/productVariantAvailability?variantIds=1\" hx-trigger=\"load, every 60s\" hx-swap=\"innerHTML\"></span>"
	hy := renderOrFatal(t, &withHx)

	delta := len(hy) - len(base)
	t.Logf("[PERF-015] 无 hx- 页产物 = %d 字节", len(base))
	t.Logf("[PERF-015] 含 hx- 页产物 = %d 字节", len(hy))
	t.Logf("[PERF-015] 单条 hx- 属性换来的增量 = %d 字节（占含 hx- 页产物的 %.1f%%）",
		delta, 100*float64(delta)/float64(len(hy)))
	if delta > 0 {
		t.Logf("[PERF-015] 增量中 htmx 占比 = %.1f%%", 100*float64(len(htmx))/float64(delta))
	}
	t.Logf("[PERF-015] 整页 gzip: 无 hx- %d → 含 hx- %d（增量 %d 字节）",
		gzipLen(base), gzipLen(hy), gzipLen(hy)-gzipLen(base))

	sig := htmx[:32]
	if n := strings.Count(hy, sig); n != 1 {
		t.Errorf("htmx 在同一产物中出现 %d 次（应恰好 1 次）", n)
	}
}

func TestPerfQuantScopedCSSGrowth(t *testing.T) {
	var first, last int
	for _, n := range []int{1, 2, 4, 8, 16} {
		var nodes []string
		for i := 0; i < n; i++ {
			nodes = append(nodes, fmt.Sprintf(
				"{\"id\":\"btn%02d\",\"type\":\"core.button\",\"props\":{\"text\":\"按钮%02d\",\"action\":\"internal\",\"value\":\"/p%02d\",\"variant\":\"solid\",\"size\":\"md\"}}",
				i, i, i))
		}
		res := perfCompile(t, rootDoc("["+strings.Join(nodes, ",")+"]"))
		if n == 1 {
			first = len(res.CSS)
		}
		last = len(res.CSS)
		t.Logf("[PERF-016] N=%2d 实例: CSS=%6d 字节, 规则=%3d（其中带 .sky-c- 作用域 %3d）, 平均每实例 %6.0f 字节",
			n, len(res.CSS), countRules(res.CSS), countScopedRules(res.CSS), float64(len(res.CSS))/float64(n))
	}

	if grew := last - first; grew > 400 {
		t.Errorf("16 个同配置实例的 CSS 比单实例多出 %d 字节（预期 < 400）：同族规则没有被合并", grew)
	}
}

func TestPerfQuantNoResidualMergeableRules(t *testing.T) {
	var nodes []string
	const n = 8
	for i := 0; i < n; i++ {
		nodes = append(nodes, fmt.Sprintf(
			"{\"id\":\"btn%02d\",\"type\":\"core.button\",\"props\":{\"text\":\"按钮%02d\",\"action\":\"internal\",\"value\":\"/p%02d\",\"variant\":\"solid\",\"size\":\"md\"}}",
			i, i, i))
	}
	res := perfCompile(t, rootDoc("["+strings.Join(nodes, ",")+"]"))

	rules := parseRules(res.CSS)
	normalized := map[string][]int{}
	scopedRules, scopedBytes := 0, 0
	for i, r := range rules {
		if !strings.Contains(r.sel, ".sky-c-") {
			continue
		}
		scopedRules++
		scopedBytes += len(r.text)
		key := normalizeScope(r.sel) + "\x00" + r.decls
		normalized[key] = append(normalized[key], i)
	}

	var sizes []int
	mergeable, dupBytes := 0, 0
	for _, idxs := range normalized {
		sizes = append(sizes, len(idxs))
		if len(idxs) > 1 {
			mergeable += len(idxs)
			dupBytes += len(rules[idxs[0]].text) * (len(idxs) - 1)
		}
	}

	slices.Sort(sizes)
	slices.Reverse(sizes)
	shown := sizes
	if len(shown) > 10 {
		shown = shown[:10]
	}

	t.Logf("[PERF-016] 8 实例：带 .sky-c- 作用域的规则 %d 条，共 %d 字节", scopedRules, scopedBytes)
	t.Logf("[PERF-016] 归一化（scope → .sky-c-#）后分组大小分布（前 10）：%v", shown)
	t.Logf("[PERF-016] 归一化后仍重复的规则 %d / %d（%.1f%%），可省字节 %d（占作用域规则 %.1f%%）",
		mergeable, scopedRules, 100*float64(mergeable)/float64(scopedRules),
		dupBytes, 100*float64(dupBytes)/float64(scopedBytes))
	t.Logf("[PERF-016] 归一化后唯一分组 %d 个（占 %d 条作用域规则）", len(normalized), scopedRules)
	if mergeable != 0 {
		t.Errorf("残留可合并组：%d 条规则相邻、只差作用域、声明相同，却没有被合并（可省 %d 字节）",
			mergeable, dupBytes)
	}

	printed := 0
	for i, r := range rules {
		if !strings.Contains(r.sel, ".sky-c-") || printed >= 6 {
			continue
		}
		t.Logf("[PERF-016 抽样] #%d\n  原始选择器: %s\n  归一化    : %s\n  声明      : %s",
			i, r.sel, normalizeScope(r.sel), strings.ReplaceAll(r.decls, "\n", " "))
		printed++
	}
}

type cssRule struct {
	sel    string
	decls  string
	text   string
	bucket string
}

func parseRules(css string) []cssRule {
	var out []cssRule
	var stack []string
	lines := strings.Split(css, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "@") && strings.HasSuffix(line, "{"):
			stack = append(stack, strings.TrimSpace(strings.TrimSuffix(line, "{")))
			continue
		case line == "}" && len(stack) > 0:
			stack = stack[:len(stack)-1]
			continue
		case strings.HasSuffix(line, "{"):
		default:
			continue
		}
		sel := strings.TrimSpace(strings.TrimSuffix(line, "{"))
		j := i + 1
		var declLines []string
		for j < len(lines) && strings.TrimSpace(lines[j]) != "}" {
			declLines = append(declLines, strings.TrimSpace(lines[j]))
			j++
		}
		if j >= len(lines) {
			break
		}
		out = append(out, cssRule{
			sel:    sel,
			decls:  strings.Join(declLines, "\n"),
			text:   strings.Join(lines[i:j+1], "\n"),
			bucket: strings.Join(stack, " > "),
		})
		i = j
	}
	return out
}

type mergePlan struct {
	members []int
	sel     string
	before  int
	after   int
}

func planScopeMerges(css string) []mergePlan {
	rules := parseRules(css)
	groups := map[string][]int{}
	var order []string
	for i, r := range rules {
		if strings.Count(r.sel, ".sky-c-") != 1 {
			continue
		}
		key := r.bucket + "\x00" + normalizeScope(r.sel) + "\x00" + r.decls
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], i)
	}

	var plans []mergePlan
	for _, key := range order {
		idxs := groups[key]
		if len(idxs) < 2 {
			continue
		}
		contiguous := true
		for k := 1; k < len(idxs); k++ {
			if idxs[k] != idxs[k-1]+1 || rules[idxs[k]].bucket != rules[idxs[0]].bucket {
				contiguous = false
				break
			}
		}
		if !contiguous {
			continue
		}
		scopes := make([]string, 0, len(idxs))
		for _, i := range idxs {
			scopes = append(scopes, strings.TrimSpace(rules[i].sel))
		}
		mergedSel := strings.Replace(normalizeScope(rules[idxs[0]].sel), ".sky-c-#", ":is("+strings.Join(scopes, ", ")+")", 1)
		first := rules[idxs[0]].text
		declsBlock := first[strings.Index(first, "{\n")+2 : strings.LastIndex(first, "\n}")]
		mergedText := mergedSel + " {\n" + declsBlock + "\n}"
		before := 0
		for _, i := range idxs {
			before += len(rules[i].text)
		}
		plans = append(plans, mergePlan{members: idxs, sel: mergedSel, before: before, after: len(mergedText)})
	}
	return plans
}

func TestPerfQuantMultiComponentScenarios(t *testing.T) {
	sameBtn := func(n int) string {
		var nodes []string
		for i := 0; i < n; i++ {
			nodes = append(nodes, fmt.Sprintf(
				"{\"id\":\"btn%02d\",\"type\":\"core.button\",\"props\":{\"text\":\"按钮%02d\",\"action\":\"internal\",\"value\":\"/p%02d\"}}", i, i, i))
		}
		return rootDoc("[" + strings.Join(nodes, ",") + "]")
	}
	diffBtn := func(n int) string {
		var nodes []string
		palette := []string{"#111111", "#222222", "#333333", "#444444", "#555555", "#666666", "#777777", "#888888"}
		for i := 0; i < n; i++ {
			nodes = append(nodes, fmt.Sprintf(
				"{\"id\":\"btn%02d\",\"type\":\"core.button\",\"props\":{\"text\":\"按钮%02d\",\"action\":\"internal\",\"value\":\"/p%02d\",\"normal\":{\"background\":\"%s\"}}}",
				i, i, i, palette[i%len(palette)]))
		}
		return rootDoc("[" + strings.Join(nodes, ",") + "]")
	}
	mixed := func() string {
		var nodes []string
		for i := 0; i < 3; i++ {
			nodes = append(nodes, fmt.Sprintf("{\"id\":\"h%02d\",\"type\":\"core.heading\",\"props\":{\"text\":\"标题%02d\",\"tag\":\"h2\"}}", i, i))
		}
		for i := 0; i < 3; i++ {
			nodes = append(nodes, fmt.Sprintf("{\"id\":\"t%02d\",\"type\":\"core.text\",\"props\":{\"mode\":\"plaintext\",\"text\":\"正文%02d\"}}", i, i))
		}
		for i := 0; i < 3; i++ {
			nodes = append(nodes, fmt.Sprintf("{\"id\":\"i%02d\",\"type\":\"core.image\",\"props\":{\"src\":\"https://example.com/%d.jpg\",\"alt\":\"图%02d\"}}", i, i, i))
		}
		for i := 0; i < 3; i++ {
			nodes = append(nodes, fmt.Sprintf("{\"id\":\"d%02d\",\"type\":\"core.divider\",\"props\":{\"style\":\"solid\",\"weight\":\"1px\"}}", i))
		}
		return rootDoc("[" + strings.Join(nodes, ",") + "]")
	}
	cards := func(n int) string {
		var nodes []string
		for i := 0; i < n; i++ {
			nodes = append(nodes, fmt.Sprintf("{\"id\":\"c%02d\",\"type\":\"core.card\",\"props\":{\"title\":\"卡片%02d\",\"text\":\"内容%02d\"}}", i, i, i))
		}
		return rootDoc("[" + strings.Join(nodes, ",") + "]")
	}

	scenes := []struct {
		name string
		doc  string
	}{
		{"8x同配置 button", sameBtn(8)},
		{"8x不同配色 button", diffBtn(8)},
		{"3xheading+3xtext+3ximage+3xdivider", mixed()},
		{"6xcard", cards(6)},
	}
	for _, sc := range scenes {
		res := perfCompile(t, sc.doc)
		plans := planScopeMerges(res.CSS)
		rules := parseRules(res.CSS)
		covered, saved := 0, 0
		for _, p := range plans {
			covered += len(p.members)
			saved += p.before - p.after
		}
		t.Logf("[PERF-016] 场景 %-36s CSS=%6d 字节 规则=%3d 可合并组=%2d 覆盖规则=%3d 可省=%5d 字节 (%.1f%%)",
			sc.name, len(res.CSS), len(rules), len(plans), covered, saved, 100*float64(saved)/float64(len(res.CSS)))
		if len(plans) != 0 {
			t.Errorf("场景 %s 残留可合并组 %d 个（覆盖 %d 条规则）", sc.name, len(plans), covered)
		}
	}
}

func countRules(css string) int { return len(parseRules(css)) }

func countScopedRules(css string) int {
	n := 0
	for _, r := range parseRules(css) {
		if strings.Contains(r.sel, ".sky-c-") {
			n++
		}
	}
	return n
}

func normalizeScope(sel string) string {
	var sb strings.Builder
	rest := sel
	for {
		i := strings.Index(rest, ".sky-c-")
		if i < 0 {
			sb.WriteString(rest)
			return sb.String()
		}
		sb.WriteString(rest[:i])
		sb.WriteString(".sky-c-#")
		j := i + len(".sky-c-")
		for j < len(rest) && isIdentByte(rest[j]) {
			j++
		}
		rest = rest[j:]
	}
}

func isIdentByte(b byte) bool {
	return b == '-' || b == '_' || (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

func renderOrFatal(t *testing.T, c *CompiledPage) string {
	t.Helper()
	out, err := RenderDocument(c)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	return out
}
