// 工作台契约生成器：在仓库根目录执行 go run ./cmd/workbench-contracts。
package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"

	"go_wp/internal/builder"
)

func main() {
	check := flag.Bool("check", false, "仅检查生成文件是否与组件声明一致")
	flag.Parse()
	if err := run(*check); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(check bool) error {
	const path = "internal/templates/static/js/workbench/generated-contracts.js"
	data, err := builder.EditorContractsJS()
	if err != nil {
		return err
	}
	if check {
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(current, data) {
			return fmt.Errorf("工作台契约已过期，请在仓库根目录执行 go run ./cmd/workbench-contracts")
		}
		return nil
	}
	return os.WriteFile(path, data, 0644)
}
