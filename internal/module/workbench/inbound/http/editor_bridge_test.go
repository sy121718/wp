package workbenchhttp

import (
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// TestEditorBridgeStripsNodeClassPrefixByLength 钉住「节点类名 → data-sky-id」的还原口径。
//
// 回归背景（实测）：桥接脚本里写的是 cls.slice(5)，而 core.NodeClass 的前缀
// 'sky-c-' 是 6 个字符。于是每个节点的 data-sky-id 都多出一个前导 "-"：
//
//	编译产物 class = "sky-c-about-h"  →  桥接还原出 data-sky-id = "-about-h"
//	AST 里的节点 id 是 "about-h"
//
// 父窗口 findNode("-about-h") 恒返回 null，画布发回的**每一条**消息都落空 ——
// 双击能进编辑态、失焦后回写被静默丢弃（文字弹回），点选、右键、拖放重排、
// 就地插入同样全部无反应。判据只能是「长度」而不是「看起来对」。
func TestEditorBridgeStripsNodeClassPrefixByLength(t *testing.T) {
	prefix := strings.TrimSuffix(core.NodeClass("x"), "x")
	if prefix != "sky-c-" {
		t.Fatalf("NodeClass 前缀已变为 %q：桥接脚本的还原逻辑必须同步", prefix)
	}
	if got := len(prefix); got != 6 {
		t.Fatalf("前缀长度 %d（预期 6）：写死数字的还原逻辑会重新踩坑", got)
	}
	if strings.Contains(editorBridgeScript, "cls.slice(5)") {
		t.Fatal("editorBridgeScript 里仍有写死的 cls.slice(5)：'sky-c-' 是 6 个字符，会还原出带前导 '-' 的 id")
	}
	if !strings.Contains(editorBridgeScript, "cls.slice(WB_SKY_PREFIX.length)") {
		t.Fatal("editorBridgeScript 未按 WB_SKY_PREFIX.length 截断节点类名")
	}
	if !strings.Contains(editorBridgeScript, "var WB_SKY_PREFIX = 'sky-c-'") {
		t.Fatal("editorBridgeScript 未声明 WB_SKY_PREFIX（前缀真源）")
	}
}
