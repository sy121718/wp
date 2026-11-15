// Command baseline-digest 打印 schema/baseline 产物的内容摘要。
//
// 与迁移器的跳过判定同源（baseline.ContentSHA256），供 scripts/baseline-adopt.sh
// 写水位时使用 —— 接入脚本若自己算 manifest 哈希，口径就会与迁移器漂移。
package main

import (
	"fmt"

	baseline "go_wp/schema/baseline"
)

func main() {
	fmt.Println(baseline.ContentSHA256())
}
