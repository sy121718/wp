package templates

// static_embed.go — 前端静态资产的构建期出口。
//
// 背景：前端资产（JS/CSS）此前散落两处 —— internal/builder/enhance.js（构建期内联进产物）
// 与 internal/templates/static/**（运行时经 /static 提供）。同一个能力容易各写一份
//（原生 select 的替身就重复了两次）。
//
// 现在统一放 internal/templates/static/，一份源文件两个出口：
//   - 运行时：/static（gin.Dir）给后台页面 <script src> 用；
//   - 构建期：本文件 embed 同一份，供 builder 内联进静态产物（调用方注入）。
//
// 为什么要 embed 而不是构建期读磁盘：页面编译发生在运行时，而生产部署可能只带二进制，
// 磁盘上不一定有源码树 —— embed 保证「构建不依赖文件路径」（与组件模板同一条原则）。

import (
	"embed"
	"fmt"
)

//go:embed static/js/*.js
var staticJSFS embed.FS

// StaticJS 取 static/js 下的脚本源码（name 形如 "enhance.js"）。
// 构建期由调用方（page service）注入给 builder；文件缺失返回错误，由调用方决定是否降级。
func StaticJS(name string) (string, error) {
	b, err := staticJSFS.ReadFile("static/js/" + name)
	if err != nil {
		return "", fmt.Errorf("读取静态脚本 %s: %w", name, err)
	}
	return string(b), nil
}
