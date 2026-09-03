// Package templates — 组件模板 Set（构建期组件渲染专用）。
//
// 与后台/工作台 Set 隔离：非开发模式（缓存编译后模板），
// 避免构建路径每次重新解析模板文件；注入同一套全局函数。
package templates

import (
	"github.com/CloudyKit/jet/v6"
)

// NewComponentSet 构建组件模板 Set。
//
//   - dir: 组件模板根目录（internal/templates/components）
//   - 非 dev 模式：Set 缓存编译后的模板（确定性 + 构建性能）
//   - 扩展名 .jet
func NewComponentSet(dir string) (*jet.Set, error) {
	loader := jet.NewOSFileSystemLoader(dir)
	set := jet.NewSet(
		loader,
		jet.WithTemplateNameExtensions([]string{"", ".jet"}),
	)
	injectGlobals(set)
	return set, nil
}
