package safepath

import (
	"strings"
	"testing"
)

// TestIsSameOriginPath 四条判据的正反例。
//
// 每条反例都标出它守着哪条判据（rule 字段），并由表尾的自检保证「四条判据各自至少有一个
// 隔离用例」—— 否则删掉某条判据时这条测试仍会全绿，等于空转（迁移类重构最怕这个：
// 判据被搬走一条，测试也跟着少一条，没人会发现）。
func TestIsSameOriginPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
		// rule 非空表示这是「只由该条判据触发」的反例：prefix / slash2 / backslash / control。
		rule string
	}{
		// —— 正向：站内相对路径（含带 query 的后台路径）——
		{"根路径", "/", true, ""},
		{"普通后台路径", "/admin/pages", true, ""},
		{"带筛选 query（回跳地址的常态）", "/admin/coupons?status=1&page=2", true, ""},
		{"带片段", "/admin/pages#form", true, ""},

		// —— 判据 1：必须以 "/" 开头 ——
		{"空串", "", false, "prefix"},
		{"无前导斜杠（会被按当前路径拼接）", "admin/roles", false, "prefix"},
		{"前导空格", " /admin", false, "prefix"},
		{"绝对 URL", "https://evil.example.com/x", false, "prefix"},
		{"缺一个斜杠的绝对 URL", "https:/evil.example.com", false, "prefix"},
		{"反斜杠开头的 UNC 形态", `\\evil.example.com`, false, "prefix"},

		// —— 判据 2：不含 "//" ——
		{"协议相对跳转（开放重定向最常见的一手）", "//evil.example.com/x", false, "slash2"},
		{"重复分隔符", "/a//b", false, "slash2"},
		{"query 里带双斜杠也会被拒（判据是子串判定，属刻意从严——见下条正向用例）", "/admin?next=//evil.com", false, "slash2"},

		// —— 判据 3：不含反斜杠 ——
		{"反斜杠（部分客户端会归一成双斜杠）", `/\\evil.example.com`, false, "backslash"},
		{"路径内的反斜杠", `/admin\pages`, false, "backslash"},

		// —— 判据 4：不含 C0 控制字符 / DEL ——
		{"换行（拆 Location 头）", "/a\nb", false, "control"},
		{"回车+换行", "/a\r\nLocation: https://evil.example.com", false, "control"},
		{"制表符", "/a\tb", false, "control"},
		{"NUL", "/a\x00b", false, "control"},
		{"退格", "/a\bb", false, "control"},
		{"DEL", "/a\x7fb", false, "control"},

		// —— 非目标：今天就是通过的，改判据要先改这条用例（而不是让它静默消失）——
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

	// 非空转自检：四条判据必须各自至少有一个隔离反例。
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

// TestHasControlRune 值级清洗用的同一份判定（shell.BackPath 对每个 query 值也用它）。
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

// TestMaxReturnPathBytesIsNotBakedIntoJudgement 长度上限是常量，**不进**判定函数。
//
// 这条守的是迁移里最容易悄悄走形的一处：把 512 并进 IsSameOriginPath 会让
// 「今天不设上限的调用方」（shell.RenderJump 只调 IsSameOriginPath）静默变严 ——
// 那是行为变更，不是重构。上限归调用方按自己的语义去比。
func TestMaxReturnPathBytesIsNotBakedIntoJudgement(t *testing.T) {
	if MaxReturnPathBytes != 512 {
		t.Fatalf("上限常量被改成 %d：这等于同时改掉所有调用方的口径，必须是有意为之并同步各处注释", MaxReturnPathBytes)
	}

	atLimit := "/" + strings.Repeat("a", MaxReturnPathBytes-1) // 恰好 512 字节
	overLimit := atLimit + "a"                                 // 513 字节

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
			// 超长仍必须被判为「站内路径」——长度由调用方比，判定函数不认识长度。
			if got := IsSameOriginPath(tc.in); !got {
				t.Errorf("IsSameOriginPath(%d 字节) = false：长度上限混进了判定函数（调用方的长度口径会被悄悄改掉）", len(tc.in))
			}
		})
	}

	if len(atLimit) != MaxReturnPathBytes || len(overLimit) != MaxReturnPathBytes+1 {
		t.Fatalf("用例构造有误：%d / %d", len(atLimit), len(overLimit))
	}
}

// TestLangRedirectPathIsTheOnlyCriterion 语言回跳的判据只有一份（task-75 自 internal/shell 迁入）。
//
// 它被两侧共用：渲染侧（adminui.Prepare，写语言切换表单的隐藏域）与消费侧
// （admin 的 adminSafeLangRedirect，校验 query 带回来的值）。此前消费侧重抄了一遍判据，
// 靠一条对照用例防漂移；现在判据只有这一处实现，这条用例把四条判据与长度口径逐项钉住 ——
// 放宽任何一条都是开放重定向面（//evil.example.com 会被浏览器当成协议相对 URL）。
//
// 返回空串表示拒绝：回落语义不在这一层（渲染侧回落当前请求路径、消费侧回落控制面首页）。
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

// TestLocalReturnPathSharesTheCriterion returnUrl 这类回跳参数与语言回跳共用同一份判据。
//
// 这条钉的是「不许长出第二份白名单」：两者分头实现必然漂移，而漏掉的那份只会在某次
// 安全复核里才被发现。逐项对照，任一侧放宽都会红。
func TestLocalReturnPathSharesTheCriterion(t *testing.T) {
	for _, raw := range []string{
		"/admin/coupons?status=1",  // 合法：带 query 的站内路径
		"/",                        // 合法：根路径
		"//evil.example.com/x",     // 协议相对
		"https://evil.example.com", // 绝对 URL
		"admin/coupons",            // 不以 / 开头
		"/admin/x\\..\\evil",       // 反斜杠
		"/admin/x\nLocation: /y",   // 控制字符
		"/" + strings.Repeat("a", MaxReturnPathBytes+1), // 超长
	} {
		if got, want := LocalReturnPath(raw), LangRedirectPath(raw); got != want {
			t.Errorf("LocalReturnPath(%q)=%q 与判据 %q 不一致（第二份白名单又长出来了）", raw, got, want)
		}
	}
	if got := LocalReturnPath("/admin/coupons?status=1"); got != "/admin/coupons?status=1" {
		t.Errorf("合法站内路径应原样返回，got %q", got)
	}
}
