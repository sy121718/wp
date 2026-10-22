package pagination

// url_test.go — 取址口径。这些判据从 internal/templates/funcs_pagination_test.go 搬来并加强：
// 原先测的是模板全局函数，现在测的是 pkg 纯函数，语义必须逐条不变。

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

// TestQueryURLEscapesValuesLikeURLValues 值的编码必须与 url.Values.Encode 同口径
// （空格 → `+`、`&`/`=`/`#`/`%` 逐字符转义、非 ASCII 走 UTF-8 百分号编码）。
// 用 url.Values 现算期望值而不是手写字符串：口径漂移时这条会红，而手写期望只会一起漂。
func TestQueryURLEscapesValuesLikeURLValues(t *testing.T) {
	for _, v := range []any{"&", "#", "%", "=", "+", "?", "a b", "汉 字", "~/!*'()", "", "page=1&x=2"} {
		got := QueryURL("/admin/x", "q", v)
		want := "/admin/x?" + url.Values{"q": {DisplayString(v)}}.Encode()
		if got != want {
			t.Fatalf("值 %q：QueryURL = %q，url.Values 口径 = %q", v, got, want)
		}
		if strings.Contains(got, "&amp;") {
			t.Fatalf("值 %q：Go 侧不得预转义（Jet 渲染时才转义一次），实际 %q", v, got)
		}
	}
}

// TestQueryURLSeparatorOrderAndDuplicates 分隔符、顺序、重复键、以及「孤键忽略」的既有语义。
func TestQueryURLSeparatorOrderAndDuplicates(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"空 base 回空串", QueryURL("", "page", 1), ""},
		{"全空白 base 回空串", QueryURL("   ", "page", 1), ""},
		{"无查询串用 ?", QueryURL("/admin/x", "page", 2), "/admin/x?page=2"},
		{"已有查询串用 &", QueryURL("/admin/x?status=1", "page", 2), "/admin/x?status=1&page=2"},
		{"现行链接形态逐字不变", QueryURL("/admin/x", "page", 2, "limit", 20), "/admin/x?page=2&limit=20"},
		{"顺序按调用顺序、不排序", QueryURL("/admin/x", "b", 1, "a", 2), "/admin/x?b=1&a=2"},
		{"同名键不去重", QueryURL("/admin/x", "b", 1, "b", 2), "/admin/x?b=1&b=2"},
		{"base 已含同名参数时原样追加", QueryURL("/admin/x?page=1", "page", 2), "/admin/x?page=1&page=2"},
		{"孤键忽略", QueryURL("/admin/x", "page"), "/admin/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("得到 %q，期望 %q", tc.got, tc.want)
			}
		})
	}
}

// TestQueryURLPanicsOnNonStringKey 键写错必须当场爆掉：静默丢一个筛选条件的链接，
// 表现是「用户点下一页后筛选没了」，比 500 难发现得多。
func TestQueryURLPanicsOnNonStringKey(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("非字符串键必须 panic")
		}
		if !strings.Contains(fmt.Sprint(r), "QueryURL") {
			t.Fatalf("panic 里应指明函数名，实际: %v", r)
		}
	}()
	_ = QueryURL("/admin/x", 1, "v")
}

// TestDisplayStringTypedNilIsEmpty typed nil（可空字段）必须渲染成空串，不是 "<nil>"。
func TestDisplayStringTypedNilIsEmpty(t *testing.T) {
	var p *int64
	var s []string
	for _, v := range []any{nil, p, s} {
		if got := DisplayString(v); got != "" {
			t.Fatalf("%T 应回空串，实际 %q", v, got)
		}
	}
	if got := DisplayString(42); got != "42" {
		t.Fatalf("整数应渲染成 42，实际 %q", got)
	}
}

// TestPageBaseDropsPagingParams 翻页基址：保留筛选、丢掉分页自己会写回的参数。
//
// 判据是「不能出现两个同名参数」——那是「点了翻页没反应」的根因。
func TestPageBaseDropsPagingParams(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		drop []string
		want string
	}{
		{"空串回空", "", []string{"page"}, ""},
		{"全空白回空", "  ", []string{"page"}, ""},
		{"无查询串只回路径", "/admin/x", []string{"page"}, "/admin/x"},
		{"丢掉 page 与 limit", "/admin/x?page=2&limit=20", []string{"page", "limit"}, "/admin/x"},
		{"保留筛选丢分页", "/admin/x?keyword=a&page=2&limit=20", []string{"page", "limit"}, "/admin/x?keyword=a"},
		{"自定义页码参数名", "/admin/mail?account_page=3&type=in", []string{"account_page", "limit"}, "/admin/mail?type=in"},
		{"多删不存在的键无副作用", "/admin/x?a=1", []string{"page", "limit", "nope"}, "/admin/x?a=1"},
		{"值按 Encode 字典序输出", "/admin/x?b=2&a=1&page=9", []string{"page"}, "/admin/x?a=1&b=2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PageBase(tc.raw, tc.drop...); got != tc.want {
				t.Fatalf("得到 %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestPageBaseThenQueryURLNeverDuplicatesPage 组合判据（真正要防的那个缺陷）：
// 请求串里已有 page=2，经 PageBase 丢掉后再 QueryURL 追加 page=3，最终只应有一个 page。
func TestPageBaseThenQueryURLNeverDuplicatesPage(t *testing.T) {
	raw := "/admin/product-brands?keyword=a&page=2&limit=20"
	base := PageBase(raw, "page", "limit")
	got := QueryURL(base, "page", 3, "limit", 20)
	if strings.Count(got, "page=3") != 1 || strings.Contains(got, "page=2") {
		t.Fatalf("翻页链接里出现多个 page 参数（点了没反应的根因）：%q", got)
	}
	if !strings.Contains(got, "keyword=a") {
		t.Fatalf("筛选条件丢了：%q", got)
	}
}
