// Command baseline-manifest 打印基线要覆盖的两段 version。
//
// 输出即 schema/baseline/manifest.txt 的正文：先 [migrations] 段（全部注册迁移），
// 再 [seeds] 段（全部注册 seed），段内顺序即执行顺序。
// 两段都是当前注册表的完整前缀 —— 新增版本排在段尾、不在基线内，照常执行。
package main

import (
	"fmt"

	"go_wp/public/migrations"
)

func main() {
	fmt.Println("[migrations]")
	for _, m := range migrations.All() {
		fmt.Println(m.Version)
	}
	fmt.Println("[seeds]")
	for _, s := range migrations.AllSeeds() {
		fmt.Println(s.Version)
	}
}
