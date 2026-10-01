package templates

// ui_token_fallback_test.go —— ui.css 的 token 兜底值必须是**浅色主题真值**。
//
// 为什么需要它：ui.css 会被注入到**没有 theme.css** 的前台产物里（段切分后内联进 HTML），
// 那时 `var(--sky-c-X, Y)` 的兜底 Y 直接生效。若 Y 抄的是**深色主题**的真值，产物侧就会
// 出现「浅底浅字」「浅底深边」这类偏色 —— 而这类错误在后台（有 theme.css）完全看不见，
// 只有打开产物页面才暴露。历史上已经修过两批（5 处 --sky-c-text-secondary、primary 系列），
// 每次都是逐个找；这个文件把它变成判据：**新写 `var(--sky-c-X, <深色值>)` 直接红**。
//
// 判据的边界（刻意保守，避免误伤）：
//   · 兜底等于该 token 的**深色真值** → 失败（这就是缺陷本身）；
//   · 等于**浅色真值**、或两侧真值相同（light == dark）→ 通过；
//   · 其它值（既不是浅色也不是深色真值，例如 rgba(...) 或某处自定的颜色）→ 只打印提示，
//     不判失败：它们不是「抄了深色主题」，是否该收敛属于别的判据。
//
// 真值来源是 theme.css（运行时读取），所以 theme.css 改 token 时这里自动跟着走。

import (
	"regexp"
	"strings"
	"testing"
)

// 例外表：兜底**故意**取深色真值的场景。默认没有例外。
// 条目不再命中即失败（同项目其它豁免约定），防止清单变成永久豁免区。
var tokenFallbackExempt = map[string]string{}

// normalizeColor 把颜色值归一化到可比较的形式：小写、去掉空白、#abc → #aabbcc。
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

// themeTokenFallbacks 从 theme.css 解析出每个 --sky-c-* 的**浅色真值**与**深色真值**。
//
// 解析链：`[data-theme="light"] / [data-theme="dark"]` 给出 `--c-X: 字面值`；
// `[data-theme]` 别名层给出 `--sky-c-X: var(--c-X, Y)`。于是
//
//	浅色真值 = lightBase[--c-X]（缺则取别名兜底 Y），深色真值同理取 darkBase。
//
// 解析不出的条目跳过（例如值里带计算式、或别名指向另一个别名）：本判据只处理能静态
// 定值的 token —— 覆盖不到的 token 不会因此静默通过，因为「既是浅色又不是深色」会走提示分支。
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

// TestUIFallbacksAreLightThemeValues ui.css 的兜底值不得取深色主题真值。
//
// 失败信息会指名 token、兜底值与它撞上的深色真值，方便直接改。
func TestUIFallbacksAreLightThemeValues(t *testing.T) {
	themeSrc := readUIOwnershipFile(t, "static/css/theme.css")
	light, dark := themeTokenFallbacks(t, themeSrc)
	uiSrc := uiCssStripComments(readUIOwnershipFile(t, "static/css/ui.css"))

	fbRe := regexp.MustCompile(`var\(\s*(--sky-c-[a-z0-9-]+)\s*,\s*([^)]*)\)`)
	seen := map[string]bool{}
	var hits []string
	var notes []string
	total := 0
	for _, m := range fbRe.FindAllStringSubmatch(uiSrc, -1) {
		token, fallback := m[1], normalizeColor(m[2])
		total++
		if fallback == "" {
			continue // var(--sky-c-X,) 之类：交给别的判据
		}
		if dv, ok := dark[token]; ok && fallback == dv {
			if lv, same := light[token]; same && lv == dv {
				continue // 两侧真值相同：取谁都一样
			}
			key := token + " = " + fallback
			if reason, exempt := tokenFallbackExempt[key]; exempt {
				seen[key] = true
				_ = reason
				continue
			}
			hits = append(hits, token+" 兜底 "+fallback+"（= 深色真值）")
			continue
		}
		if lv, ok := light[token]; ok && fallback == lv {
			continue
		}
		notes = append(notes, token+" 兜底 "+fallback+"（既非浅色也非深色真值）")
	}
	if total < 100 {
		t.Fatalf("只扫到 %d 处 var(--sky-c-*, …)（预期 ≥100）：口径可能已失效", total)
	}
	if len(hits) > 0 {
		t.Errorf("ui.css 有 %d 处兜底取了**深色主题真值**（ui.css 会进没有 theme.css 的前台产物，"+
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
