package container

// css_delegation_test.go — 容器白名单必须委托 core，不许自带第二份实现。
//
// 历史现场：容器曾有一份自己的 cssValueRe（字符集与 core 相同但少了外联封禁），
// 于是 `url(//attacker)` 在 core 侧被拒、在容器侧放行 —— 而容器恰好是最容易拿到
// 任意 CSS 值的组件。两份实现分叉的成本总是落在「收紧的一方以为已经收紧了」。
//
// 判据：同一组值的结论必须与 core 完全一致，且容器侧源码里不再出现自己的正则。
// 前者管行为，后者管「别再加回第三份」。

import (
	"testing"

	"go_wp/internal/builder/core"
)

func TestIsSafeCSSValueDelegatesToCore(t *testing.T) {
	for _, v := range []string{
		// 注入载体：容器侧必须与 core 一样拒绝。
		"javascript:alert(1)",
		"url(javascript:alert(1))",
		"expression(alert(1))",
		"data:text/html;base64,PHNjcmlwdD4=",
		"url(//evil.example/x.png)",
		"url(https://evil.example/x.png)",
		"@import url(x.css)",
		// 存量合法值：容器 Overlay / 渐变 / 尺寸。
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
