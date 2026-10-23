package builder

// component_token_fallback_test.go — 组件 CSS 主题令牌兜底的一致性门禁（审计修复 P1/P2）。
//
// 背景：组件 CSS 经 var(--sky-c-X, <fallback>) 消费站点主题令牌。ThemeVarsCSS 只为
// 有主题的页面输出 :root 变量；**无主题页面里 fallback 就是真实生效值**。历史上同一
// --sky-c-primary 的 fallback 有五种取值（#2563eb / #0084ff / #5e5cfc / #1d4ed8 /
// #111827），无主题时各组件各画各的蓝 —— 而模板侧的 ui_token_fallback_test.go 只扫
// 后台七份 CSS，组件层无人守护。
//
// 本测试的判据：
//  1. 通用令牌（--sky-c-*）必须在下方登记表内，且 fallback 落在该令牌的允许值集 ——
//     新增通用令牌或新兜底值：先加登记表（写清语义），否则红；
//  2. 孤儿令牌名禁止：--sky- 直接跟通用色词（--sky-danger 这类）永远等不到主题供值，
//     正确写法是 --sky-c-danger（ThemeVarsCSS 的 add() 只输出 --sky-c-* 一族）；
//  3. 解析自检：一个 --sky-c-* 引用都没扫到说明正则/路径失效，测试在空转。
//
// 不在管辖范围：--sky-<组件名>-* 私有令牌（cardstack-text 等，组件自治）、主题必供值
// 的 --sky-btn-* / --sky-density-* / --sky-heading-* 等（ThemeVarsCSS 全量输出，fallback
// 只是防御）、结构私有令牌（--sky-deck-* / --sky-al-*）。

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// componentTokenFallbacks 通用令牌登记表：令牌 → 允许的 fallback 值集。
// 取值对齐站点默认视觉（showcase 蓝图主色 #2563eb + Tailwind 灰阶），
// 与 theme.css 后台桥接层的兜底同族（名字统一、取值各供）。
var componentTokenFallbacks = map[string][]string{
	"--sky-c-primary":       {"#2563eb"},                              // 主色：showcase 默认品牌蓝（blue-600）
	"--sky-c-primary-deep":  {"#1d4ed8"},                              // 主色深档（blue-700）：hover / 角标底
	"--sky-c-primary-weak":  {"rgba(37,99,235,0.08)"},                 // 主色 8% 浅底：选中态
	"--sky-c-surface":       {"#fff"},                                 // 卡片/控件表面
	"--sky-c-surface-muted": {"#f3f4f6"},                              // 浅灰表面：表头底纹 / 手风琴 hover
	"--sky-c-surface-alt":   {"rgba(0,0,0,0.03)", "rgba(0,0,0,0.05)"}, // 两档语义：图片占位底 / 胶囊标签底
	"--sky-c-text":          {"#111827"},                              // 正文（gray-900）
	"--sky-c-text-muted":    {"#6b7280"},                              // 弱化文字（gray-500）
	"--sky-c-text-mute":     {"#6b7280"},                              // 辅助指示（chevron 等；与 muted 同值）
	"--sky-c-muted":         {"#6b7280"},                              // 弱化文字（历史别名，同值）
	"--sky-c-heading":       {"#000"},                                 // 标题（无主题时纯黑）
	"--sky-c-border":        {"#e5e7eb"},                              // 标准边框（gray-200，与后台 --c-border 同值）
	"--sky-c-border-strong": {"#d1d5db"},                              // 深档边框（gray-300，与 --c-border-strong 同值）
	"--sky-c-bg-soft":       {"#f4f5f6"},                              // 浅灰页面底（与后台 --c-bg-soft 同值）
	"--sky-c-danger":        {"#dc2626"},                              // 危险（red-600，与后台 --c-danger 同值）
	"--sky-c-warning":       {"#f59e0b"},                              // 警告（amber-500）

	// —— 尺度令牌（间距 / 圆角 / 字号）——
	//
	// 这一族与 --sky-c-* 的区别：颜色令牌由主题供值（ThemeVarsCSS 输出 --sky-c-*），
	// 尺度令牌**不由主题供值** —— 它是组件样式自己的档位，兜底值是唯一真值。
	// 之所以也走 var()，是为了将来能在主题层覆写密度（--sky-density-* 已经这么做）。
	// 因此登记形式是「令牌 → 唯一允许兜底值」，判据仍是「同一令牌全仓一种兜底」。
	"--sky-space-1": {"2px"}, "--sky-space-2": {"4px"}, "--sky-space-3": {"6px"},
	"--sky-space-4": {"8px"}, "--sky-space-5": {"10px"}, "--sky-space-6": {"12px"},
	"--sky-space-7": {"16px"}, "--sky-space-8": {"24px"},
	"--sky-radius-sm": {"4px"}, "--sky-radius-md": {"6px"}, "--sky-radius-lg": {"8px"},
	"--sky-radius-xl": {"12px"}, "--sky-radius-pill": {"999px", "9999px"},
	"--sky-fs-xs": {"11px"}, "--sky-fs-sm": {"12px"}, "--sky-fs-base": {"13px"},
	"--sky-fs-md": {"14px"}, "--sky-fs-lg": {"16px"}, "--sky-fs-xl": {"20px"},
	"--sky-fs-2xl": {"24px"},
}

