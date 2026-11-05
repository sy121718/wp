package templates

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

func uiCssSource(t *testing.T) string {
	t.Helper()
	raw, err := AssetFile("css/ui.css")
	if err != nil {
		t.Fatalf("读取 ui.css 失败: %v", err)
	}
	return string(raw)
}

func themeCssSource(t *testing.T) string {
	t.Helper()
	raw, err := AssetFile("css/theme.css")
	if err != nil {
		t.Fatalf("读取 theme.css 失败: %v", err)
	}
	return string(raw)
}

var reAnyTokenRef = regexp.MustCompile(`var\(\s*(--[a-z0-9-]+)`)

var reBackendTokenRef = regexp.MustCompile(`var\(\s*--c-[a-z0-9-]+`)

var reSkyCRefNoFallback = regexp.MustCompile(`var\(\s*--sky-c-[a-z0-9-]+\s*\)`)

func TestUICssConsumesOnlySkyNamespace(t *testing.T) {
	src := uiCssStripComments(uiCssSource(t))
	if hits := reBackendTokenRef.FindAllString(src, -1); len(hits) > 0 {
		t.Errorf("ui.css 仍有 %d 处后台令牌引用 %v：基座只该写 --sky-*，后台的取值由 theme.css 的别名段供给",
			len(hits), hits[:min(3, len(hits))])
	}
}

func TestUICssTokenRefsHaveFallback(t *testing.T) {
	src := uiCssStripComments(uiCssSource(t))
	if hits := reSkyCRefNoFallback.FindAllString(src, -1); len(hits) > 0 {
		t.Errorf("ui.css 有 %d 处令牌引用缺少 fallback（该变量在某一端缺失时整条声明会失效），例如 %v",
			len(hits), hits[:min(3, len(hits))])
	}

	if n := len(regexp.MustCompile(`var\(\s*--sky-c-[a-z0-9-]+\s*,`).FindAllString(src, -1)); n < 40 {
		t.Errorf("ui.css 只解析出 %d 处带兜底的 --sky-c-* 引用（预期 ≥40）：解析口径可能已失效", n)
	}
}

var reThemeAlias = regexp.MustCompile(`(?m)^\s*(--sky-c-[a-z0-9-]+)\s*:\s*(.+?);`)

func backendAliases(t *testing.T) map[string]string {
	t.Helper()
	src := themeCssSource(t)

	block := reDataThemeBlock.FindString(src)
	if block == "" {
		t.Fatal("theme.css 缺少 [data-theme] 块：后台侧的基座令牌别名无处安放")
	}
	out := map[string]string{}
	for _, m := range reThemeAlias.FindAllStringSubmatch(block, -1) {
		out[m[1]] = strings.TrimSpace(m[2])
	}
	return out
}

var reDataThemeBlock = regexp.MustCompile(`(?s)\[data-theme\]\s*\{[^}]*\}`)

func TestUICssSkyTokensHaveBackendAlias(t *testing.T) {

	src := uiCssStripComments(uiCssSource(t))
	used := map[string]bool{}
	for _, m := range reAnyTokenRef.FindAllStringSubmatch(src, -1) {
		if strings.HasPrefix(m[1], "--sky-c-") {
			used[m[1]] = true
		}
	}
	uiOnly := len(used)

	for name := range backendStaticAliasConsumers(t) {
		used[name] = true
	}
	if uiOnly < 15 {
		t.Fatalf("ui.css 只引用了 %d 个 --sky-c-* 槽（预期 ≥15）：解析口径可能已失效", uiOnly)
	}
	aliases := backendAliases(t)
	if len(aliases) < 15 {
		t.Fatalf("后台别名段只有 %d 条（预期 ≥15）：别名段可能没解析到", len(aliases))
	}

	var missing, unused []string
	for name := range used {
		if _, ok := aliases[name]; !ok {
			missing = append(missing, name)
		}
	}
	for name := range aliases {
		if !used[name] {
			unused = append(unused, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(unused)
	if len(missing) > 0 {
		t.Errorf("这些令牌被 ui.css 或后台静态样式引用，后台别名段却没有供给 %v：\n"+
			"后台深色主题下，引用它们的声明会退化成写死的亮色值（例如主色落到 #3d444f、面板落到 #fff）", missing)
	}
	if len(unused) > 0 {
		t.Errorf("后台别名段有无人引用的槽 %v：死别名会让「哪些槽还受支持」变得含糊，请一并删掉", unused)
	}
}

func TestThemeCSSAliasValuesHaveFallback(t *testing.T) {
	aliases := backendAliases(t)
	noFallback := regexp.MustCompile(`var\(\s*--c-[a-z0-9-]+\s*\)`)
	for name, val := range aliases {
		if noFallback.MatchString(val) {
			t.Errorf("别名 %s 的值 %q 缺兜底：--c-* 缺失时它会变成 guaranteed-invalid，反而比不写别名更糟", name, val)
		}
		if !strings.HasPrefix(strings.TrimSpace(val), "var(--c-") {
			t.Errorf("别名 %s 的值 %q 不指向后台色板 --c-*：后台取值就成了第二份真源", name, val)
		}
	}
}
