package architecture

import (
	"os/exec"
	"strings"
	"testing"
)

// TestContractPackagesDoNotReachBuilderCore 校验契约包的**传递依赖**不含构建器内核。
//
// 为什么不是只看直接 import：internal/module/content/contract 并不直接 import
// builder/core，它是经 content/dto 把 core 拖进来的 —— 后者曾在 DTO 里调用
// core.SanitizeRichHTML 做正文清洗。直接 import 检查对此完全失明。
//
// scripts/check-contract-deps.sh 是同一判据的脚本版（可在 CI 单独跑）；
// 这里用 go list -deps 给它一条仓库内可执行的回归，防止「清洗函数又搬回 DTO」。
func TestContractPackagesDoNotReachBuilderCore(t *testing.T) {
	root := repoRoot(t)
	cmd := exec.Command("go", "list", "-deps", "./internal/module/content/contract")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		// 编译不过时依赖图不可信：明确失败，不降级成「没命中」。
		t.Fatalf("go list -deps 失败（契约包编译不过，依赖方向无法判定）：%v", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch strings.TrimSpace(line) {
		case "go_wp/pkg/builder/core", "go_wp/pkg/builder/style", "go_wp/pkg/builder/plugincomp":
			t.Fatalf("契约包不得到达构建器内核：%s —— 正文清洗等中立能力归 pkg/richtext，不放在 DTO", line)
		}
	}
}
