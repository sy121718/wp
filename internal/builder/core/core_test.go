package core

import (
	"strings"
	"testing"
)

// TestIsSafeCSSValue CSS 注入防护白名单。
func TestIsSafeCSSValue(t *testing.T) {
	valid := []string{
		"#2563eb",
		"rgba(0,0,0,0.5)",
		"0 1px 3px rgba(0,0,0,0.12)",
		"var(--color-primary)",
		"50%",
		"1.5rem",
		"calc(100% - 20px)",
		"url(/img/a.jpg)", // 站内相对路径
		"",                // 空值视为合法（未设置）
	}
	for _, v := range valid {
		if !IsSafeCSSValue(v) {
			t.Errorf("应判定合法: %q", v)
		}
	}

	invalid := []string{
		"red;background:url(https://evil.com/x)", // 分号注入
		"url(https://evil.com/x)",                // 外联 http
		"url(//evil.com/x)",                      // 外联协议相对
		`url("https://evil.com/x")`,              // 引号包裹外联
		`url('//evil.com/x')`,
		"url('/img/a.jpg')",  // 引号形式一律拒绝（' 不在字符集）
		strings.Repeat("a", 501), // 超长
	}
	for _, v := range invalid {
		if IsSafeCSSValue(v) {
			t.Errorf("应判定非法: %q", v)
		}
	}
}

// TestCSSDecl 声明拼接：跳过空值。
func TestCSSDecl(t *testing.T) {
	tests := []struct {
		name   string
		values []string
		want   string
	}{
		{"正常", []string{"1px", "solid", "red"}, "border-top: 1px solid red"},
		{"全空", []string{"", ""}, "prop: "},
		{"跳过中间空值", []string{"a", "", "b"}, "x: a b"},
		{"单个", []string{"42px"}, "width: 42px"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prop := "prop"
			if strings.HasPrefix(tt.want, "border-top") {
				prop = "border-top"
			} else if strings.HasPrefix(tt.want, "x") {
				prop = "x"
			} else if strings.HasPrefix(tt.want, "width") {
				prop = "width"
			}
			if got := CSSDecl(prop, tt.values...); got != tt.want {
				t.Errorf("CSSDecl(%q,%q) = %q, want %q", prop, tt.values, got, tt.want)
			}
		})
	}
}

// TestCSSBucketsOrder 输出顺序确定性：关键帧 → 桌面 → 平板 → 手机。
func TestCSSBucketsOrder(t *testing.T) {
	b := &CSSBuckets{}
	b.Add(BreakpointMobile, ".m", []string{"color: blue"})
	b.Add(BreakpointDesktop, ".d", []string{"color: red"})
	b.Add(BreakpointTablet, ".t", []string{"color: green"})
	b.NeedKeyframes("wp-fade-in")
	out := b.String()

	di := strings.Index(out, ".d")
	ti := strings.Index(out, ".t")
	mi := strings.Index(out, ".m")
	ki := strings.Index(out, "@keyframes wp-fade-in")
	if !(ki < di && di < ti && ti < mi) {
		t.Fatalf("输出顺序错误 keyframes=%d desktop=%d tablet=%d mobile=%d\n%s", ki, di, ti, mi, out)
	}
}

// TestCSSBucketsSkipEmpty 空声明忽略：不产生无效规则。
func TestCSSBucketsSkipEmpty(t *testing.T) {
	b := &CSSBuckets{}
	b.Add(BreakpointDesktop, ".x", []string{"", " ", "color: red"})
	b.Add(BreakpointDesktop, ".empty", []string{"", ""})
	out := b.String()
	if strings.Contains(out, ".empty") {
		t.Fatalf("空声明规则不应输出: %s", out)
	}
	if !strings.Contains(out, ".x") {
		t.Fatalf("有效规则应输出: %s", out)
	}
}

