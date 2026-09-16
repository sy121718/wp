package builder

// css_verify_multidevice_test.go — 多端硬规则守卫的断言集（审计 UI-015）。
//
// 两个方向都必须有，否则检查器可能是恒真或恒假：
//   · 故意违规的样例必须被报出来（并且要指明规则、位置）；
//   · 合规样例必须一条都不报（min() / clamp 下界 / 桶内 :hover / 注释里的说明文字）。
//
// 还有一条最容易漏的：**模式语义**。warn 不能让构建失败，error 必须真的拦住 ——
// 只测「分析函数返回了什么」证明不了这两件事，所以这里直接打在生产路径的钩子上
// （verifyAnimationRefs 就是 Compile 里唯一调用守卫的地方）。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// guardViolations 跑一次分析并返回指定规则的违规。
func guardViolations(css, scope, rule string, srcMode bool) []CSSViolation {
	var out []CSSViolation
	for _, v := range analyzeCSS(css, scope, "test.css", srcMode) {
		if v.Rule == rule {
			out = append(out, v)
		}
	}
	return out
}

// TestCSSGuardBareHover 规则 1：裸 :hover 必须报，桶内 / 注释里的不报。
func TestCSSGuardBareHover(t *testing.T) {
	cases := []struct {
		name    string
		css     string
		srcMode bool
		want    bool
	}{
		{"产物级裸 :hover", ".sky-c-1 .x:hover { color: #f00; }", false, true},
		{"产物级桶内 :hover", "@media (hover: hover) { .sky-c-1 .x:hover { color: #f00; } }", false, false},
		{"产物级复合条件也算桶", "@media (hover: hover) and (pointer: fine) { .x:hover { color: #f00; } }", false, false},
		{"图层里嵌套的桶同样有效", "@layer sky-base { @media (hover: hover) { .x:hover { color: #f00; } } }", false, false},
		{"源级裸 :hover", "& .sky-x:hover { color: #f00; }", true, true},
		{"源级 @hover 前缀", "@hover & .sky-x:hover { color: #f00; }", true, false},
		{"源级 @active 前缀里的 :hover 不算裸写", "@active & .sky-x:hover { color: #f00; }", true, false},
		{"注释里的 :hover 不算", "/* 触屏上 :hover 永不触发，靠它展开的浮层等于不存在 */\n& { color: #f00; }", true, false},
		{"字符串里的 :hover 不算", "&::after { content: \":hover\"; }", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := guardViolations(c.css, "t", ruleBareHover, c.srcMode)
			if c.want && len(got) == 0 {
				t.Fatalf("应报裸 :hover 却放行：%s", c.css)
			}
			if !c.want && len(got) > 0 {
				t.Fatalf("合规写法被误报：%s → %s", c.css, got[0].String())
			}
			if c.want && strings.TrimSpace(got[0].Selector) == "" {
				t.Errorf("报错必须带选择器定位：%s", got[0].String())
			}
		})
	}
}

// TestCSSGuardFixedWidth 规则 2：写死 px 宽度。
func TestCSSGuardFixedWidth(t *testing.T) {
	cases := []struct {
		name string
		css  string
		want bool
	}{
		{"纯 px 超过阈值", "& { width: 300px; }", true},
		{"min-width 超过阈值", "& { min-width: 180px; }", true},
		{"flex-basis 超过阈值", "& { flex-basis: 200px; }", true},
		{"带 !important 也拦", "& { width: 96px !important; }", true},
		{"min() 收口不算写死", "& { width: min(100%, 300px); }", false},
		{"clamp() 收口不算写死", "& { width: clamp(120px, 40vw, 300px); }", false},
		{"阈值内的图标尺寸不算", "& { width: 48px; }", false},
		{"阈值刚好等于不算", "& { width: 64px; }", false},
		{"max-width 是上限不算", "& { max-width: 640px; }", false},
		{"calc 相对计算不算", "& { width: calc(100% - 24px); }", false},
		{"百分比不算", "& { width: 60%; }", false},
		{"变量不算", "& { width: var(--sky-w); }", false},
		{"源里的 {{变量}} 跳过", "& { width: {{cardWidth}}; }", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := guardViolations(c.css, "t", ruleFixedWidth, true)
			if c.want && len(got) == 0 {
				t.Fatalf("应报写死宽度却放行：%s", c.css)
			}
			if !c.want && len(got) > 0 {
				t.Fatalf("合规宽度被误报：%s → %s", c.css, got[0].String())
			}
		})
	}
}

