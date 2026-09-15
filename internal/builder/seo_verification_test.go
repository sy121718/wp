package builder

// seo_verification_test.go — GSC 站点验证 meta 注入（SEO-009）的字节级与安全边界断言。
//
// 与 ga4_test.go 同形：空值零字节、设值进 head、非法值被拒（绝不原样进 head）、
// 同一构建输入产出同一字节；额外钉住一处与 GA4 相反的口径 —— token 大小写敏感。

import (
	"strings"
	"testing"

	"go_wp/internal/templates"
)

// seoTestToken 合法样本：base64url 字符集，含 - 与 _ 与混合大小写（40 位）。
const seoTestToken = "AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_aB"

// TestSearchConsoleHeadEmptyAddsNoBytes 未配置 token 时产物一个字节都不多。
func TestSearchConsoleHeadEmptyAddsNoBytes(t *testing.T) {
	if got := buildSearchConsoleHead(""); got != "" {
		t.Fatalf("空 token 应零字节注入，实际 %q", got)
	}
	if got := buildSearchConsoleHead("   "); got != "" {
		t.Fatalf("纯空白 token 应零字节注入，实际 %q", got)
	}
	body := &CompiledPage{HTML: "<p>x</p>", CSS: "p{color:red}"}
	doc, err := RenderDocument(body)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	if strings.Contains(doc, "google-site-verification") {
		t.Fatalf("未配置站点验证，产物却包含验证 meta:\n%s", doc)
	}
}

// TestSearchConsoleHeadInjectionIntoDocument 配置了合法 token 时 head 里出现验证 meta。
func TestSearchConsoleHeadInjectionIntoDocument(t *testing.T) {
	body := &CompiledPage{HTML: "<p>x</p>", SearchConsoleHead: buildSearchConsoleHead(seoTestToken)}
	doc, err := RenderDocument(body)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	headEnd := strings.Index(doc, "</head>")
	if headEnd < 0 {
		t.Fatalf("产物缺少 </head>:\n%s", doc)
	}
	head := doc[:headEnd]
	want := "<meta name=\"google-site-verification\" content=\"" + seoTestToken + "\">"
	if !strings.Contains(head, want) {
		t.Fatalf("head 缺少验证 meta（期望 %q）:\n%s", want, head)
	}
	// 注入点在 head 内、且只出现一次（重复注入对验证无意义，但说明装配被调了两次）。
	if n := strings.Count(doc, "google-site-verification"); n != 1 {
		t.Fatalf("验证 meta 应恰好出现 1 次，实际 %d 次", n)
	}
}

// TestSearchConsoleVerificationCasePreserved token 大小写敏感：归一化只去空白、不折叠大小写。
func TestSearchConsoleVerificationCasePreserved(t *testing.T) {
	token, ok := NormalizeSearchConsoleVerification("  " + seoTestToken + "  ")
	if !ok {
		t.Fatalf("应判为合法: %q", seoTestToken)
	}
	if token != seoTestToken {
		t.Fatalf("归一化改变了 token 字节: 输入 %q 得到 %q", seoTestToken, token)
	}
	lower := strings.ToLower(seoTestToken)
	got, ok := NormalizeSearchConsoleVerification(lower)
	if !ok {
		t.Fatalf("小写形式同样合法: %q", lower)
	}
	if got != lower || got == token {
		t.Fatalf("大小写不得被折叠（与 GA4 的 ToUpper 相反）: 输入 %q 得到 %q", lower, got)
	}
}

// TestSearchConsoleVerificationShape 形状校验：合法通过、非法一律拒绝且零字节注入。
func TestSearchConsoleVerificationShape(t *testing.T) {
	valid := []string{"AbCdEfGh", "a_b-cD9-12345678", seoTestToken}
	for _, in := range valid {
		if !ValidSearchConsoleVerification(in) {
			t.Errorf("应判为合法: %q", in)
		}
	}
	invalid := []string{
		"", "   ", "abcdefg", "short",
		// 注入形状：这些值一旦被拼进 head，就是属性逃逸 / 标签逃逸。
		"X\"><script>alert(1)</script>", "tok'en", "tok en", "token<br>", "令牌abcdefgh",
		"<meta name=\"google-site-verification\" content=\"abc\">",
	}
	for _, in := range invalid {
		if ValidSearchConsoleVerification(in) {
			t.Errorf("应判为非法: %q", in)
		}
		if got := buildSearchConsoleHead(in); got != "" {
			t.Errorf("非法 token 必须零字节注入，输入 %q 得到 %q", in, got)
		}
	}
}