// componentOrphanTokenRe 孤儿令牌名：--sky- 直接跟通用**色词**（缺 -c- 段）。
// ThemeVarsCSS 只输出 --sky-c-* 一族，这类名字永远拿不到主题供值。
//
// 注意它只匹配色词（primary/text/border/…），不匹配 space/radius/text-xs 这类
// **尺度档位** —— 后者本来就不由主题供值，靠兜底值生效，不是孤儿。
var componentOrphanTokenRe = regexp.MustCompile(
	`var\(--sky-(primary|secondary|accent|success|warning|danger|surface|text|heading|border|bg|muted)\b`)

// componentTokenRefRe **带兜底**的引用：`var(--sky-c-x, #fff)`，捕获兜底值。
// fallback 捕获允许一层嵌套括号（rgba(37,99,235,0.08) 这类值自带括号）。
var componentTokenRefRe = regexp.MustCompile(
	`var\(\s*(--sky-(?:c|space|radius|fs)-[a-z0-9-]+)\s*,\s*([^()]*(?:\([^()]*\)[^()]*)*)\)`)

// componentBareTokenRefRe **不带兜底**的引用：`var(--sky-c-surface)`。
//
// 这个正则的存在本身就是一次修 bug 的产物：原先只有上面那条（要求必须有逗号），
// 于是 `var(--sky-c-surface)` 一个逗号都没有 → 不匹配 → 完全不在扫描面 →
// 「缺兜底」这种最要紧的形态恰好看不见。274471ae 就是这么把 27 处兜底删掉、
// 门禁却一路 ok 的（无主题页面里按钮文字变黑，对比度 4.06:1 低于 AA）。
//
// 判据方向必须是：**先抓所有引用，再看有没有兜底** —— 而不是「只抓有兜底的，
// 然后校验兜底值」。后者在删掉兜底时会让分母一起缩，问题凭空消失。
var componentBareTokenRefRe = regexp.MustCompile(
	`var\(\s*(--sky-(?:c|space|radius|fs)-[a-z0-9-]+)\s*\)`)

