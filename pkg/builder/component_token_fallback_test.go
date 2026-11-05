package builder

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var componentTokenFallbacks = map[string][]string{
	"--sky-c-primary":       {"#2563eb"},
	"--sky-c-primary-deep":  {"#1d4ed8"},
	"--sky-c-primary-weak":  {"rgba(37,99,235,0.08)"},
	"--sky-c-surface":       {"#fff"},
	"--sky-c-surface-muted": {"#f3f4f6"},
	"--sky-c-surface-alt":   {"rgba(0,0,0,0.03)", "rgba(0,0,0,0.05)"},
	"--sky-c-text":          {"#111827"},
	"--sky-c-text-muted":    {"#6b7280"},
	"--sky-c-text-mute":     {"#6b7280"},
	"--sky-c-muted":         {"#6b7280"},
	"--sky-c-heading":       {"#000"},
	"--sky-c-border":        {"#e5e7eb"},
	"--sky-c-border-strong": {"#d1d5db"},
	"--sky-c-bg-soft":       {"#f4f5f6"},
	"--sky-c-danger":        {"#dc2626"},
	"--sky-c-warning":       {"#f59e0b"},

	"--sky-space-1": {"2px"}, "--sky-space-2": {"4px"}, "--sky-space-3": {"6px"},
	"--sky-space-4": {"8px"}, "--sky-space-5": {"10px"}, "--sky-space-6": {"12px"},
	"--sky-space-7": {"16px"}, "--sky-space-8": {"24px"},
	"--sky-space-10":  {"36px"},
	"--sky-radius-sm": {"4px"}, "--sky-radius-md": {"6px"}, "--sky-radius-lg": {"8px"},
	"--sky-radius-xl": {"12px"}, "--sky-radius-pill": {"999px", "9999px"},
	"--sky-fs-xs": {"11px"}, "--sky-fs-sm": {"12px"}, "--sky-fs-base": {"13px"},
	"--sky-fs-md": {"14px"}, "--sky-fs-lg": {"16px"}, "--sky-fs-xl": {"20px"},
	"--sky-fs-2xl": {"24px"},
}

var componentOrphanTokenRe = regexp.MustCompile(
	`var\(--sky-(primary|secondary|accent|success|warning|danger|surface|text|heading|border|bg|muted)\b`)

var componentTokenRefRe = regexp.MustCompile(
	`var\(\s*(--sky-(?:c|space|radius|fs)-[a-z0-9-]+)\s*,\s*([^()]*(?:\([^()]*\)[^()]*)*)\)`)

var componentBareTokenRefRe = regexp.MustCompile(
	`var\(\s*(--sky-(?:c|space|radius|fs)-[a-z0-9-]+)\s*\)`)

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
	var files []string
	for _, root := range componentRoots {
		matches, err := filepath.Glob(filepath.Join(root, "*", "*.css"))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, matches...)
	}
	if len(files) < 30 {
		t.Fatalf("只枚举到 %d 个组件 CSS（预期 ≥30）：路径口径失效，本测试会在空转中通过", len(files))
	}
	return files
}

func TestComponentTokenFallbackConsistency(t *testing.T) {
	files := componentCSSFiles(t)
	totalRefs := 0
	bareRefs := 0
	var problems []string
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("读取 %s 失败: %v", f, err)
		}
		src := string(raw)

		for _, m := range componentOrphanTokenRe.FindAllString(src, -1) {
			problems = append(problems, f+": 孤儿令牌 "+m+"…（--sky- 缺 -c- 段，ThemeVarsCSS 不会供值；应为 --sky-c-* 全名）")
		}

		for _, m := range componentBareTokenRefRe.FindAllStringSubmatch(src, -1) {
			bareRefs++
			token := m[1]
			if !tokenNeedsFallback(token) {
				continue
			}
			problems = append(problems, f+": "+token+" 缺兜底值"+
				"（写成 var("+token+", <默认值>) —— 无主题页面里该令牌没有定义，声明会退化成继承/初始值）")
		}

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

	if all := totalRefs + bareRefs; all < 50 {
		t.Fatalf("只扫到 %d 处令牌引用（带兜底 %d + 裸 %d，预期 ≥50）：正则或路径口径失效，测试在空转",
			all, totalRefs, bareRefs)
	}
	if len(problems) > 0 {
		sort.Strings(problems)
		t.Errorf("组件令牌兜底不一致（%d 处）:\n  %s", len(problems), strings.Join(problems, "\n  "))
	}
}

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
