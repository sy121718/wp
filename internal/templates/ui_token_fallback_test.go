package templates

import (
	"regexp"
	"strings"
	"testing"
)

var cssFallbackFiles = []string{
	"ui.css", "theme.css", "media-lib.css", "workbench.css", "workbench-a11y.css", "rich-editor.css", "automation.css",
}

func stripThemeTokenDefinitions(src string) string {
	out := src
	for _, sel := range []string{`[data-theme="light"]`, `[data-theme="dark"]`, "[data-theme]"} {
		for {
			i := strings.Index(out, sel)
			if i < 0 {
				break
			}
			open := strings.Index(out[i:], "{")
			if open < 0 {
				break
			}
			open += i
			end := matchBrace(out, open) + 1
			out = out[:i] + out[end:]
		}
	}
	re := regexp.MustCompile(`(?m)^[ \t]*--[a-z0-9-]+\s*:[^;{}]*;[ \t]*$`)
	return re.ReplaceAllString(out, "")
}

var tokenFallbackExempt = map[string]string{
	"--sky-c-bg = #101318":       "workbench.css「10. 深色主题」段（选择器全部限定 [data-theme=\"dark\"]）—— 只在深色语境下生效，兜底取深色真值是有意的",
	"--sky-c-bg-soft = #171b21":  "同上：workbench.css 深色主题段（[data-theme=\"dark\"] .wb-media-field）",
	"--sky-c-bg-hover = #1f242b": "同上：workbench.css 深色主题段（[data-theme=\"dark\"] .wb-media-cell img）",
}

func normalizeColor(v string) string {
	v = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(v)), ""))
	if strings.HasPrefix(v, "#") && len(v) == 4 {
		var b strings.Builder
		b.WriteByte('#')
		for _, c := range v[1:] {
			b.WriteRune(c)
			b.WriteRune(c)
		}
		return b.String()
	}
	return v
}

func themeTokenFallbacks(t *testing.T, themeSrc string) (light, dark map[string]string) {
	t.Helper()
	lightBase := map[string]string{}
	darkBase := map[string]string{}
	tokenRe := regexp.MustCompile(`(?m)(--c-[a-z0-9-]+)\s*:\s*([^;]+);`)
	for sel, dst := range map[string]map[string]string{
		`[data-theme="light"]`: lightBase,
		`[data-theme="dark"]`:  darkBase,
	} {
		body, ok := cssRuleBlock(themeSrc, sel)
		if !ok {
			t.Fatalf("theme.css 里找不到 %s 块：两套真值表拿不到，本判据会空转通过", sel)
		}
		for _, m := range tokenRe.FindAllStringSubmatch(body, -1) {
			dst[m[1]] = normalizeColor(m[2])
		}
	}
	light, dark = map[string]string{}, map[string]string{}
	aliasBody, ok := cssRuleBlock(themeSrc, "[data-theme]")
	if !ok {
		t.Fatal("theme.css 里找不到 [data-theme] 别名层：拿不到 --sky-c-* → --c-* 的映射")
	}
	aliasRe := regexp.MustCompile(`(?m)(--sky-c-[a-z0-9-]+)\s*:\s*var\(\s*(--c-[a-z0-9-]+)\s*,\s*([^)]*)\)`)
	for _, m := range aliasRe.FindAllStringSubmatch(aliasBody, -1) {
		sky, base, fallback := m[1], m[2], normalizeColor(m[3])
		if v, ok := lightBase[base]; ok {
			light[sky] = v
		} else {
			light[sky] = fallback
		}
		if v, ok := darkBase[base]; ok {
			dark[sky] = v
		} else {
			dark[sky] = fallback
		}
	}
	if len(light) < 10 || len(dark) < 10 {
		t.Fatalf("解析到的 sky token 太少（浅 %d / 深 %d）：theme.css 的写法可能变了，判据会空转",
			len(light), len(dark))
	}
	return light, dark
}

func TestUIFallbacksAreLightThemeValues(t *testing.T) {
	themeSrc := readUIOwnershipFile(t, "static/css/theme.css")
	light, dark := themeTokenFallbacks(t, themeSrc)

	fbRe := regexp.MustCompile(`var\(\s*(--sky-c-[a-z0-9-]+)\s*,\s*([^)]*)\)`)
	seen := map[string]bool{}
	var hits []string
	var notes []string
	total := 0
	for _, file := range cssFallbackFiles {
		src := uiCssStripComments(readUIOwnershipFile(t, "static/css/"+file))
		if file == "theme.css" {
			src = stripThemeTokenDefinitions(src)
		}
		for _, m := range fbRe.FindAllStringSubmatch(src, -1) {
			token, fallback := m[1], normalizeColor(m[2])
			total++
			if fallback == "" {
				continue
			}
			if dv, ok := dark[token]; ok && fallback == dv {
				if lv, same := light[token]; same && lv == dv {
					continue
				}
				key := token + " = " + fallback
				if reason, exempt := tokenFallbackExempt[key]; exempt {
					seen[key] = true
					_ = reason
					continue
				}
				hits = append(hits, file+"  "+token+" 兜底 "+fallback+"（= 深色真值）")
				continue
			}
			if lv, ok := light[token]; ok && fallback == lv {
				continue
			}
			notes = append(notes, file+"  "+token+" 兜底 "+fallback+"（既非浅色也非深色真值）")
		}
	}
	if total < 300 {
		t.Fatalf("只扫到 %d 处 var(--sky-c-*, …)（预期 ≥300）：口径或扫描范围可能已失效", total)
	}
	if len(hits) > 0 {
		t.Errorf("有 %d 处兜底取了**深色主题真值**（这些 CSS 会进没有 theme.css 的前台产物，"+
			"那时兜底直接生效）：\n  %s\n请改成浅色真值；确实要取深色值的场景写进 tokenFallbackExempt 并注明理由",
			len(hits), strings.Join(hits, "\n  "))
	}
	if len(notes) > 0 {
		t.Logf("提示：%d 处兜底既非浅色也非深色真值（不判失败，供人工判断）：\n  %s",
			len(notes), strings.Join(notes, "\n  "))
	}
	for k, reason := range tokenFallbackExempt {
		if !seen[k] {
			t.Errorf("例外条目 %q 已不再命中（理由：%s）：请删掉它", k, reason)
		}
	}
}