// TestCSSGuardClampLower 规则 4：clamp 下界不小于最窄视口。
func TestCSSGuardClampLower(t *testing.T) {
	cases := []struct {
		name string
		css  string
		want bool
	}{
		{"下界 800px（审计 UI-007 的实证）", "& { width: clamp(800px, 86vw, 1280px); }", true},
		{"下界等于视口宽", "& { width: clamp(375px, 86vw, 1280px); }", true},
		{"下界小于视口宽", "& { width: clamp(320px, 62vh, 760px); }", false},
		{"下界非 px（rem）", "& { font-size: clamp(3rem, 20vw, 13rem); }", false},
		{"下界是百分比", "& { width: clamp(80%, 86vw, 1280px); }", false},
		{"min() 不取下界，不算", "& { width: min(100%, 1280px); }", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := guardViolations(c.css, "t", ruleClampLower, true)
			if c.want && len(got) == 0 {
				t.Fatalf("应报 clamp 下界越界却放行：%s", c.css)
			}
			if !c.want && len(got) > 0 {
				t.Fatalf("合规 clamp 被误报：%s → %s", c.css, got[0].String())
			}
		})
	}
}

// TestCSSGuardAcceptsCompliantCSS 合规样例一条都不报（防恒真检查器）。
func TestCSSGuardAcceptsCompliantCSS(t *testing.T) {
	css := "@media (hover: hover) {\n" +
		"  .sky-c-1 .sky-card:hover {\n    transform: translateY(-2px);\n  }\n" +
		"}\n" +
		"@media (hover: none) {\n" +
		"  .sky-c-1 .sky-card {\n    box-shadow: 0 2px 8px rgba(0,0,0,.06);\n  }\n" +
		"}\n" +
		".sky-c-1 {\n  width: min(100%, 320px);\n  padding: clamp(16px, 2vw, 24px);\n}\n" +
		".sky-c-1 .sky-card-icon {\n  width: 24px;\n  height: 24px;\n}\n" +
		".sky-c-1 .sky-card-img {\n  width: min(88vw, 560px);\n  max-width: 100%;\n}"
	if got := analyzeCSS(css, "t", "t.css", false); len(got) > 0 {
		t.Fatalf("合规产物被误报 %d 条：%s", len(got), summarizeViolations(got, 3))
	}
}

// TestCSSGuardSourceScanHoverFallback 规则 3：有 hover 桶但触屏侧零输出。
//
// 判据：目录里既没有 @hovernone / AddHoverNone（触屏等价形态）也没有 @active / AddActive
// （按压反馈）。只有按压反馈的组件归到「次级观察」，不算违规 —— 审计在 UI-004 里把
// 「hover 进桶 + AddActive」视为合格，这里沿用同一口径。
func TestCSSGuardSourceScanHoverFallback(t *testing.T) {
	root := t.TempDir()
	writeTestComponent(t, root, "foo", "& .sky-foo { color: #000; }\n@hover & .sky-foo-link:hover { color: #f00; }\n")
	writeTestComponent(t, root, "bar", "& .sky-bar { color: #000; }\n@hover & .sky-bar-link:hover { color: #f00; }\n@hovernone & .sky-bar-link { color: #00f; }\n")
	writeTestComponent(t, root, "baz", "& .sky-baz { color: #000; }\n@hover & .sky-baz-link:hover { color: #f00; }\n@active & .sky-baz-link:active { opacity: .9; }\n")

	rep, err := ScanMultiDeviceCSS(root)
	if err != nil {
		t.Fatalf("扫描失败: %v", err)
	}
	hit := map[string]bool{}
	for _, v := range rep.Violations {
		if v.Rule == ruleHoverNoFallback {
			hit[v.Scope] = true
		}
	}
	if !hit["foo"] {
		t.Errorf("foo 只有 hover 桶、触屏零输出，应报违规；实际：%s", FormatCSSGuardReport(rep))
	}
	if hit["bar"] {
		t.Error("bar 有 @hovernone 触屏等价形态，不该报")
	}
	if hit["baz"] {
		t.Error("baz 有 @active 按压反馈，按审计口径不算违规")
	}
	noted := false
	for _, n := range rep.Notes {
		if strings.Contains(n, "baz") {
			noted = true
		}
	}
	if !noted {
		t.Error("只有按压反馈的组件应进次级观察，便于整改时统一补形态")
	}
}

// TestCSSGuardModes warn 不拦、error 拦、off 不检查（打在生产路径的钩子上）。
func TestCSSGuardModes(t *testing.T) {
	bad := ".sky-c-1 .sky-x:hover { color: #f00; }"
	good := "@media (hover: hover) { .sky-c-1 .sky-x:hover { color: #f00; } }"

	t.Setenv(CSSGuardModeEnv, CSSGuardModeOff)
	if err := verifyAnimationRefs(bad); err != nil {
		t.Fatalf("off 模式不该报错: %v", err)
	}

	t.Setenv(CSSGuardModeEnv, CSSGuardModeWarn)
	if err := verifyAnimationRefs(bad); err != nil {
		t.Fatalf("warn 模式不该拦住构建: %v", err)
	}

	t.Setenv(CSSGuardModeEnv, CSSGuardModeError)
	err := verifyAnimationRefs(bad)
	if err == nil {
		t.Fatal("error 模式必须拦住违规产物")
	}
	if !strings.Contains(err.Error(), ruleBareHover) {
		t.Errorf("拦下的错误信息必须指明是哪条规则: %v", err)
	}
	if err := verifyAnimationRefs(good); err != nil {
		t.Fatalf("error 模式下合规产物不该被拦: %v", err)
	}
}