// tokenNeedsFallback 哪些令牌族**必须**带兜底。
//
// 判据是「谁在无主题页面里没有值」：ThemeVarsCSS 只对挂了主题的页面输出 --sky-c-*
// 一族，所以它们无兜底就等于无值。尺度档（space/radius/fs）同理 —— 它们不在
// ThemeVarsCSS 的输出里，兜底值就是唯一真值。
//
// 反例（不在管辖内）：--sky-btn-* / --sky-density-* 由 ThemeVarsCSS 全量输出，
// 兜底只是防御性写法，缺失不会让页面变黑。
func tokenNeedsFallback(token string) bool {
	for _, prefix := range []string{"--sky-c-", "--sky-space-", "--sky-radius-", "--sky-fs-"} {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

func componentCSSFiles(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("components", "*", "*.css"))
	if err != nil {
		t.Fatal(err)
	}
	if len(files) < 30 {
		t.Fatalf("只枚举到 %d 个组件 CSS（预期 ≥30）：路径口径失效，本测试会在空转中通过", len(files))
	}
	return files
}

func TestComponentTokenFallbackConsistency(t *testing.T) {
	files := componentCSSFiles(t)
	totalRefs := 0 // 带兜底的引用数
	bareRefs := 0  // 不带兜底的引用数
	var problems []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		src := string(raw)

		// 判据 2：孤儿令牌名。
		for _, m := range componentOrphanTokenRe.FindAllString(src, -1) {
			problems = append(problems, f+": 孤儿令牌 "+m+"…（--sky- 缺 -c- 段，ThemeVarsCSS 不会供值；应为 --sky-c-* 全名）")
		}

		// 判据 1a：**缺兜底**的引用（最要紧的一档，原先完全漏掉）。
		//
		// 先跑这一条再跑有兜底那条：两者是互补的形态，一条引用要么带兜底要么不带。
		for _, m := range componentBareTokenRefRe.FindAllStringSubmatch(src, -1) {
			bareRefs++
			token := m[1]
			if !tokenNeedsFallback(token) {
				continue
			}
			problems = append(problems, f+": "+token+" 缺兜底值"+
				"（写成 var("+token+", <默认值>) —— 无主题页面里该令牌没有定义，声明会退化成继承/初始值）")
		}

		// 判据 1b：通用令牌登记与 fallback 一致。
		for _, m := range componentTokenRefRe.FindAllStringSubmatch(src, -1) {
			totalRefs++
			token, fallback := m[1], strings.TrimSpace(m[2])
			allowed, ok := componentTokenFallbacks[token]
			if !ok {
				problems = append(problems, f+": 未登记的通用令牌 "+token+"（fallback "+fallback+
					"）——先在 componentTokenFallbacks 登记表加条目并写清语义")
				continue
			}
			found := false
			for _, a := range allowed {
				if fallback == a {
					found = true
					break
				}
			}
			if !found {
				problems = append(problems, f+": "+token+" 的 fallback "+fallback+" 不在允许值集 "+strings.Join(allowed, " / ")+
					" —— 同一令牌全仓只许一种兜底（无主题页面里它就是真实生效值）")
			}
		}
	}
	// 判据 3：解析自检。
	//
	// 分母用**两类之和**：只数带兜底的那一类，会在兜底被批量删掉时让分母一起缩、
	// 自检跟着失效 —— 那正是这道门禁当初漏掉 27 处缺陷的同一个原因。
	if all := totalRefs + bareRefs; all < 50 {
		t.Fatalf("只扫到 %d 处令牌引用（带兜底 %d + 裸 %d，预期 ≥50）：正则或路径口径失效，测试在空转",
			all, totalRefs, bareRefs)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("组件令牌兜底不一致（%d 处）:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}

// TestBareTokenRefIsDetected 反向用例：门禁必须能发现「缺兜底的引用」。
//
// 这道测试守的是门禁自己。原实现只匹配 `var(--x, fallback)` 形态，于是
// `var(--sky-c-surface)` 零逗号 → 不进入扫描面 → 27 处兜底被删而门禁一路 ok，
// 最终让无主题页面的按钮文字变黑（对比度 4.06:1）。
//
// 判据：把一条**已知缺兜底**的写法喂给两条正则，bare 那条必须命中、带兜底那条必须不命中。
// 只断言「bare 正则能匹配 var(--x)」是不够的 —— 还得断言它**不会**误伤带兜底的形态，
// 否则一条 `var\(` 什么都匹配的正则也能让上面那条变绿。
func TestBareTokenRefIsDetected(t *testing.T) {
	bare := []string{
		"color: var(--sky-c-surface);",
		"background: var(--sky-c-primary);",
		"padding: var(--sky-space-4);",
		"border-radius: var(--sky-radius-md);",
		"font-size: var(--sky-fs-md);",
	}
	for _, src := range bare {
		matches := componentBareTokenRefRe.FindAllStringSubmatch(src, -1)
		if len(matches) != 1 {
			t.Errorf("缺兜底的写法没被 bare 正则检出：%q（命中 %d 次）—— 门禁对这处是瞎的", src, len(matches))
			continue
		}
		if !tokenNeedsFallback(matches[0][1]) {
			t.Errorf("%s 被判为「不需要兜底」，但它是无主题时没有值的令牌族", matches[0][1])
		}
		if got := componentTokenRefRe.FindAllStringSubmatch(src, -1); len(got) != 0 {
			t.Errorf("带兜底的正则不该匹配缺兜底的写法：%q 命中了 %v", src, got)
		}
	}

	// 反向：带兜底的写法不能被 bare 正则误判成「缺兜底」（否则修好也报错）。
	withFallback := []string{
		"color: var(--sky-c-surface, #fff);",
		"background: var(--sky-btn-bg, var(--sky-c-primary, #2563eb));",
		"padding: var(--sky-space-4, 8px);",
	}
	for _, src := range withFallback {
		if got := componentBareTokenRefRe.FindAllStringSubmatch(src, -1); len(got) != 0 {
			t.Errorf("带兜底的写法被误判成缺兜底：%q 命中了 %v", src, got)
		}
	}
}
