package i18n

import "testing"

func TestCountPlaceholders(t *testing.T) {
	cases := []struct {
		text string
		want int
	}{
		{"", 0},
		{"无占位符", 0},
		{"%s %d", 2},
		{"%[1]s 和 %[2]s", 2},
		{"100%% 完成", 0},
		{"%.2f", 1},
		{"%+v", 1},
		{"%#v", 1},
		{"%5d", 1},
		{"%s%t%x", 3},
		{"不是%占位", 0},
	}

	for _, tc := range cases {
		if got := CountPlaceholders(tc.text); got != tc.want {
			t.Fatalf("CountPlaceholders(%q) = %d, want %d", tc.text, got, tc.want)
		}
	}
}

func TestHasStringPlaceholdersOnly(t *testing.T) {
	cases := []struct {
		text string
		want bool
	}{
		{"", true},
		{"纯文本", true},
		{"%s", true},
		{"%[1]s %[2]s", true},
		{"100%% 完成", true},
		{"%d", false},
		{"%.2f", false},
		{"%s%d", false},
	}

	for _, tc := range cases {
		if got := HasStringPlaceholdersOnly(tc.text); got != tc.want {
			t.Fatalf("HasStringPlaceholdersOnly(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestFillNamedPlaceholders(t *testing.T) {
	cases := []struct {
		name   string
		tpl    string
		params map[string]string
		want   string
		wantOK bool
	}{
		{"全部填上", "已删除 {n} 个页面", map[string]string{"n": "3"}, "已删除 3 个页面", true},
		{"无占位符", "纯文本", map[string]string{"n": "3"}, "纯文本", true},
		{"多个占位符", "{a} 到 {b}", map[string]string{"a": "1", "b": "9"}, "1 到 9", true},
		{"缺参数判 false", "已删除 {n} 个", map[string]string{"other": "3"}, "已删除 {n} 个", false},
		{"只剩一个未填", "{a} 与 {b}", map[string]string{"a": "1"}, "1 与 {b}", false},
		{"空模板", "", nil, "", true},
	}

	for _, tc := range cases {
		got, ok := FillNamedPlaceholders(tc.tpl, tc.params)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("%s: FillNamedPlaceholders(%q) = (%q, %v), want (%q, %v)",
				tc.name, tc.tpl, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestFillTranslateFallsBackOnBrokenEntry(t *testing.T) {
	broken := func(string, string) string { return "被改坏的词条 {other}" }
	if got := FillTranslate(broken, "k", "已删除 {n} 个页面。", map[string]string{"n": "3"}); got != "已删除 3 个页面。" {
		t.Errorf("词条被改坏时应回落兜底再填，got %q", got)
	}

	absent := func(_ string, fallback string) string { return fallback }
	if got := FillTranslate(absent, "k", "已删除 {n} 个页面。", map[string]string{"n": "3"}); got != "已删除 3 个页面。" {
		t.Errorf("词条缺席时应填兜底原文，got %q", got)
	}
}

func TestFillTranslateKeepsCallerFreeOfEntryWordOrder(t *testing.T) {

	reordered := func(string, string) string { return "{b} 到 {a}" }
	params := map[string]string{"a": "1", "b": "9"}
	if got := FillTranslate(reordered, "k", "1 到 9", params); got != "9 到 1" {
		t.Errorf("FillTranslate = %q，want %q —— 具名参数按词条语序填，与调用方给的顺序无关", got, "9 到 1")
	}
}
