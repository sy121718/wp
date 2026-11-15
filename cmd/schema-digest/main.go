// Command schema-digest 打印 public/schema 的内容摘要，供 scripts/schema-adopt.sh 写入水位。
package main

import (
	"fmt"

	"go_wp/public/schema"
)

func main() {
	fmt.Println(schema.Digest())
	fmt.Println(schema.Version())
}
