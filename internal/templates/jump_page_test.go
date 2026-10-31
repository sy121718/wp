package templates

import (
	"strings"
	"testing"
)

func jumpPageData(ok bool, msg, back, backText string, seconds int) map[string]any {
	return map[string]any{
		"title": msg,
		"t":     func(key, fallback string) string { return fallback },
		"tf":    func(key, fallback string, args []string) string { return fillForTest(fallback, args) },
		"Jump": map[string]any{
			"OK":           ok,
			"MsgKey":       "",
			"Msg":          msg,
			"MsgArgs":      []string{},
			"Back":         back,
			"BackTextKey":  "",
			"BackText":     backText,
			"BackTextShow": backText != "",
			"Seconds":      seconds,
		},
	}
}

func fillForTest(fallback string, args []string) string {
	out := fallback
	for _, a := range args {
		out = strings.Replace(out, "%s", a, 1)
	}
	return out
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

func TestJumpPageFailureShape(t *testing.T) {
	out := renderJumpPage(t, jumpPageData(false, "该优惠码已有核销记录", "/admin/coupons", "返回列表", 2))

	if !strings.Contains(out, `data-jump-state="err"`) {
		t.Fatalf("失败态标记缺失: %s", out)
	}
	if !strings.Contains(out, "!") {
		t.Fatal("失败态应显示感叹号图标")
	}
}

func TestJumpPageAutoRedirectOnlyWhenSecondsPositive(t *testing.T) {
	withRedirect := renderJumpPage(t, jumpPageData(true, "已保存", "/admin/coupons?project=p1&page=2", "返回", 1))
	if !strings.Contains(withRedirect, `http-equiv="refresh"`) {
		t.Fatal("停留秒数 > 0 时应输出 meta refresh")
	}

	if !strings.Contains(withRedirect, `content="1;url=/admin/coupons?project=p1&amp;page=2"`) {
		t.Fatalf("meta refresh 的 url 段不对: %s", withRedirect)
	}

	noRedirect := renderJumpPage(t, jumpPageData(true, "已保存", "/admin/coupons", "返回", 0))
	if strings.Contains(noRedirect, `http-equiv="refresh"`) {
		t.Fatal("停留秒数 0 时不应自动跳转")
	}
}

func TestJumpPageWithoutBackTextRendersNoLink(t *testing.T) {
	out := renderJumpPage(t, jumpPageData(true, "已保存", "/admin/coupons", "", 1))
	if strings.Contains(out, "<a class=\"btn btn-primary\"") {
		t.Fatalf("无链接文字时不应渲染链接: %s", out)
	}
}

func TestJumpPageEscapesMessage(t *testing.T) {
	out := renderJumpPage(t, jumpPageData(false, `<script>alert(1)</script>`, "/admin/coupons", "返回", 0))
	if strings.Contains(out, "<script>alert(1)</script>") {
		t.Fatal("正文未转义，可注入脚本")
	}
	if !strings.Contains(out, "&lt;script&gt;") {
		t.Fatalf("期望转义后的正文: %s", out)
	}
}
