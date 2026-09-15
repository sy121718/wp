// Package main 插件脚手架 CLI：`go run ./cmd/plugin init <name>`。
//
// 生成标准插件模板工程（docs/06-plugin-system.md §5），与后台插件体系
// 端到端配套：模板改内容 → zip 打包 → 上传安装 → 组件进工作台组件库。
package main

import (
	"fmt"
	"os"

	"go_wp/internal/module/plugin/scaffold"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "init":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "用法：plugin init <name>（小写字母开头，如 marketing）")
			os.Exit(1)
		}
		name := os.Args[2]
		if err := scaffold.Write(name); err != nil {
			fmt.Fprintln(os.Stderr, "生成失败:", err)
			os.Exit(1)
		}
		fmt.Printf("插件模板已生成：%s/\n", name)
		fmt.Printf("改内容后打包：zip -r %s.zip %s/\n", name, name)
		fmt.Println("到后台「插件管理」上传并启用。")
	case "module":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "用法：plugin module <name>（小写字母开头，如 member）")
			os.Exit(1)
		}
		name := os.Args[2]
		if err := scaffold.WriteModule(name); err != nil {
			fmt.Fprintln(os.Stderr, "生成失败:", err)
			os.Exit(1)
		}
		fmt.Printf("业务模块骨架已生成：%s/（拷进 internal/module/ 后 go build 验证）\n", name)
		fmt.Println("装配触点见模块 README.md —— 路由挂载 / 迁移注册 / 权限点与菜单需人工接入。")
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("脚手架（docs/06-plugin-system.md）")
	fmt.Println("  go run ./cmd/plugin init <name>     生成插件模板工程（zip 上传安装）")
	fmt.Println("  go run ./cmd/plugin module <name>   生成新业务模块目录骨架（internal/module 分层）")
}
