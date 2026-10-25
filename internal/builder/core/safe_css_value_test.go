package core

// safe_css_value_test.go — CSS 值白名单的**拒绝面**契约。
//
// 这个文件的存在理由是两处实测的真缺陷：`javascript:alert(1)`、
// `url(javascript:alert(1))`、`expression(alert(1))` 曾经全部返回 true ——
// 字符集层为容纳 rgba() 保留了括号、为容纳渐变保留了冒号，于是「不需要引号和分号」
// 的注入载体一路通行。
//
// 判据分两半，缺一半就是自欺：
//   - **必须拒绝**（下面这张表的每一行都是一个真实/典型注入载体）；
//   - **必须放行**（存量的合法取值；只写前者的话，把白名单改成"返回 false"也能全绿）。
//
// 每次新增拒绝规则都要同时问：「它误伤了哪条合法取值？」—— 放行表就是答案的存档。

import "testing"

func TestIsSafeCSSValueRejectsScriptVectors(t *testing.T) {
	for _, v := range []string{
		// 脚本协议：不需要引号与分号，字符集层完全看不见。
		"javascript:alert(1)",
		"JavaScript:alert(1)",
		"JAVASCRIPT:alert(1)",
		" javascript:alert(1)",
		"url(javascript:alert(1))",
		"url( javascript:alert(1) )",
		"vbscript:msgbox(1)",
		"livescript:alert(1)",
		"mocha:alert(1)",
		// IE 表达式：E 系列浏览器里等于 eval。
		"expression(alert(1))",
		"EXPRESSION(alert(1))",
		"expression (alert(1))",
		// 可引入外部脚本的遗留属性值。
		"behavior:url(x.htc)",
		"-moz-binding:url(x.xml)",
		// 内联数据资源：data:text/html 能直接携带脚本。
		"data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==",
		"url(data:text/html,x)",
		// CSS 导入。
		"@import url(x.css)",
		// 已有的外联封禁（回归：别在重构这一层时把它弄丢）。
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
		// 空值：未设置的字段（零值到处在传）。
		"",
		// 颜色与函数值：容器 Overlay / 渐变 / calc 靠这些。
		"#fff",
		"rgba(0,0,0,0.5)",
		"rgba(255, 255, 255, .08)",
		"rgb(10,20,30)",
		"hsl(210 40% 50%)",
		"var(--sky-c-primary)",
		"calc(100% - 32px)",
		"linear-gradient(135deg,#2563eb 0%,rgba(0,0,0,.2) 100%)",
		"radial-gradient(circle at 50% 0%,#fff,transparent)",
		// 站内相对路径：url() 的合法用法（外联才拦）。
		"url(/img/a.jpg)",
		"url(img/a.jpg)",
		"url(/uploads/2026/01/x.webp?v=2)",
		// 尺寸与布局值。
		"1.5rem",
		"100%",
		"-24px",
		"0 -12px 24px rgba(0,0,0,.12)",
		// 字体栈（逗号 + 空格）。
		"Inter, system-ui, -apple-system, sans-serif",
		// 关键词里恰好含危险词的字面片段：判据是「构造」（: 或 ( 紧跟），不是词本身。
		"monospace",
		"flex-start",
	} {
		if !IsSafeCSSValue(v) {
			t.Errorf("%q 应被放行，实际拒绝 ——— 存量合法取值被误伤", v)
		}
	}
}

// TestIsSafeCSSValueLengthCap 长度上限单独钉一次：它是唯一的非正则判据，容易被漏。
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

// TestContainerDelegatesToCore 容器组件的白名单必须就是 core 这份。
//
// 两份实现今天字面相同、明天就会分叉 —— 而分叉形态最糟：容器是最容易拿到任意 CSS 值的
// 组件，用旧的那份等于全局收紧对它无效。这里用同一组注入载体与合法值双向对账。
func TestContainerDelegatesToCore(t *testing.T) {
	// 该测试在核心包内跑：容器侧委托关系的判据是「两边对同一组值给出相同结论」，
	// 真正调用 container.IsSafeCSSValue 的对账在 components/container 包内
	// （见 css_delegation_test.go），这里只钉住 core 侧结论稳定。
	if IsSafeCSSValue("javascript:alert(1)") {
		t.Fatal("core 侧放行了 javascript: ——— 容器委托也不会有救")
	}
	if !IsSafeCSSValue("rgba(0,0,0,.5)") {
		t.Fatal("core 侧拒绝了 rgba() ——— 容器 Overlay 会跟着坏")
	}
}
