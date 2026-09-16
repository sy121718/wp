package builder

// perf_asset_bytes_quant_test.go — PERF-015 / PERF-016 的产物字节量化（先测量，后决策）。
//
// 两条审计条目都声称「产物白送字节」，但字节数必须实测才能判断优化空间：
//   PERF-015：htmx.min.js 内联进每个含 hx- 属性的产物页面；
//   PERF-016：同类型组件多实例各自一份 scoped 规则，CSS 随实例数线性增长。
//
// 本文件只做测量与判断，不改产物。

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"sort"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// realUISources 取真实的控件资源源码（与 pipeline 装配层同一条读取路径）。
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

// perfCompile 用真实资源编译一个页面文档。
func perfCompile(t *testing.T, docJSON string) *CompiledPage {
	t.Helper()
	set, err := templates.NewComponentSet("../templates/components")
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

// TestPerfQuantHtmxInlineBytes PERF-015 量化：htmx 源字节 vs 产物字节。
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

	// A：原样产物（无 hx- 特征）
	base := renderOrFatal(t, res)

	// B：把一行真实的 htmx 用法（等价 product.jet 的实时库存行）插进 HTML。
	//
	// Features 置 nil（审计 PERF-014）：这段 hx- 是**手工拼进 HTML** 的，编译期登记表里
	// 当然没有它 —— 置 nil 让 RenderDocument 回退到扫描 HTML 那条路径，量化口径与实施
	// 登记制之前完全一致（否则这里量的是「登记表里有没有」，不是「HTML 字节值多少」）。
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

	// 防重复内联：同一份产物里 htmx 只应出现一次。
	sig := htmx[:32]
	if n := strings.Count(hy, sig); n != 1 {
		t.Errorf("htmx 在同一产物中出现 %d 次（应恰好 1 次）", n)
	}
}

// TestPerfQuantScopedCSSGrowth PERF-016 量化 + 回归护栏：同类型组件实例数 → CSS 字节。
//
// 合并前实测：N=1 827 → N=2 1327 → N=4 2327 → N=8 4327 → N=16 8327 字节
// （每个实例约 +500 字节，严格线性）。合并后只随并列选择器本身的长度增长。
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
	// 16 个实例的 CSS 不得再随实例数线性膨胀：并列选择器本身只值几十字节，
	// 给 4 倍余量（修复前这个差值是 7500 字节）。
	if grew := last - first; grew > 400 {
		t.Errorf("16 个同配置实例的 CSS 比单实例多出 %d 字节（预期 < 400）：同族规则没有被合并", grew)
	}
}

// TestPerfQuantNoResidualMergeableRules PERF-016 回归护栏：产物里不应残留
// 「相邻、只差作用域、声明相同」的规则组。
//
// 判据与生产实现一致（planScopeMerges 复刻了 core 的三条安全边界）。有残留就意味着
// 某个桶没有接入合并（例如新增了一种包裹形态的外壳而没同步处理）。
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
	sort.Sort(sort.Reverse(sort.IntSlice(sizes)))
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
	bucket string // 所属外层块（@media / @layer …）串联标识：跨桶的规则不能合并
}

// parseRules 把产物 CSS 切分成分层之前/之内的全部规则（行状态机）。
// @media / @layer / @container 外壳行本身不产出规则，但会记入 bucket：
// 合并只能发生在同一个桶内，跨桶合并会改变响应式语义。
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

// mergePlan 一个可合并组：同桶、同归一化选择器、同声明、且成员在桶内相邻。
type mergePlan struct {
	members []int
	sel     string // 合并后的选择器（:is(...) 形态）
	before  int    // 合并前这些规则的字节
	after   int    // 合并后字节
}

// planScopeMerges 按「保守但严格等价」的判据找出可合并组：
//  1. 选择器折叠掉实例 scope 后完全相同；
//  2. 声明完全相同；
//  3. 同一外层桶（@media / @layer 不跨越）；
//  4. 成员在输出序列里**相邻**（中间没有任何其它规则）——
//     这条是安全边界：只要中间无规则，把成员折叠到首个成员的位置就不改变
//     与任何其它规则的先后关系，因此优先级语义逐字节等价。
//  5. 选择器里恰好一个 .sky-c- 占位（多占位需要实例间映射关系，不在此处理）。
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

// TestPerfQuantMultiComponentScenarios 多组件 / 多配置场景下的可提取比例。
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

// normalizeScope 把实例作用域类名折叠掉，用于判断「规则内容是否只差 scope」。
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
