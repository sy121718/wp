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
	default:
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Println("插件脚手架（docs/06-plugin-system.md）")
	fmt.Println("  go run ./cmd/plugin init <name>   生成插件模板工程")
}
