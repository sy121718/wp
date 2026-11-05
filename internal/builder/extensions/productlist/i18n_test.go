package productlist

import (
	"fmt"
	"strings"
	"testing"
)

func TestApplyI18nNilKeepsChineseFallback(t *testing.T) {
	v := &View{Page: 3}
	v.ApplyI18n(nil)
	if v.Labels.Filters != "筛选" || v.Labels.Price != "价格" || v.Labels.Prev != "上一页" {
		t.Fatalf("text 为 nil 时应落中文兜底，实际 %+v", v.Labels)
	}
	if v.PageText != "第 3 页" {
		t.Fatalf("分页文案应含实际页码，实际 %q", v.PageText)
	}
}

func TestApplyI18nFillsEveryField(t *testing.T) {
	seen := map[string]bool{}
	text := func(key, fallback string) string {
		seen[key] = true
		return "[" + fallback + "]"
	}
	v := &View{Page: 7}
	v.ApplyI18n(text)

	wantKeys := []string{
		TextKeyFilters, TextKeyRating, TextKeyPrice, TextKeyPriceMin, TextKeyPriceMinAria,
		TextKeyPriceMax, TextKeyPriceMaxAria, TextKeyPriceApply, TextKeySort, TextKeyPageSize,
		TextKeyView, TextKeyOnSale, TextKeyPager, TextKeyPrev, TextKeyNext, TextKeyPageCurrent,
	}
	for _, k := range wantKeys {
		if !seen[k] {
			t.Errorf("取词未覆盖 %s", k)
		}
	}

	if v.Labels.Sort != "[排序]" || v.Labels.OnSale != "[在售]" {
		t.Fatalf("字段应使用取词返回值，实际 %+v", v.Labels)
	}

	if v.PageText != fmt.Sprintf("[第 %s 页]", "7") {
		t.Fatalf("分页文案应按译文格式串渲染，实际 %q", v.PageText)
	}
}

func TestListLabelsCoverTemplate(t *testing.T) {
	tpl := productlistTemplate
	for _, zh := range []string{"筛选", "评分", "应用价格", "上一页", "下一页", "每页", "视图"} {
		if strings.Contains(tpl, ">"+zh) || strings.Contains(tpl, ">"+zh+"<") {
			t.Errorf("模板里仍有硬编码文案 %q（应改为 .V.Labels 取值）", zh)
		}
	}
	if !strings.Contains(tpl, ".V.Labels.") {
		t.Fatalf("模板应通过 .V.Labels 取文案")
	}
}
