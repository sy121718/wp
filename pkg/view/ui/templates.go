package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:templates
var templatesFS embed.FS

// TemplatesFS 返回公共 UI 模板源，逻辑键以 admin/、partials/ 开头。
//
// 应用装配时把它与业务模板合并进同一复合 FS，逻辑键不变。
func TemplatesFS() fs.FS {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		panic("pkg/view/ui: 公共模板 embed 子目录失败: " + err.Error())
	}
	return sub
}
