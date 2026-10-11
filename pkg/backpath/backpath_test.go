package backpath

import (
	"net/url"
	"strings"
	"testing"
)

// buildCase 一条 Build 用例。
//
// target 是**请求 URI 的 query 来源**（空串表示 source 为 nil）；path 空串表示用默认的
// 合法站内路径。rule 非空表示它守着哪条值级规则（empty/overlong/control），表尾的自检
// 保证每条规则都有隔离用例 —— 否则删掉一条规则这条测试仍会全绿，等于空转。
type buildCase struct {
	name   string
	target string
	path   string
	keys   []string
	want   string
	ok     bool
	rule   string
}

func TestBuild(t *testing.T) {
	const defaultPath = "/admin/coupons"

	long := strings.Repeat("k", MaxQueryValueBytes)      // 恰好 200 字节
	tooLong := strings.Repeat("k", MaxQueryValueBytes+1) // 201 字节
	okPath := "/" + strings.Repeat("a", 512-1)           // 恰好 512 字节的站内路径
	tooLongPath := okPath + "a"                          // 513 字节

	cases := []buildCase{
		// —— 白名单：只有列出的键会被透传 ——
		{
			name:   "只透传列出的键",
			target: "/admin/coupons/create?project=p1&status=enabled&evil=1&keyword=k",
			keys:   []string{"project", "status"},
			want:   "/admin/coupons?project=p1&status=enabled",
			ok:     true,
		},
		{
			name:   "键顺序不影响产物（按键排序）",
			target: "/admin/coupons/create?status=enabled&project=p1&page=2",
			keys:   []string{"project", "status", "page"},
			want:   "/admin/coupons?page=2&project=p1&status=enabled",
			ok:     true,
		},
		{
			name:   "没有任何参数时保持路径原样（不带问号）",
			target: "/admin/coupons/create",
			keys:   []string{"project"},
			want:   "/admin/coupons",
			ok:     true,
		},
		{
			name: "source 为 nil 安全",
			keys: []string{"project"},
			want: "/admin/coupons",
			ok:   true,
		},

		// —— 值级规则 1：trim 后为空 → 丢弃且静默 ——
		{
			name:   "空值与纯空白值都丢弃",
			target: "/admin/coupons/create?project=p1&status=&keyword=%20%20",
			keys:   []string{"project", "status", "keyword"},
			want:   "/admin/coupons?project=p1",
			ok:     true,
			rule:   "empty",
		},

		// —— 值级规则 2：超长（>200 字节）→ 丢弃 ——
		{
			name:   "201 字节的值被丢弃",
			target: "/admin/coupons/create?project=p1&keyword=" + tooLong,
			keys:   []string{"project", "keyword"},
			want:   "/admin/coupons?project=p1",
			ok:     true,
			rule:   "overlong",
		},
		{
			name:   "恰好 200 字节的值保留（边界）",
			target: "/admin/coupons/create?keyword=" + long,
			keys:   []string{"keyword"},
			want:   "/admin/coupons?keyword=" + long,
			ok:     true,
		},

		// —— 值级规则 3：含 C0 控制字符 / DEL → 丢弃 ——
		{
			name:   "换行值被丢弃",
			target: "/admin/coupons/create?keyword=a%0Ab&project=p1",
			keys:   []string{"project", "keyword"},
			want:   "/admin/coupons?project=p1",
			ok:     true,
			rule:   "control",
		},
		{
			name:   "制表符 / NUL / DEL 值都被丢弃",
			target: "/admin/coupons/create?a=x%09y&b=x%00y&c=x%7Fy&project=p1",
			keys:   []string{"project", "a", "b", "c"},
			want:   "/admin/coupons?project=p1",
			ok:     true,
			rule:   "control",
		},

		// —— 编码与 trim：透传的是 trim 后的值，编码交给 url.Values ——
		{
			name:   "值里的分隔符被转义",
			target: "/admin/coupons/create?keyword=a%26b%3Dc",
			keys:   []string{"keyword"},
			want:   "/admin/coupons?keyword=a%26b%3Dc",
			ok:     true,
		},
		{
			name:   "值首尾空白被 trim 掉",
			target: "/admin/coupons/create?keyword=%20%20p1%20%20",
			keys:   []string{"keyword"},
			want:   "/admin/coupons?keyword=p1",
			ok:     true,
		},
		{
			name:   "非 ASCII 值按 UTF-8 百分号编码",
			target: "/admin/coupons/create?keyword=%E4%BC%98%E6%83%A0%E7%A0%81",
			keys:   []string{"keyword"},
			want:   "/admin/coupons?keyword=%E4%BC%98%E6%83%A0%E7%A0%81",
			ok:     true,
		},

		// —— 既有行为 1：同一键重复时取第一个 ——
		{
			name:   "同一键重复出现取第一个值（url.Values.Get 语义，既有行为）",
			target: "/admin/coupons/create?keyword=first&keyword=second",
			keys:   []string{"keyword"},
			want:   "/admin/coupons?keyword=first",
			ok:     true,
		},

		// —— 既有行为 2：path 自带 query 时不解析、直接字符串拼（调用点传的都是裸路径）——
		{
			name:   "path 自带 query 时按字符串拼（既有行为）",
			target: "/admin/coupons/create?project=p1",
			path:   "/admin/coupons?filter=1",
			keys:   []string{"project"},
			want:   "/admin/coupons?filter=1?project=p1",
			ok:     true,
		},

		// —— 路径判据：不合法返回 ("", false) ——
		{name: "协议相对跳转被拒", target: "/admin/coupons/create?project=p1", path: "//evil.example.com/x", keys: []string{"project"}, want: "", ok: false},
		{name: "绝对 URL 被拒", path: "https://evil.example.com", keys: []string{"project"}, want: "", ok: false},
		{name: "不以斜杠开头被拒", path: "admin/coupons", keys: []string{"project"}, want: "", ok: false},
		{name: "反斜杠被拒", path: `/admin/x\..\evil`, keys: []string{"project"}, want: "", ok: false},
		{name: "控制字符被拒", path: "/admin/x\nLocation: /y", keys: []string{"project"}, want: "", ok: false},
		{name: "513 字节路径被拒", path: tooLongPath, keys: []string{"project"}, want: "", ok: false},
		{name: "恰好 512 字节路径合法（边界）", target: "/admin/coupons/create?project=p1", path: okPath, keys: []string{"project"}, want: okPath + "?project=p1", ok: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := tc.path
			if path == "" {
				path = defaultPath
			}
			var src url.Values
			if tc.target != "" {
				u, err := url.Parse(tc.target)
				if err != nil {
					t.Fatalf("用例的 target 不合法: %v", err)
				}
				src = u.Query()
			}
			if tc.rule != "" && !tc.ok {
				t.Fatalf("表写错了：标了值级规则 %s 的用例却是路径被拒", tc.rule)
			}
			if tc.rule == "" && !tc.ok && tc.path == "" {
				t.Fatalf("表写错了：路径被拒的用例必须显式给出 path")
			}
			got, ok := Build(path, src, tc.keys...)
			if ok != tc.ok {
				t.Fatalf("Build(%q, %v, %v) ok = %v，应为 %v", path, src, tc.keys, ok, tc.ok)
			}
			if got != tc.want {
				t.Fatalf("Build(%q, %v, %v) = %q，应为 %q", path, src, tc.keys, got, tc.want)
			}
		})
	}

	// 非空转自检：三条值级规则必须各自至少有一个隔离用例。
	//
	// 注意 empty 这条由**两处**共同实现：Build 里 `if v == "" { continue }`（显式跳过、不记日志）
	// 与 KeepValue 的 `v == "" → false`（把空串判成不可透传）。所以它的隔离变异必须同时改两处，
	// 只改一处不会有任何可观察差异（两者是冗余实现，不是双保险缺口）。
	rules := map[string]bool{}
	for _, tc := range cases {
		if tc.rule != "" {
			rules[tc.rule] = true
		}
	}
	for _, want := range []string{"empty", "overlong", "control"} {
		if !rules[want] {
			t.Errorf("值级规则 %s 在表里没有隔离用例：删掉它这条测试也不会红，等于空转", want)
		}
	}
}

