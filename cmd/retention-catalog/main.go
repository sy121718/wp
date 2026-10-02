// Package main retention 声明表的运维读出口（审计 IDX-019）。
//
// 用法：
//
//	go run ./cmd/retention-catalog
//
// 只读、不需要数据库与配置：声明是静态数据（真源 internal/retention/catalog.go），
// 这张表回答的是「哪张增长表留多久、按哪一列清、谁在清、为什么」——
// 也就是包注释里那句「运维手册与后台展示都从这里取」的兑现处。
package main

import (
	"fmt"

	"go_wp/internal/retention"
)

func main() {
	fmt.Print(retention.FormatDeclarations())
}
