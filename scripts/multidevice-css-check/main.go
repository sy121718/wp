// Command multidevice-css-check 打印多端硬规则守卫（审计 UI-015）的全量违规清单。
//
// 用法（在仓库根执行）：
//
//	go run ./scripts/multidevice-css-check            # 默认 warn：照清单，退出码 0
//	SKY_CSS_GUARD=error go run ./scripts/multidevice-css-check   # 硬门禁：有未豁免违规则退出码 1
//
// 退出码设计：CI 里这一步现在是常绿的「清单步骤」（本批不整改违规组件，见任务边界），
// 整改完成后只需把 CI 的 SKY_CSS_GUARD 改成 error，同一个脚本立刻变成硬门禁 ——
// 不需要改脚本本身，避免「验收标准写在两个地方」。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"go_wp/internal/builder"
)

func main() {
	rootFlag := flag.String("root", "", "仓库根目录（默认从当前目录向上找 go.mod）")
	flag.Parse()

	root := *rootFlag
	if root == "" {
		var err error
		root, err = repoRoot()
		if err != nil {
			fmt.Fprintln(os.Stderr, "定位仓库根失败:", err)
			os.Exit(2)
		}
	}

	report, err := builder.ScanMultiDeviceCSS(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "扫描失败:", err)
		os.Exit(2)
	}
	fmt.Print(builder.FormatCSSGuardReport(report))

	if report.Mode == builder.CSSGuardModeError && report.HasViolations() {
		fmt.Fprintf(os.Stderr, "多端硬规则守卫：%d 条未豁免违规（%s=error 时构建与本步骤都会失败）\n",
			len(report.Violations), builder.CSSGuardModeEnv)
		os.Exit(1)
	}
	fmt.Printf("\n模式 %s：只报告不拦截。整改完成后把 %s 设为 error，本步骤即成为硬门禁。\n",
		report.Mode, builder.CSSGuardModeEnv)
}

// repoRoot 从当前目录向上找 go.mod（CI 与本地都在仓库内执行）。
func repoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到 go.mod（请在仓库内运行，或用 -root 指定）")
		}
		dir = parent
	}
}
