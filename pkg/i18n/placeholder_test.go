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

// FillNamedPlaceholders 的判据：填不干净必须 false —— 缺参数时**不补默认值**，
// 补 0 会渲染出「已删除 0 个」这种看起来像成功结论的错话。
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

// FillTranslate 的判据：词条被改坏（残留别的具名占位符）时回落**兜底原文**再填一次 ——
// 页面上宁可显示代码里写死的中文，也不能把 `{other}` 这样的字面量摆给运营看。
func TestFillTranslateFallsBackOnBrokenEntry(t *testing.T) {
	broken := func(string, string) string { return "被改坏的词条 {other}" }
	if got := FillTranslate(broken, "k", "已删除 {n} 个页面。", map[string]string{"n": "3"}); got != "已删除 3 个页面。" {
		t.Errorf("词条被改坏时应回落兜底再填，got %q", got)
	}

	// 词条缺席（tr 返回兜底）时同样要填上参数。
	absent := func(_ string, fallback string) string { return fallback }
	if got := FillTranslate(absent, "k", "已删除 {n} 个页面。", map[string]string{"n": "3"}); got != "已删除 3 个页面。" {
		t.Errorf("词条缺席时应填兜底原文，got %q", got)
	}
}

// FillTranslate 的**存在理由**：语序归词条，运营改了不用动调用方代码。
//
// 位置参数（fmt.Sprintf 那一档）做不到这一点：词条把两个占位符换个位置，调用方按
// 老顺序传参就会渲染错，而调用方并没有改过任何东西。这条是两种参数形态的分水岭。
func TestFillTranslateKeepsCallerFreeOfEntryWordOrder(t *testing.T) {
	// 运营把「{a} 到 {b}」改成了「{b} 到 {a}」（换个语序在别的语言里很常见）。
	reordered := func(string, string) string { return "{b} 到 {a}" }
	params := map[string]string{"a": "1", "b": "9"}
	if got := FillTranslate(reordered, "k", "1 到 9", params); got != "9 到 1" {
		t.Errorf("FillTranslate = %q，want %q —— 具名参数按词条语序填，与调用方给的顺序无关", got, "9 到 1")
	}
}
