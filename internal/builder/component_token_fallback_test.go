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
}

// componentOrphanTokenRe 孤儿令牌名：--sky- 直接跟通用色词（缺 -c- 段）。
// ThemeVarsCSS 只输出 --sky-c-* 一族，这类名字永远拿不到主题供值。
var componentOrphanTokenRe = regexp.MustCompile(
	`var\(--sky-(primary|secondary|accent|success|warning|danger|surface|text|heading|border|bg|muted)\b`)

// fallback 捕获允许一层嵌套括号（rgba(37,99,235,0.08) 这类值自带括号）。
var componentTokenRefRe = regexp.MustCompile(`var\(\s*(--sky-c-[a-z0-9-]+)\s*,\s*([^()]*(?:\([^()]*\)[^()]*)*)\)`)

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
	totalRefs := 0
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

		// 判据 1：通用令牌登记与 fallback 一致。
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
	if totalRefs < 50 {
		t.Fatalf("只扫到 %d 处 --sky-c-* 引用（预期 ≥50）：正则或路径口径失效，测试在空转", totalRefs)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("组件令牌兜底不一致（%d 处）:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}