// TestCSSGuardWarnModeCompileSucceeds 真实编译路径在 warn 模式下不失败（本批口径：
// 违规清单先亮出来，整改是后续批次，不能让现有构建挂掉）。
func TestCSSGuardWarnModeCompileSucceeds(t *testing.T) {
	t.Setenv(CSSGuardModeEnv, CSSGuardModeWarn)
	doc := "{\n" +
		"  \"settings\": {\"layout\": {\"mode\": \"full\"}, \"seo\": {\"title\": \"守卫测试\"}},\n" +
		"  \"root\": [{\"id\": \"b1\", \"type\": \"core.button\", \"props\": {\"action\": \"external\", \"text\": \"按钮\", \"value\": \"https://example.com/x\"}}]\n" +
		"}"
	page, err := ParsePage([]byte(doc))
	if err != nil {
		t.Fatalf("页面文档解析失败: %v", err)
	}
	set, err := templates.NewEmbeddedComponentSet()
	if err != nil {
		t.Fatalf("组件模板 Set 加载失败: %v", err)
	}
	compiled, err := Compile(page, WithComponentSet(set))
	if err != nil {
		t.Fatalf("warn 模式下构建不该失败: %v", err)
	}
	if compiled == nil || compiled.CSS == "" {
		t.Fatal("产物 CSS 为空，用例失效")
	}
	if got := analyzeCSS(compiled.CSS, "compiled-page", "", false); len(got) > 0 {
		t.Logf("注意：本例产物本身命中 %d 条（warn 不拦）：%s", len(got), summarizeViolations(got, 3))
	}
}

// TestCSSGuardExemptionsCarryReasons 豁免必须写理由，且理由要有实质内容。
func TestCSSGuardExemptionsCarryReasons(t *testing.T) {
	if err := validateCSSGuardExemptions(); err != nil {
		t.Fatalf("豁免清单不合规: %v", err)
	}
	if len(cssGuardExemptions) == 0 {
		t.Fatal("豁免清单为空时本用例是空转的（若确实清空了豁免，请连同本断言一起删）")
	}
	for i, ex := range cssGuardExemptions {
		if strings.TrimSpace(ex.Match) == "" {
			t.Errorf("豁免第 %d 条缺少匹配目标", i+1)
		}
		if len([]rune(strings.TrimSpace(ex.Reason))) < 12 {
			t.Errorf("豁免第 %d 条理由过短: %q", i+1, ex.Reason)
		}
	}
	// 豁免真的生效（不是清单摆着好看）
	kept, exempted := applyCSSGuardExemptions([]CSSViolation{
		{Rule: ruleHoverNoFallback, Scope: "marquee", File: "a.css", Line: 1, Detail: "x"},
		{Rule: ruleHoverNoFallback, Scope: "accordion", File: "b.css", Line: 2, Detail: "y"},
	})
	if len(exempted) != 1 || len(kept) != 1 {
		t.Fatalf("豁免过滤没按预期工作：kept=%d exempted=%d", len(kept), len(exempted))
	}
	if kept[0].Scope != "accordion" {
		t.Errorf("未在豁免清单里的组件不该被放过: %s", kept[0].Scope)
	}
}

// TestCSSGuardScansRealComponents 对真实仓库跑一遍（warn 语义：只打印清单，不失败）。
//
// 这条用例的价值是「清单随时可复现」：CI 与本地拿到的是同一份数据。
// 它同时防了空转 —— 组件数或样式文件数为 0 说明扫描路径写错了。
func TestCSSGuardScansRealComponents(t *testing.T) {
	t.Setenv(CSSGuardModeEnv, CSSGuardModeWarn)
	root := testRepoRoot(t)
	rep, err := ScanMultiDeviceCSS(root)
	if err != nil {
		t.Fatalf("扫描真实仓库失败: %v", err)
	}
	if rep.Components == 0 || rep.Files == 0 {
		t.Fatalf("扫描空转：组件 %d / 文件 %d", rep.Components, rep.Files)
	}
	t.Logf("真实扫描：%d 个组件 / %d 个样式文件 / 违规 %d 条（豁免 %d 条）",
		rep.Components, rep.Files, len(rep.Violations), len(rep.Exempted))
	t.Log(FormatCSSGuardReport(rep))
}

// writeTestComponent 在临时仓库根下造一个组件样式源。
func writeTestComponent(t *testing.T, root, comp, body string) {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(CSSGuardComponentsDir), comp)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, comp+".css"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// testRepoRoot 从当前测试目录向上找 go.mod（internal/builder → 仓库根）。
func testRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("未找到 go.mod")
		}
		dir = parent
	}
}
