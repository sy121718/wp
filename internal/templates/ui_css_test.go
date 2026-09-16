package templates

// ui_css_test.go — ui.css 的跨投递约束（审计 UIK-003 第一层 / UIK-004）。
//
// ui.css 同时服务**两个投递目标**：
//
//	后台页面 —— theme.css 的 [data-theme] 块把后台色板 --c-* 以同名的 --sky-c-* 暴露；
//	构建产物 —— builder.ThemeVarsCSS 按站点主题生成 --sky-c-*（产物里没有 --c-*）。
//
// 命名统一之后，这里的约束从「桥接写法」变成三条硬约束：
//
//	① 基座只写一个命名空间：ui.css 里不得出现 var(--c-*)；
//	② 每个变量引用都必须带兜底：变量在两端都可能缺失（产物侧的令牌集合随主题设置
//	   变化 —— 没配主色就根本没有 --sky-c-primary；后台侧的别名段也可能漏一条），
//	   var() 无兜底且变量未定义时整条声明被丢弃，表现是「控件丢了颜色 / 边框」，
//	   不报任何错；
//	③ 后台别名段与实际引用**一一对应**（两个方向都查）：漏一条 = 后台深色
//	   主题下那条声明退化成写死的亮色值；多一条 = 死别名，说明有人改完引用没清清单。
//	   UIK-003 第四层之后，「实际引用」不再只有 ui.css：后台静态样式（theme.css /
//	   workbench.css / media-lib.css / workbench-a11y.css）同样只写 --sky-c-*，
//	   对表的覆盖范围随之扩展到它们（见 backend_css_naming_contract_test.go）。
//
// 为什么 ① 是有意义的约束而不是表面整齐：改名之前，同一条声明在两端可能取到**不同语义**
// 的令牌（.form-input 的边框在产物侧取 --sky-c-border、在后台侧取 --c-border-input），
// 于是「这行代码在两端长什么样」必须逐处心算。统一命名后这件事只剩一个来源，
// 取值差异被收进 theme.css 的别名段（一处可查），产物侧则完全由主题生成。

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// uiCssSource 读取基座样式源。
func uiCssSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("static/css/ui.css")
	if err != nil {
		t.Fatalf("读取 ui.css 失败: %v", err)
	}
	return string(raw)
}

// themeCssSource 读取后台主题样式源。
func themeCssSource(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("static/css/theme.css")
	if err != nil {
		t.Fatalf("读取 theme.css 失败: %v", err)
	}
	return string(raw)
}

// reAnyTokenRef 抓任意 var(--x...) 引用（含无兜底形态）。
var reAnyTokenRef = regexp.MustCompile(`var\(\s*(--[a-z0-9-]+)`)

// reBackendTokenRef 抓 var(--c-xxx)（统一命名后不允许出现在基座里）。
var reBackendTokenRef = regexp.MustCompile(`var\(\s*--c-[a-z0-9-]+`)

// reSkyCRefNoFallback 抓 --sky-c-* 的无兜底引用：括号内没有逗号。
var reSkyCRefNoFallback = regexp.MustCompile(`var\(\s*--sky-c-[a-z0-9-]+\s*\)`)

// TestUICssConsumesOnlySkyNamespace 基座只消费 --sky-* 命名空间（UIK-003 第一层的机器判据）。
func TestUICssConsumesOnlySkyNamespace(t *testing.T) {
	src := uiCssStripComments(uiCssSource(t))
	if hits := reBackendTokenRef.FindAllString(src, -1); len(hits) > 0 {
		t.Errorf("ui.css 仍有 %d 处后台令牌引用 %v：基座只该写 --sky-*，后台的取值由 theme.css 的别名段供给",
			len(hits), hits[:min(3, len(hits))])
	}
}

// TestUICssTokenRefsHaveFallback ui.css 引用的令牌一律要带 fallback。
func TestUICssTokenRefsHaveFallback(t *testing.T) {
	src := uiCssStripComments(uiCssSource(t))
	if hits := reSkyCRefNoFallback.FindAllString(src, -1); len(hits) > 0 {
		t.Errorf("ui.css 有 %d 处令牌引用缺少 fallback（该变量在某一端缺失时整条声明会失效），例如 %v",
			len(hits), hits[:min(3, len(hits))])
	}
	// 解析口径自检：引用数太少说明正则或文件结构变了，本约束会在空转中通过。
	if n := len(regexp.MustCompile(`var\(\s*--sky-c-[a-z0-9-]+\s*,`).FindAllString(src, -1)); n < 40 {
		t.Errorf("ui.css 只解析出 %d 处带兜底的 --sky-c-* 引用（预期 ≥40）：解析口径可能已失效", n)
	}
}

// reThemeAlias 抓 theme.css 别名段里的 --sky-c-x: 定义。
var reThemeAlias = regexp.MustCompile(`(?m)^\s*(--sky-c-[a-z0-9-]+)\s*:\s*(.+?);`)

// backendAliases 解析后台别名段：槽名 → 值。
func backendAliases(t *testing.T) map[string]string {
	t.Helper()
	src := themeCssSource(t)
	// 只取别名段（[data-theme] 块）：别处的 --sky-c-* 定义不参与本契约。
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

// reDataThemeBlock 抓 [data-theme] { ... } 块（含选择器行）。
var reDataThemeBlock = regexp.MustCompile(`(?s)\[data-theme\]\s*\{[^}]*\}`)

// TestUICssSkyTokensHaveBackendAlias 基座引用 ↔ 后台别名对表（两个方向都查）。
func TestUICssSkyTokensHaveBackendAlias(t *testing.T) {
	// 注释里为说明写法而举的 var(--sky-c-X, 兜底) 不算引用，必须先剥掉注释再解析。
	src := uiCssStripComments(uiCssSource(t))
	used := map[string]bool{}
	for _, m := range reAnyTokenRef.FindAllStringSubmatch(src, -1) {
		if strings.HasPrefix(m[1], "--sky-c-") {
			used[m[1]] = true
		}
	}
	uiOnly := len(used)
	// UIK-003 第四层：后台静态样式也消费同一批别名，纳入对表覆盖范围。不需要别名供给的
	// 两类已在 backendStaticAliasConsumers 里剔除（本文件自有定义、已登记的无供给槽）。
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

// TestThemeCSSAliasValuesHaveFallback 别名值必须自带兜底。
//
// 写成 var(--c-x) 时，一旦 --c-x 未定义，--sky-c-x 会解析成 guaranteed-invalid：
// 引用它的声明整体失效 —— ui.css 里那层兜底救不回来（这正是别名段存在的意义）。
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
