package templates

import (
	"os/exec"
	"testing"
)

// 接通门禁，真实运行保存时序与样式粘贴的行为测试。
func TestWorkbenchExperienceBehavior(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("缺少 Node")
	}
	output, err := exec.Command(node, "--test", "static/js/workbench/experience.test.cjs").CombinedOutput()
	if err != nil {
		t.Fatalf("工作台交互回归失败: %v\n%s", err, output)
	}
}
