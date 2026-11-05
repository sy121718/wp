package richdoc

import (
	"strings"
	"testing"
)

func TestRoundTripMalformedNestingKnownDrift(t *testing.T) {
	const src = "<A>0<tABle><A>"

	first, err := HTMLToNodes(src)
	if err != nil {
		t.Fatalf("首次导入失败：%v", err)
	}
	html1, err := NodesToHTML(first.Nodes)
	if err != nil {
		t.Fatalf("首次导出失败：%v", err)
	}
	second, err := HTMLToNodes(html1.HTML)
	if err != nil {
		t.Fatalf("二次导入失败（导出结果必须是合法富文本 HTML）：%v\nHTML=%q", err, html1.HTML)
	}
	html2, err := NodesToHTML(second.Nodes)
	if err != nil {
		t.Fatalf("二次导出失败：%v", err)
	}

	if html1.HTML == html2.HTML {
		t.Fatalf("该缺陷似乎已修复（往返已收敛）：请把本测试改成收敛断言（调用 checkRoundTrip），"+
			"并从 AGENTS.md CMP-15 与 AGENTS.md 决策 5 移除这条件挂账。\n第一次 %q",
			html1.HTML)
	}

	third, err := HTMLToNodes(html2.HTML)
	if err != nil {
		t.Fatalf("三次导入失败：%v\nHTML=%q", err, html2.HTML)
	}
	_ = third
	if containsScriptTag(html1.HTML) || containsScriptTag(html2.HTML) {
		t.Fatalf("导出结果里出现了脚本标签\n第一次 %q\n第二次 %q", html1.HTML, html2.HTML)
	}
}

func containsScriptTag(s string) bool {
	return strings.Contains(strings.ToLower(s), "<script")
}
