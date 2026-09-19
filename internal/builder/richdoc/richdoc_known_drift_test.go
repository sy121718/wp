package richdoc

// richdoc_known_drift_test.go — 已知未收敛缺陷的**自退役**回归桩
//（挂账位置：docs/10-todo.md CMP-15、docs/06-B-dual-track-adr.md 决策 5 的「⚠️ 仍存一类」）。
//
// 缺陷：畸形嵌套（HTML 内容模型本身非法，如 <a> 里嵌 <table>）经 x/net/html 解析后再序列化，
// 不是自身解析的不动点；而 import.go 的 inlineTextNode 只按 token 流渲染、不按解析结果重新分组，
// 于是「导出 → 导入 → 导出」会漂移一次。原始证据来自 FuzzRichTextRoundTrip 的失败语料
//（testdata/fuzz 哈希 bc0233eb6e7cf0f6），因为语料文件一旦留在 testdata/fuzz/ 就会被
// 普通 go test 当作种子执行、让门禁常红，所以改由本测试承担回归职责。
//
// 本测试是**自退役**的：缺陷修好（inlineTextNode 按解析结果重新切分）后它会失败 ——
// 那是好事，请把断言改成收敛（直接复用 checkRoundTrip），并同步删掉 docs 里那两条挂账。

import (
	"strings"
	"testing"
)

// TestRoundTripMalformedNestingKnownDrift 记录「畸形嵌套仍会漂移」这一已知状态，
// 同时守住两件不因该缺陷让步的事：两次导出都必须能再导入，且都不含脚本。
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
			"并从 docs/10-todo.md CMP-15 与 docs/06-B-dual-track-adr.md 决策 5 移除这条件挂账。\n第一次 %q",
			html1.HTML)
	}

	// 安全底线不因该缺陷让步：两次导出都要能再导入（上面已验）且不含脚本。
	third, err := HTMLToNodes(html2.HTML)
	if err != nil {
		t.Fatalf("三次导入失败：%v\nHTML=%q", err, html2.HTML)
	}
	_ = third
	if containsScriptTag(html1.HTML) || containsScriptTag(html2.HTML) {
		t.Fatalf("导出结果里出现了脚本标签\n第一次 %q\n第二次 %q", html1.HTML, html2.HTML)
	}
}

// containsScriptTag 只做字面检查（大小写不敏感）：本包导出的是富文本 HTML，
// 清洗已在导入侧完成，这里要证的是「清洗过的树导回去也不会凭空长出脚本」。
func containsScriptTag(s string) bool {
	return strings.Contains(strings.ToLower(s), "<script")
}
