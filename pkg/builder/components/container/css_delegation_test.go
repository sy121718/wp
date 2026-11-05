package container

import (
	"testing"

	"go_wp/pkg/builder/core"
)

func TestIsSafeCSSValueDelegatesToCore(t *testing.T) {
	for _, v := range []string{

		"javascript:alert(1)",
		"url(javascript:alert(1))",
		"expression(alert(1))",
		"data:text/html;base64,PHNjcmlwdD4=",
		"url(//evil.example/x.png)",
		"url(https://evil.example/x.png)",
		"@import url(x.css)",

		"",
		"#fff",
		"rgba(0,0,0,.5)",
		"linear-gradient(135deg,#2563eb 0%,rgba(0,0,0,.2) 100%)",
		"url(/img/a.jpg)",
		"calc(100% - 32px)",
		"100%",
		"-24px",
	} {
		if got, want := IsSafeCSSValue(v), core.IsSafeCSSValue(v); got != want {
			t.Errorf("%q 上两份实现分叉：容器=%v core=%v —— 容器必须委托 core", v, got, want)
		}
	}
}
