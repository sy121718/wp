package builder

// ga4_test.go — 站点统计代码注入（BIZ-8）的字节级与安全边界断言。
//
// 三条不变量各有对应断言：空值零字节注入、形状不合法不注入（且绝不原样进 head）、
// 同一构建输入产出同一字节（确定性）。

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// TestGA4HeadEmptyAddsNoBytes 未配置测量 ID 时产物一个字节都不多。
func TestGA4HeadEmptyAddsNoBytes(t *testing.T) {
	if got := buildGA4Head(""); got != "" {
		t.Fatalf("空测量 ID 应零字节注入，实际 %q", got)
	}
	if got := buildGA4Head("   "); got != "" {
		t.Fatalf("纯空白测量 ID 应零字节注入，实际 %q", got)
	}
	body := &CompiledPage{HTML: "<p>x</p>", CSS: "p{color:red}"}
	doc, err := RenderDocument(body)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	if strings.Contains(doc, "googletagmanager") {
		t.Fatalf("未配置统计代码，产物却包含 gtag 引用:\n%s", doc)
	}
}

// TestGA4HeadInjectionIntoDocument 配置了合法测量 ID 时 head 里出现两段 gtag 脚本。
func TestGA4HeadInjectionIntoDocument(t *testing.T) {
	body := &CompiledPage{HTML: "<p>x</p>", GA4Head: buildGA4Head("G-ABC1234567")}
	doc, err := RenderDocument(body)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	headEnd := strings.Index(doc, "</head>")
	if headEnd < 0 {
		t.Fatalf("产物缺少 </head>:\n%s", doc)
	}
	head := doc[:headEnd]
	if !strings.Contains(head, "googletagmanager.com/gtag/js?id=G-ABC1234567") {
		t.Fatalf("head 缺少 gtag 脚本引用:\n%s", head)
	}
	if !strings.Contains(head, "gtag('config','G-ABC1234567')") {
		t.Fatalf("head 缺少 gtag config 调用:\n%s", head)
	}
	// 注入点在 head 内、且只出现一次（重复注入会把每次访问算成两次）。
	if n := strings.Count(doc, "gtag/js?id="); n != 1 {
		t.Fatalf("gtag 脚本应恰好出现 1 次，实际 %d 次", n)
	}
}

// TestGA4MeasurementIDShape 形状校验：合法归一化、非法一律拒绝。
func TestGA4MeasurementIDShape(t *testing.T) {
	valid := []string{"G-ABC1234567", "g-abc1234567", "  G-ABCD  ", "G-1234"}
	for _, in := range valid {
		id, ok := NormalizeGA4MeasurementID(in)
		if !ok {
			t.Errorf("应判为合法: %q", in)
			continue
		}
		if id != strings.ToUpper(strings.TrimSpace(in)) {
			t.Errorf("归一化结果不符: 输入 %q 得到 %q", in, id)
		}
	}
	invalid := []string{
		"", "   ", "UA-123456-1", "GTM-ABCDEF", "G-", "G-abc 123",
		// 注入形状：这些值一旦被拼进 head，就是脚本/属性逃逸。
		`G-ABC"><script>alert(1)</script>`, "G-ABC'1234", "G-ABC<br>",
	}
	for _, in := range invalid {
		if _, ok := NormalizeGA4MeasurementID(in); ok {
			t.Errorf("应判为非法: %q", in)
		}
		if got := buildGA4Head(in); got != "" {
			t.Errorf("非法测量 ID 必须零字节注入，输入 %q 得到 %q", in, got)
		}
	}
}

// TestCompileGA4Determinism 同一输入重复编译，GA4 片段与打点配置逐字节一致。
func TestCompileGA4Determinism(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	opts := []CompileOption{
		WithComponentSet(set),
		WithGA4MeasurementID("G-ABC1234567"),
		WithProjectID("2f1a4c0e-0000-4000-8000-000000000001"),
		WithLanguage("zh-CN"),
	}
	first, err := Compile(page, opts...)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if first.GA4Head == "" {
		t.Fatal("配置了合法测量 ID，编译结果却没有 GA4 片段")
	}
	if !strings.Contains(first.TrackConfig, `"projectId":"2f1a4c0e-0000-4000-8000-000000000001"`) {
		t.Fatalf("打点配置缺少工程 ID: %q", first.TrackConfig)
	}
	for i := 0; i < 5; i++ {
		got, cerr := Compile(page, opts...)
		if cerr != nil {
			t.Fatalf("第 %d 次 Compile: %v", i, cerr)
		}
		if got.GA4Head != first.GA4Head {
			t.Fatalf("第 %d 次编译 GA4 片段字节不一致:\n%q\n%q", i, first.GA4Head, got.GA4Head)
		}
		if got.TrackConfig != first.TrackConfig {
			t.Fatalf("第 %d 次编译打点配置字节不一致: %q vs %q", i, first.TrackConfig, got.TrackConfig)
		}
	}
}

// TestCompileGA4AbsentZeroBytes 未注入测量 ID / 工程 ID 时，编译结果零字节。
func TestCompileGA4AbsentZeroBytes(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	got, err := Compile(page, WithComponentSet(set))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got.GA4Head != "" || got.TrackConfig != "" {
		t.Fatalf("未配置时应零字节注入，实际 GA4Head=%q TrackConfig=%q", got.GA4Head, got.TrackConfig)
	}
}