// TestCompileSearchConsoleInvalidNotInjected 非法 token 经编译选项进入构建，产物零字节且无残留。
func TestCompileSearchConsoleInvalidNotInjected(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	evil := "X\"><script>alert(1)</script>"
	got, err := Compile(page, WithComponentSet(set), WithSearchConsoleVerification(evil))
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if got.SearchConsoleHead != "" {
		t.Fatalf("非法 token 必须零字节注入，实际 %q", got.SearchConsoleHead)
	}
	doc, err := RenderDocument(got)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	if strings.Contains(doc, "google-site-verification") || strings.Contains(doc, "alert(1)") {
		t.Fatalf("非法输入泄漏进产物:\n%s", doc)
	}
}

// TestCompileSearchConsoleDeterminism 同一输入重复编译，验证 meta 逐字节一致。
func TestCompileSearchConsoleDeterminism(t *testing.T) {
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
		WithSearchConsoleVerification(seoTestToken),
		WithProjectID("2f1a4c0e-0000-4000-8000-000000000001"),
		WithLanguage("zh-CN"),
	}
	first, err := Compile(page, opts...)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if !strings.Contains(first.SearchConsoleHead, seoTestToken) {
		t.Fatalf("配置了合法 token，编译结果却没有验证 meta: %q", first.SearchConsoleHead)
	}
	for i := 0; i < 5; i++ {
		got, cerr := Compile(page, opts...)
		if cerr != nil {
			t.Fatalf("第 %d 次 Compile: %v", i, cerr)
		}
		if got.SearchConsoleHead != first.SearchConsoleHead {
			t.Fatalf("第 %d 次编译验证 meta 字节不一致:\n%q\n%q", i, first.SearchConsoleHead, got.SearchConsoleHead)
		}
	}
}

// TestCompileSearchConsoleAbsentZeroBytes 未注入 token 时编译结果零字节。
func TestCompileSearchConsoleAbsentZeroBytes(t *testing.T) {
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
	if got.SearchConsoleHead != "" {
		t.Fatalf("未配置时应零字节注入，实际 SearchConsoleHead=%q", got.SearchConsoleHead)
	}
}

// TestSearchConsoleEndToEndRender 走完整链路：Compile（注入）→ RenderDocument，产物 head 里出现验证 meta。
func TestSearchConsoleEndToEndRender(t *testing.T) {
	set, err := templates.NewComponentSet("../templates/components")
	if err != nil {
		t.Fatalf("NewComponentSet: %v", err)
	}
	page, err := ParsePage([]byte(jetDocJSON))
	if err != nil {
		t.Fatalf("ParsePage: %v", err)
	}
	// 有值：产物 head 里出现验证 meta。
	compiled, err := Compile(page,
		WithComponentSet(set),
		WithSearchConsoleVerification(seoTestToken),
		WithLanguage("zh-CN"),
	)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	doc, err := RenderDocument(compiled)
	if err != nil {
		t.Fatalf("RenderDocument: %v", err)
	}
	headEnd := strings.Index(doc, "</head>")
	if headEnd < 0 {
		t.Fatalf("产物缺少 </head>")
	}
	want := "<meta name=\"google-site-verification\" content=\"" + seoTestToken + "\">"
	if !strings.Contains(doc[:headEnd], want) {
		t.Fatalf("产物 head 缺少验证 meta（期望 %q）:\n%s", want, doc[:headEnd])
	}
	// 空值：同一页面不注入时产物零字节（对比同一构建路径）。
	compiled, err = Compile(page, WithComponentSet(set), WithLanguage("zh-CN"))
	if err != nil {
		t.Fatalf("Compile(空): %v", err)
	}
	doc, err = RenderDocument(compiled)
	if err != nil {
		t.Fatalf("RenderDocument(空): %v", err)
	}
	if strings.Contains(doc, "google-site-verification") {
		t.Fatalf("未配置 token 时产物不得含验证 meta:\n%s", doc)
	}
}
