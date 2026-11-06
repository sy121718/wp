package editor

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed all:templates
var templatesFS embed.FS

// TemplatesFS 返回编辑器区域模板源，逻辑键为 editor/...：
// editor/workspace.html（工作区主体）、editor/inspector_panel.html、
// editor/outline_tree.html。
//
// 应用模板复合 FS 按逻辑键挂载本层；模板内部相对 include 继续按逻辑键解析。
func TemplatesFS() fs.FS {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		panic("pkg/builder/editor: 编辑器模板 embed 子目录失败: " + err.Error())
	}
	return sub
}

// TemplateFile 按逻辑键读取一个编辑器模板源。
func TemplateFile(name string) ([]byte, error) {
	b, err := fs.ReadFile(TemplatesFS(), name)
	if err != nil {
		return nil, fmt.Errorf("读取编辑器模板 %s: %w", name, err)
	}
	return b, nil
}
