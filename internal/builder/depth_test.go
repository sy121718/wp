package builder

// 组件树深度上限校验测试（docs/06 §5 预设防线 / 02-A 容器结构）：
// 正常页面 3~6 层通过，超限（globalref 内联叠加、插件预设失控场景）拒绝。

import (
	"encoding/json"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

// makeDeepChain 构造 n 层嵌套的容器链（顶级节点为第 1 层）。
// props 携带合法语义标签（container 校验必填）。
func makeDeepChain(depth int) *core.Node {
	root := &core.Node{ID: "c-1", Type: "core.container", Props: containerProps()}
	cur := root
	for i := 2; i <= depth; i++ {
		child := &core.Node{ID: "c-" + itoa(i), Type: "core.container", Props: containerProps()}
		cur.Children = []*core.Node{child}
		cur = child
	}
	return root
}

// containerProps 最小合法容器 props（tag + flex 排版引擎，均校验必填）。
func containerProps() json.RawMessage {
	return json.RawMessage(`{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column","gap":"16px"}}}`)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// TestValidatePageDepthWithinLimit 正常复杂度（6 层卡片网格）与上限边界（10 层）通过。
func TestValidatePageDepthWithinLimit(t *testing.T) {
	for _, depth := range []int{3, 6, MaxNodeDepth} {
		p := &Page{
			Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull}},
			Root:     []*core.Node{makeDeepChain(depth)},
		}
		if err := ValidatePage(p); err != nil {
			// 深度合规时若失败，只可能是其他校验（本用例无 props/ID 冲突，不应发生）。
			t.Fatalf("深度 %d 应通过校验: %v", depth, err)
		}
	}
}

// TestValidatePageDepthOverLimit 超限拒绝：错误消息含深度值与上限。
func TestValidatePageDepthOverLimit(t *testing.T) {
	p := &Page{
		Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull}},
		Root:     []*core.Node{makeDeepChain(MaxNodeDepth + 1)},
	}
	err := ValidatePage(p)
	if err == nil {
		t.Fatalf("深度 %d 超过上限 %d 应拒绝", MaxNodeDepth+1, MaxNodeDepth)
	}
	if !strings.Contains(err.Error(), "组件树深度") || !strings.Contains(err.Error(), "嵌套失控") {
		t.Fatalf("错误消息应说明深度与原因: %v", err)
	}
}

// TestNodeDepth nodeDepth 递归取最长分支（子树多分支时取最大，非节点总数）。
func TestNodeDepth(t *testing.T) {
	root := &core.Node{ID: "a", Type: "core.container", Children: []*core.Node{
		{ID: "b", Type: "core.container"}, // 分支深度 2
		{ID: "c", Type: "core.container", Children: []*core.Node{
			{ID: "d", Type: "core.container"}, // 分支深度 3
		}},
	}}
	if got := nodeDepth(root); got != 3 {
		t.Fatalf("nodeDepth 应取最长分支 3, got %d", got)
	}
	if got := nodeDepth(nil); got != 1 {
		t.Fatalf("空节点深度应为 1, got %d", got)
	}
}
