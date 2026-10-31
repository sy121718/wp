package safepath

import (
	"strings"
	"testing"
)

func TestIsSameOriginPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool

		rule string
	}{

		{"根路径", "/", true, ""},
		{"普通后台路径", "/admin/pages", true, ""},
		{"带筛选 query（回跳地址的常态）", "/admin/coupons?status=1&page=2", true, ""},
		{"带片段", "/admin/pages#form", true, ""},

		{"空串", "", false, "prefix"},
		{"无前导斜杠（会被按当前路径拼接）", "admin/roles", false, "prefix"},
		{"前导空格", " /admin", false, "prefix"},
		{"绝对 URL", "https://evil.example.com/x", false, "prefix"},
		{"缺一个斜杠的绝对 URL", "https:/evil.example.com", false, "prefix"},
		{"反斜杠开头的 UNC 形态", `\\evil.example.com`, false, "prefix"},

		{"协议相对跳转（开放重定向最常见的一手）", "//evil.example.com/x", false, "slash2"},
		{"重复分隔符", "/a//b", false, "slash2"},
		{"query 里带双斜杠也会被拒（判据是子串判定，属刻意从严——见下条正向用例）", "/admin?next=//evil.com", false, "slash2"},

		{"反斜杠（部分客户端会归一成双斜杠）", `/\\evil.example.com`, false, "backslash"},
		{"路径内的反斜杠", `/admin\pages`, false, "backslash"},

		{"换行（拆 Location 头）", "/a\nb", false, "control"},
		{"回车+换行", "/a\r\nLocation: https://evil.example.com", false, "control"},
		{"制表符", "/a\tb", false, "control"},
		{"NUL", "/a\x00b", false, "control"},
		{"退格", "/a\bb", false, "control"},
		{"DEL", "/a\x7fb", false, "control"},

		{"点段不在判据内（浏览器自己会归一，落点仍是站内）", "/a/../b", true, ""},
		{"百分号编码不在判据内（访问面按已编码字符处理）", "/a%2e%2e/b", true, ""},
		{"编码的制表符不在判据内（未解码时只是普通路径字符）", "/%09/evil.example.com", true, ""},
		{"编码的双斜杠不在判据内", "/a%2f%2fb", true, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsSameOriginPath(tc.in); got != tc.want {
				t.Errorf("IsSameOriginPath(%q) = %v，应为 %v", tc.in, got, tc.want)
			}
		})
	}

	rules := map[string]bool{}
	for _, tc := range cases {
		if tc.rule != "" {
			if tc.want {
				t.Fatalf("表写错了：标了判据 %s 的用例却是正例（%q）", tc.rule, tc.in)
			}
			rules[tc.rule] = true
		}
	}
	for _, want := range []string{"prefix", "slash2", "backslash", "control"} {
		if !rules[want] {
			t.Errorf("判据 %s 在表里没有隔离反例：删掉它这条测试也不会红，等于空转", want)
		}
	}
}

func TestHasControlRune(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"普通文本", "/admin/pages", false},
		{"中文与数字", "优惠码-2026", false},
		{"空串", "", false},
		{"换行", "a\nb", true},
		{"回车", "a\rb", true},
		{"制表符", "a\tb", true},
		{"NUL", "a\x00b", true},
		{"Unit Separator（0x1f 仍属 C0）", "a\x1fb", true},
		{"DEL（0x7f 单列，不属于 C0 但同样要挡）", "a\x7fb", true},
		{"空格不在判据内（值里的空格由 URL 编码承担）", "a b", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasControlRune(tc.in); got != tc.want {
				t.Errorf("HasControlRune(%q) = %v，应为 %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestMaxReturnPathBytesIsNotBakedIntoJudgement(t *testing.T) {
	if MaxReturnPathBytes != 512 {
		t.Fatalf("上限常量被改成 %d：这等于同时改掉所有调用方的口径，必须是有意为之并同步各处注释", MaxReturnPathBytes)
	}

	atLimit := "/" + strings.Repeat("a", MaxReturnPathBytes-1)
	overLimit := atLimit + "a"

	for _, tc := range []struct {
		name string
		in   string
	}{
		{"恰好 512 字节", atLimit},
		{"513 字节", overLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if len(tc.in) <= MaxReturnPathBytes && !IsSameOriginPath(tc.in) {
				t.Errorf("长度范围内的站内路径被判非法：%q", tc.in)
			}

			if got := IsSameOriginPath(tc.in); !got {
				t.Errorf("IsSameOriginPath(%d 字节) = false：长度上限混进了判定函数（调用方的长度口径会被悄悄改掉）", len(tc.in))
			}
		})
	}

	if len(atLimit) != MaxReturnPathBytes || len(overLimit) != MaxReturnPathBytes+1 {
		t.Fatalf("用例构造有误：%d / %d", len(atLimit), len(overLimit))
	}
}

func TestLangRedirectPathIsTheOnlyCriterion(t *testing.T) {
	long := "/admin/products?project=" + strings.Repeat("x", 300)
	over := "/admin/products?project=" + strings.Repeat("x", 520)
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"首页", "/", "/"},
		{"路径带 query", "/admin/roles?page=2&keyword=x", "/admin/roles?page=2&keyword=x"},
		{"512 字节以内原样", long, long},
		{"超过 512 字节拒绝", over, ""},
		{"超长路径拒绝", "/admin/" + strings.Repeat("y", 600), ""},
		{"空串拒绝", "", ""},
		{"不以 / 开头拒绝", "admin/system/roles", ""},
		{"协议相对跳转拒绝", "//evil.example.com/x", ""},
		{"绝对 URL 拒绝", "https://evil.example.com/x", ""},
		{"反斜杠拒绝", "/admin/roles\\x", ""},
		{"换行拒绝", "/admin/roles\nSet-Cookie: a=b", ""},
		{"NUL 拒绝", "/admin/roles\x00", ""},
		{"ESC 拒绝", "/admin/roles\x1b[31m", ""},
		{"DEL 拒绝", "/admin/roles\x7f", ""},
	}
	for _, tc := range cases {
		if got := LangRedirectPath(tc.raw); got != tc.want {
			t.Errorf("%s：LangRedirectPath(%q)=%q，want %q", tc.name, tc.raw, got, tc.want)
		}
	}
}

func TestLocalReturnPathSharesTheCriterion(t *testing.T) {
	for _, raw := range []string{
		"/admin/coupons?status=1",
		"/",
		"//evil.example.com/x",
		"https://evil.example.com",
		"admin/coupons",
		"/admin/x\\..\\evil",
		"/admin/x\nLocation: /y",
		"/" + strings.Repeat("a", MaxReturnPathBytes+1),
	} {
		if got, want := LocalReturnPath(raw), LangRedirectPath(raw); got != want {
			t.Errorf("LocalReturnPath(%q)=%q 与判据 %q 不一致（第二份白名单又长出来了）", raw, got, want)
		}
	}
	if got := LocalReturnPath("/admin/coupons?status=1"); got != "/admin/coupons?status=1" {
		t.Errorf("合法站内路径应原样返回，got %q", got)
	}
}
