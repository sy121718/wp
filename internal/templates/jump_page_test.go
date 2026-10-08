package templates

// jump_page_test.go — 提示页（admin/jump.html）的渲染判据。
//
// 提示页替换的是「302 + ?err= / ?ok= / ?done= 回列表页」那条通道，所以它必须自己把
// 三件事说清楚：**发生了什么**（正文）、**去哪儿**（链接）、**多久走**（meta refresh）。
// 判据全部落在渲染产物上：
//
//  1. 正文出现且被转义 —— 文案里带 HTML 特殊字符时不能拼进结构；
//  2. 停留秒数 > 0 才输出 meta refresh，且 url 就是回跳地址（秒数 0 时不能自动跳，
//     否则「只给链接不自动跳」这个选项形同虚设）；
//  3. 没有链接文字时不渲染空链接 —— 一个没有文字的 <a> 在页面上是个看不见的按钮；
//  4. 整页渲染完成（有 </html>）—— 缺键导致的中断在这里表现为半截页面，
//     而半截页面在 htmx 那一档是「点了没反应」，最难查。

import (
	"strings"
	"testing"
)

// jumpPageData 复刻 shell.RenderJump 传的键（struct 在模板里按字段名取，与 map 等价）。
func jumpPageData(ok bool, msg, back, backText string, seconds int) map[string]any {
	return map[string]any{
		"title": msg,
		"Jump": map[string]any{
			"OK":       ok,
			"Msg":      msg,
			"Back":     back,
			"BackText": backText,
			"Seconds":  seconds,
		},
	}
}

func renderJumpPage(t *testing.T, data map[string]any) string {
	t.Helper()
	out, err := render(t, groupDSet(t), "admin/jump.html", data)
	if err != nil {
		t.Fatalf("渲染提示页失败: %v", err)
	}
	if strings.Contains(out, "{{") {
		t.Fatalf("提示页输出残留模板语法（Jet 在中间某行中断）: %s", out)
	}
	return out
}

// TestJumpPageSuccessShape 成功提示：勾形图标、正文、链接、整页完整。
func TestJumpPageSuccessShape(t *testing.T) {
	out := renderJumpPage(t, jumpPageData(true, "已删除 2 张优惠码", "/admin/coupons?project=p1", "返回优惠码列表", 1))

	if !strings.Contains(out, `data-jump-state="ok"`) {
		t.Fatalf("成功态标记缺失: %s", out)
	}
	if !strings.Contains(out, "✓") {
		t.Fatal("成功态应显示勾形图标")
	}
	if !strings.Contains(out, "已删除 2 张优惠码") {
		t.Fatal("正文缺失")
	}
	if !strings.Contains(out, `href="/admin/coupons?project=p1"`) {
		t.Fatalf("回跳链接缺失或落点不对: %s", out)
	}
	if !strings.Contains(out, "返回优惠码列表") {
		t.Fatal("链接文字缺失")
	}
	if !strings.Contains(out, "</html>") {
		t.Fatal("整页未渲染完成（后半截被中断）")
	}
}

// TestJumpPageFailureShape 失败提示：感叹号图标 + err 态。
func TestJumpPageFailureShape(t *testing.T) {
	out := renderJumpPage(t, jumpPageData(false, "该优惠码已有核销记录", "/admin/coupons", "返回列表", 2))

	if !strings.Contains(out, `data-jump-state="err"`) {
		t.Fatalf("失败态标记缺失: %s", out)
	}
	if !strings.Contains(out, "!") {
		t.Fatal("失败态应显示感叹号图标")
	}
}

// TestJumpPageAutoRedirectOnlyWhenSecondsPositive 秒数决定是否自动跳转。
func TestJumpPageAutoRedirectOnlyWhenSecondsPositive(t *testing.T) {
	withRedirect := renderJumpPage(t, jumpPageData(true, "已保存", "/admin/coupons?project=p1&page=2", "返回", 1))
	if !strings.Contains(withRedirect, `http-equiv="refresh"`) {
		t.Fatal("停留秒数 > 0 时应输出 meta refresh")
	}
	// url 里的 & 在 HTML 属性里应当被转义成 &amp;（浏览器会解码回 &），
	// 这里断言的是「回跳地址进了 refresh 的 url 段」。
	if !strings.Contains(withRedirect, `content="1;url=/admin/coupons?project=p1&amp;page=2"`) {
		t.Fatalf("meta refresh 的 url 段不对: %s", withRedirect)
	}

	noRedirect := renderJumpPage(t, jumpPageData(true, "已保存", "/admin/coupons", "返回", 0))
	if strings.Contains(noRedirect, `http-equiv="refresh"`) {
		t.Fatal("停留秒数 0 时不应自动跳转")
	}
}

// TestJumpPageWithoutBackTextRendersNoLink 没有链接文字就不渲染链接（避免空按钮）。
func TestJumpPageWithoutBackTextRendersNoLink(t *testing.T) {
	out := renderJumpPage(t, jumpPageData(true, "已保存", "/admin/coupons", "", 1))
	if strings.Contains(out, "<a class=\"btn btn-primary\"") {
		t.Fatalf("无链接文字时不应渲染链接: %s", out)
	}
}

// TestJumpPageEscapesMessage 正文里的 HTML 特殊字符必须转义。
func TestJumpPageEscapesMessage(t *testing.T) {
	out := renderJumpPage(t, jumpPageData(false, `<script>alert(1)</script>`, "/admin/coupons", "返回", 0))
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Fatal("正文未转义，可注入脚本")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("期望转义后的正文: %s", out)
	}
}