func TestKeepValue(t *testing.T) {
	long := strings.Repeat("k", MaxQueryValueBytes)
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"普通筛选值", "p1", true},
		{"中文值", "优惠码", true},
		{"恰好 200 字节", long, true},
		{"201 字节", long + "k", false},
		{"空串", "", false},
		{"换行", "a\nb", false},
		{"制表符", "a\tb", false},
		{"NUL", "a\x00b", false},
		{"DEL", "a\x7fb", false},
		{"空格不算控制字符（是否 trim 由调用方决定）", "a b", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := KeepValue(tc.in); got != tc.want {
				t.Errorf("KeepValue(%q) = %v，应为 %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestWithParams(t *testing.T) {
	cases := []struct {
		name   string
		path   string
		params map[string]string
		want   string
	}{
		{"nil 参数保持原样", "/admin/orders", nil, "/admin/orders"},
		{"空 map 保持原样", "/admin/orders", map[string]string{}, "/admin/orders"},
		{"空值丢弃", "/admin/orders", map[string]string{"project": ""}, "/admin/orders"},
		{"全空白值丢弃", "/admin/orders", map[string]string{"project": "  "}, "/admin/orders"},
		{"单参数", "/admin/orders", map[string]string{"project": "p1"}, "/admin/orders?project=p1"},
		{"键有序", "/admin/orders", map[string]string{"b": "2", "a": "1"}, "/admin/orders?a=1&b=2"},
		{"空格编码成 +", "/admin/orders", map[string]string{"q": "a b"}, "/admin/orders?q=a+b"},
		{"值里的分隔符转义", "/admin/orders", map[string]string{"q": "a&b=c"}, "/admin/orders?q=a%26b%3Dc"},
		// 与 Build 的差异：WithParams 只看「是不是全空白」，**不 trim 透传值**（既有语义）。
		{"透传值不 trim（与 Build 的差异，既有语义）", "/admin/orders", map[string]string{"q": " a "}, "/admin/orders?q=+a+"},
		{"path 自带 query 时按字符串拼（既有行为）", "/admin/orders?filter=1", map[string]string{"project": "p1"}, "/admin/orders?filter=1?project=p1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WithParams(tc.path, tc.params); got != tc.want {
				t.Errorf("WithParams(%q, %v) = %q，应为 %q", tc.path, tc.params, got, tc.want)
			}
		})
	}
}
