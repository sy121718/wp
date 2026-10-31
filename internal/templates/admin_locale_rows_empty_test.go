package templates

import (
	"strings"
	"testing"
)

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

func TestLocaleRowsEmptySliceRendersEmptyState(t *testing.T) {
	out := renderLocaleRows(t, []map[string]any{})
	if !strings.Contains(out, localeRowsEmptyText) {
		t.Fatalf("非 nil 空切片应渲染空态文案 %q，实际输出：\n%s", localeRowsEmptyText, out)
	}
	if strings.Contains(out, `class="locale-row"`) {
		t.Fatalf("空切片不应渲染任何 .locale-row，实际输出：\n%s", out)
	}
}

func TestLocaleRowsNilSliceRendersEmptyState(t *testing.T) {
	out := renderLocaleRows(t, []map[string]any(nil))
	if !strings.Contains(out, localeRowsEmptyText) {
		t.Fatalf("nil 切片应渲染空态文案 %q，实际输出：\n%s", localeRowsEmptyText, out)
	}
	if strings.Contains(out, `class="locale-row"`) {
		t.Fatalf("nil 切片不应渲染任何 .locale-row，实际输出：\n%s", out)
	}
}

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
