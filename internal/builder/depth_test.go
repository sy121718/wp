package builder

import (
	json "go_wp/pkg/jsonx"
	"strings"
	"testing"

	"go_wp/internal/builder/core"
)

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

func containerProps() json.RawMessage {
	return json.RawMessage(`{"tag":"section","layout":{"engine":"flex","flex":{"direction":"column","gap":"16px"}}}`)
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

func TestValidatePageDepthWithinLimit(t *testing.T) {
	for _, depth := range []int{3, 6, MaxNodeDepth} {
		p := &Page{
			Settings: PageSettings{Layout: PageLayout{Mode: LayoutFull}},
			Root:     []*core.Node{makeDeepChain(depth)},
		}
		if err := ValidatePage(p); err != nil {

			t.Fatalf("深度 %d 应通过校验: %v", depth, err)
		}
	}
}

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

func TestNodeDepth(t *testing.T) {
	root := &core.Node{ID: "a", Type: "core.container", Children: []*core.Node{
		{ID: "b", Type: "core.container"},
		{ID: "c", Type: "core.container", Children: []*core.Node{
			{ID: "d", Type: "core.container"},
		}},
	}}
	if got := nodeDepth(root); got != 3 {
		t.Fatalf("nodeDepth 应取最长分支 3, got %d", got)
	}
	if got := nodeDepth(nil); got != 1 {
		t.Fatalf("空节点深度应为 1, got %d", got)
	}
}
