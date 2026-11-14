// Command baseline-manifest 打印当前注册表里的迁移 version，逐行一条。
//
// 顺序即 migrations.All() 的执行顺序（注册序列，按版本比较排序），
// 也是基线跳过判定的来源：schema/baseline/manifest.txt 由它生成。
package main

import (
	"fmt"

	"go_wp/public/migrations"
)

func main() {
	for _, m := range migrations.All() {
		fmt.Println(m.Version)
	}
}
