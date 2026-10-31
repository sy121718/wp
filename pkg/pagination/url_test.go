package pagination

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
)

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
