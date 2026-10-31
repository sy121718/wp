package core

import "testing"

func TestIsSafeCSSValueRejectsScriptVectors(t *testing.T) {
	for _, v := range []string{

		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"JAVASCRIPT:alert(1)",
		" javascript:alert(1)",
		"url(javascript:alert(1))",
		"url( javascript:alert(1) )",
		"vbscript:msgbox(1)",
		"livescript:alert(1)",
		"mocha:alert(1)",

		"expression(alert(1))",
		"EXPRESSION(alert(1))",
		"expression (alert(1))",

		"behavior:url(x.htc)",
		"-moz-binding:url(x.xml)",

		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"url(data:text/html,x)",

		"@import url(x.css)",

		"url(https://evil.example/x.png)",
		"url(//evil.example/x.png)",
		"url( 'http://evil.example/x' )",
	} {
		if IsSafeCSSValue(v) {
			t.Errorf("%q 应被拒绝，实际放行 ——— 这是一条注入载体", v)
		}
	}
}

func TestIsSafeCSSValueAllowsLegitimateValues(t *testing.T) {
	for _, v := range []string{

		"",

		"#fff",
		"rgba(0,0,0,0.5)",
		"rgba(255, 255, 255, .08)",
		"rgb(10,20,30)",
		"hsl(210 40% 50%)",
		"var(--sky-c-primary)",
		"calc(100% - 32px)",
		"linear-gradient(135deg,#2563eb 0%,rgba(0,0,0,.2) 100%)",
		"radial-gradient(circle at 50% 0%,#fff,transparent)",

		"url(/img/a.jpg)",
		"url(img/a.jpg)",
		"url(/uploads/2026/01/x.webp?v=2)",

		"1.5rem",
		"100%",
		"-24px",
		"0 -12px 24px rgba(0,0,0,.12)",

		"Inter, system-ui, -apple-system, sans-serif",

		"monospace",
		"flex-start",
	} {
		if !IsSafeCSSValue(v) {
			t.Errorf("%q 应被放行，实际拒绝 ——— 存量合法取值被误伤", v)
		}
	}
}

func TestIsSafeCSSValueLengthCap(t *testing.T) {
	long := make([]byte, 0, 501)
	for i := 0; i < 501; i++ {
		long = append(long, 'a')
	}
	if IsSafeCSSValue(string(long)) {
		t.Error("超过 500 字符应被拒绝")
	}
	if !IsSafeCSSValue(string(long[:500])) {
		t.Error("恰好 500 字符应被放行")
	}
}

func TestContainerDelegatesToCore(t *testing.T) {

	if IsSafeCSSValue("javascript:alert(1)") {
		t.Fatal("core 侧放行了 javascript: ——— 容器委托也不会有救")
	}
	if !IsSafeCSSValue("rgba(0,0,0,.5)") {
		t.Fatal("core 侧拒绝了 rgba() ——— 容器 Overlay 会跟着坏")
	}
}
