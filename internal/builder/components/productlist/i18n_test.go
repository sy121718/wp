package productlist

// i18n_test.go — 列表固定文案的多语言取词（审计 I18N-010）。
//
// 两条不能破的约束：
//  1. 未接入 i18n（text == nil）时产物与接入前**逐字一致** —— 抽 key 不该顺带改文案；
//  2. 接入后每个字段都真的走取词（漏一个的表现是「切了英文，只有这一处还是中文」，
//     页面上看起来像没翻译完，实际是漏接）。

import (
	"fmt"
	"strings"
	"testing"
)

// TestApplyI18nNilKeepsChineseFallback 未接入 i18n 时落中文兜底。
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

// TestApplyI18nFillsEveryField 每个字段都走取词（漏一个就是漏翻译一处）。
func TestApplyI18nFillsEveryField(t *testing.T) {
	seen := map[string]bool{}
	text := func(key, fallback string) string {
		seen[key] = true
		return "[" + fallback + "]" // 用包裹形式，便于确认每个字段真的被替换
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
	// 兜底值已被包裹 → 说明字段用的是取词返回值而不是包内常量。
	if v.Labels.Sort != "[排序]" || v.Labels.OnSale != "[在售]" {
		t.Fatalf("字段应使用取词返回值，实际 %+v", v.Labels)
	}
	// 带计数的文案：译文提供的格式串决定最终形状（不拼词序）。
	if v.PageText != fmt.Sprintf("[第 %s 页]", "7") {
		t.Fatalf("分页文案应按译文格式串渲染，实际 %q", v.PageText)
	}
}

// TestListLabelsCoverTemplate 模板不再直写中文兜底串。
//
// 抽 key 的漏网之鱼最容易出现在「模板里还剩一句中文」：字段填对了，模板没改。
// 这里直接查模板源码 —— 组件测试能看到模板（同包 embed），比人眼可靠。
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
