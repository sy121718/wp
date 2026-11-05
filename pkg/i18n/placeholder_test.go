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

// StringPlaceholderCount 是「这句话能不能被纯字符串参数填满」的唯一判定。
// 它同时回答个数与可行性：调用方拿 count 核对参数个数，拿 ok 判能不能交给 Sprintf。
func TestStringPlaceholderCount(t *testing.T) {
	cases := []struct {
		text  string
		count int
		ok    bool
		why   string
	}{
		{"", 0, true, "空句没有可填位置"},
		{"纯文本", 0, true, "没有占位符"},
		{"%s", 1, true, "裸 %s 是唯一合法形态"},
		{"%s 与 %s", 2, true, "两个裸 %s"},
		{"100%% 完成", 0, true, "%% 是字面百分号，不算占位符"},
		{"占比 %s%%", 1, true, "%% 与 %s 可以共存"},
		{"%s%d", 0, false, "%d 不接受字符串参数"},
		{"%d", 0, false, "非 %s 动词"},
		{"%.2f", 0, false, "非 %s 动词"},
		{"%+v", 0, false, "非 %s 动词"},
		{"%#v", 0, false, "非 %s 动词"},
		{"%5d", 0, false, "非 %s 动词"},
		{"%[1]s 和 %[2]s", 0, false, "显式索引会在参数不足时吐标记，一律拒绝"},
		{"%05s", 0, false, "宽度会改写输出与参数消费规则"},
		{"%.2s", 0, false, "精度同上"},
		{"%*s", 0, false, "星号宽度同上"},
		{"% s", 0, false, "空格标志不接受"},
		{"不是%占位", 0, false, "% 后不是动词"},
		{"尾随 %", 0, false, "% 在末尾没有动词"},
	}

	for _, tc := range cases {
		count, ok := StringPlaceholderCount(tc.text)
		if count != tc.count || ok != tc.ok {
			t.Errorf("StringPlaceholderCount(%q) = (%d, %v), want (%d, %v) —— %s",
				tc.text, count, ok, tc.count, tc.ok, tc.why)
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