// TestValidateNodeID ID 白名单、长度、唯一性、Name 长度。
func TestValidateNodeID(t *testing.T) {
	ids := map[string]bool{}
	t.Run("合法 ID", func(t *testing.T) {
		for _, id := range []string{"a", "node-1", "abc_XYZ-09", strings.Repeat("x", 64)} {
			if err := ValidateNodeID(id, "", ids); err != nil {
				t.Errorf("ID %q 应合法: %v", id, err)
			}
		}
	})
	t.Run("非法 ID", func(t *testing.T) {
		ids2 := map[string]bool{}
		for _, id := range []string{"", "a b", "点", "a/b", "a.b", strings.Repeat("x", 65)} {
			if err := ValidateNodeID(id, "", ids2); err == nil {
				t.Errorf("ID %q 应非法", id)
			}
		}
	})
	t.Run("重复 ID", func(t *testing.T) {
		ids3 := map[string]bool{}
		_ = ValidateNodeID("dup", "", ids3)
		if err := ValidateNodeID("dup", "", ids3); err == nil {
			t.Fatalf("重复 ID 应报错")
		}
	})
	t.Run("Name 超长", func(t *testing.T) {
		if err := ValidateNodeID("ok", strings.Repeat("名", 101), map[string]bool{}); err == nil {
			t.Fatalf("Name 超 100 字符应报错")
		}
	})
}

// TestValidateTextStyle 排版校验：字号/行高/clamp/对齐白名单。
func TestValidateTextStyle(t *testing.T) {
	valid := []TextStyle{
		{},
		{Desktop: TextStyleValue{FontSize: "16px", LineHeight: "1.5", TextAlign: "center"}},
		{Tablet: TextStyleValue{FontSize: "clamp(12px, 2vw, 20px)"}},
		{Mobile: TextStyleValue{FontSize: "1.2rem"}},
		{Desktop: TextStyleValue{FontSize: "16"}}, // 无单位数字在字符集内（设计允许）
	}
	for _, ts := range valid {
		if err := ValidateTextStyle("n1", &ts); err != nil {
			t.Errorf("应合法: %+v err=%v", ts, err)
		}
	}

	invalid := []TextStyle{
		{Desktop: TextStyleValue{FontSize: "bold"}},            // 非长度
		{Desktop: TextStyleValue{LineHeight: "calc(100%)"}},    // 行高不支持 calc
		{Desktop: TextStyleValue{TextAlign: "middle"}},         // 非法对齐
		{Desktop: TextStyleValue{FontSize: strings.Repeat("1", 41)}}, // 超 40 上限
		{Desktop: TextStyleValue{FontSize: "16px;color:red"}},  // 注入
	}
	for _, ts := range invalid {
		if err := ValidateTextStyle("n1", &ts); err == nil {
			t.Errorf("应非法: %+v", ts)
		}
	}
}

// TestTextStyleDecls 排版声明生成。
func TestTextStyleDecls(t *testing.T) {
	v := TextStyleValue{FontSize: "16px", LineHeight: "1.5", TextAlign: "right"}
	got := strings.Join(v.Decls(), "|")
	want := "font-size: 16px|line-height: 1.5|text-align: right"
	if got != want {
		t.Fatalf("Decls = %q, want %q", got, want)
	}
	if len(TextStyleValue{}.Decls()) != 0 {
		t.Fatalf("空值不应产生声明")
	}
}

// fakeComponent 测试用最小组件。
type fakeComponent struct{ t string }

func (f fakeComponent) Type() string { return f.t }
func (f fakeComponent) Validate(_ *Node, _ map[string]bool) error { return nil }

// TestRegisterLookup 组件注册表：注册/查询/类型排序确定性。
func TestRegisterLookup(t *testing.T) {
	// 独立注册测试组件（core 单测不依赖其他包的 init）。
	Register(fakeComponent{t: "core.test_fake"})
	defer func() {
		delete(registry, "core.test_fake")
	}()

	if _, err := Lookup("core.test_fake"); err != nil {
		t.Fatalf("注册后应可查询: %v", err)
	}
	if _, err := Lookup("core.not_exist"); err == nil {
		t.Fatalf("未知类型应报错")
	}
	types := Types()
	for i := 1; i < len(types); i++ {
		if types[i-1] >= types[i] {
			t.Fatalf("Types 应确定性排序: %s >= %s", types[i-1], types[i])
		}
	}
}
