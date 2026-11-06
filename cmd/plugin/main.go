package main

import (
	"fmt"
	"io"
	"os"

	"go_wp/pkg/scaffold"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run 执行插件 / 模块脚手架子命令，返回进程退出码。
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 {
		usage(stdout)
		return 1
	}
	switch args[0] {
	case "init":
		// 保持插件 init 原有行为：只要求至少一个 name，多余参数忽略。
		if len(args) < 2 {
			fmt.Fprintln(stderr, "用法：plugin init <name>（小写字母开头，如 marketing）")
			return 1
		}
		name := args[1]
		if err := scaffold.Write(name); err != nil {
			fmt.Fprintln(stderr, "生成失败:", err)
			return 1
		}
		fmt.Fprintf(stdout, "插件模板已生成：%s/\n", name)
		fmt.Fprintf(stdout, "改内容后打包：zip -r %s.zip %s/\n", name, name)
		fmt.Fprintln(stdout, "到后台「插件管理」上传并启用。")
		return 0
	case "module":
		if len(args) != 3 {
			fmt.Fprintln(stderr, "用法：plugin module <name> <module-path>（如 member example.org/team/cms/v2）")
			return 1
		}
		name, modulePath := args[1], args[2]
		if err := scaffold.WriteModule(name, modulePath); err != nil {
			fmt.Fprintln(stderr, "生成失败:", err)
			return 1
		}
		fmt.Fprintf(stdout, "业务模块骨架已生成：%s/（导入按根模块路径 %s 书写）\n", name, modulePath)
		fmt.Fprintln(stdout, "装配触点见模块 README.md —— 路由挂载 / 迁移注册 / 权限点与菜单需人工接入。")
		return 0
	default:
		usage(stdout)
		return 1
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "脚手架（AGENTS.md）")
	fmt.Fprintln(w, "  go run ./cmd/plugin init <name>     生成插件模板工程（zip 上传安装）")
	fmt.Fprintln(w, "  go run ./cmd/plugin module <name> <module-path>   生成新业务模块目录骨架（导入按实际根模块路径）")
}
