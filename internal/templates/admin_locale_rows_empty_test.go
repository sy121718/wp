package templates

import (
	"strings"
	"testing"
)

// admin_locale_rows_empty_test.go — locale_rows 片段空态的回归（2026-09 修复的钉子）。
//
// 缺陷：旧写法直接对 .Locales 求真值判断分支，Jet 会把「非 nil 空切片」当真 ——
// 片段端点（LocaleRowsFragment）在用户把语言行删光后回渲染的是 len 0 非 nil 切片，
// 旧写法走进 range 渲染 0 行，空态分支永远不可达；nil 切片（localeRowsOf 读失败 /
// parseLocaleRows 空表单）倒是会走 else，所以缺陷只在「删光行」这一条路径上暴露。
//
// 修复：isset(.Locales) + len(.Locales) > 0 双判。本文件钉三条路径：
//  ① 非 nil 空切片 → 空态文案出现、0 个 .locale-row（核心回归，旧写法必红）；
//  ② nil 切片 → 空态（行为保持：修复前后都走空态）；
//  ③ 有行 → 行渲染、无空态（行为保持）。
//
// 渲染基建与 i18n_jet_test.go 同源（memSet + adminTemplateSource 读真实模板源）；
// t 取兜底文案即可 —— 空态断言盯的是兜底字面量「还没有语言清单」，与词条库无关。

func renderLocaleRows(t *testing.T, locales any) string {
	t.Helper()
	set := memSet(t, map[string]string{
		"admin/partials/locale_rows.html": adminTemplateSource(t, "locale_rows.html"),
	})
	out, err := render(t, set, "admin/partials/locale_rows.html", map[string]any{
		"t":       func(key, fallback string) string { return fallback },
		"Locales": locales,
	})
	if err != nil {
		t.Fatalf("locale_rows 片段渲染失败: %v", err)
	}
	return out
}

const localeRowsEmptyText = "还没有语言清单"

// ① 核心：非 nil 空切片必须出空态 —— 旧写法 {{if .Locales}} 把它当真，渲染 0 行。
func TestLocaleRowsEmptySliceRendersEmptyState(t *testing.T) {
	out := renderLocaleRows(t, []map[string]any{})
	if !strings.Contains(out, localeRowsEmptyText) {
		t.Fatalf("非 nil 空切片应渲染空态文案 %q，实际输出：\n%s", localeRowsEmptyText, out)
	}
	if strings.Contains(out, `class="locale-row"`) {
		t.Fatalf("空切片不应渲染任何 .locale-row，实际输出：\n%s", out)
	}
}

// ② 行为保持：nil 切片（键存在、值为 nil）修复前后都走空态。
func TestLocaleRowsNilSliceRendersEmptyState(t *testing.T) {
	out := renderLocaleRows(t, []map[string]any(nil))
	if !strings.Contains(out, localeRowsEmptyText) {
		t.Fatalf("nil 切片应渲染空态文案 %q，实际输出：\n%s", localeRowsEmptyText, out)
	}
	if strings.Contains(out, `class="locale-row"`) {
		t.Fatalf("nil 切片不应渲染任何 .locale-row，实际输出：\n%s", out)
	}
}

// ③ 行为保持：有行时渲染行、不出空态。
func TestLocaleRowsRowsRenderWithoutEmptyState(t *testing.T) {
	out := renderLocaleRows(t, []map[string]any{
		{"Lang": "zh-CN", "IsDefault": true, "Enabled": true},
		{"Lang": "en-US", "IsDefault": false, "Enabled": true},
	})
	if got := strings.Count(out, `class="locale-row"`); got != 2 {
		t.Fatalf("两行数据应渲染 2 个 .locale-row，实际 %d 个，输出：\n%s", got, out)
	}
	if strings.Contains(out, localeRowsEmptyText) {
		t.Fatalf("有行时不应出现空态文案，实际输出：\n%s", out)
	}
	if !strings.Contains(out, `value="zh-CN"`) || !strings.Contains(out, `value="en-US"`) {
		t.Fatalf("行内语言码应回显，实际输出：\n%s", out)
	}
}
