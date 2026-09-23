package productlist

import (
	"testing"

	"go_wp/internal/builder/core"
)

// TestInspectorExposesTreeControls 检查器里能看到分类树的三个新开关。
func TestInspectorExposesTreeControls(t *testing.T) {
	props := (&Component{}).PropsSpec()
	controls, err := core.ParseControls(props)
	if err != nil {
		t.Fatalf("解析控件失败: %v", err)
	}
	byKey := map[string]core.Control{}
	for _, c := range controls {
		byKey[c.Key] = c
	}
	for _, key := range []string{"categoryMulti", "caretIcon"} {
		c, ok := byKey[key]
		if !ok {
			t.Fatalf("检查器缺少 %s 控件", key)
		}
		if c.Kind != core.ControlSelect {
			t.Fatalf("%s 应为下拉控件，实际 %s", key, c.Kind)
		}
		if len(c.Options) == 0 {
			t.Fatalf("%s 应有可选值", key)
		}
	}
}
