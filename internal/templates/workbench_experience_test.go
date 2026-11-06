package templates

import (
	"os/exec"
	"testing"
)

func TestWorkbenchExperienceBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("缺少 Node")
	}
	// 交互回归随编辑器工具资产走：脚本直接读 pkg/builder/editor 的源文件。
	output, err := exec.Command(node, "--test", "../../pkg/builder/editor/assets/js/editor/editor-catalog.test.cjs",
		"../../pkg/builder/editor/assets/js/editor/editor-isolation.test.cjs",
		"../../pkg/builder/editor/assets/js/editor/editor-host.test.cjs",
		"../../pkg/builder/editor/assets/js/editor/editor-state.test.cjs").CombinedOutput()
	if err != nil {
		t.Fatalf("工作台交互回归失败: %v\n%s", err, output)
	}
}
