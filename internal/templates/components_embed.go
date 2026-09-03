// Package templates — 组件模板 embed 加载（构建期确定性，不依赖进程工作目录）。
//
// 生产构建（page service / dashboard / pipeline）可能从不同工作目录启动
// （如 feature 测试从 public/test/page/feature 启动），相对路径会失效。
// 组件模板经 go:embed 打进二进制，NewEmbeddedComponentSet 从 embed.FS 构建 Set，
// 任何工作目录下都稳定可用（符合确定性构建约束）。
package templates

import (
	"embed"
	"fmt"
	"io/fs"

	"github.com/CloudyKit/jet/v6"
)

// componentsFS 组件模板 embed.FS（根为 templates 包目录）。
//
//go:embed components/*.jet
var componentsFS embed.FS

// NewEmbeddedComponentSet 从 embed.FS 构建组件模板 Set。
//
//   - 不依赖进程工作目录（生产/测试任意 cwd 均正确）；
//   - 非 dev 模式：Set 缓存编译后的模板（确定性 + 构建性能）；
//   - 扩展名 .jet。
func NewEmbeddedComponentSet() (*jet.Set, error) {
	sub, err := fs.Sub(componentsFS, "components")
	if err != nil {
		return nil, fmt.Errorf("组件模板 embed 子目录失败: %w", err)
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, fmt.Errorf("读取组件模板目录失败: %w", err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	set := jet.NewSet(
		newEmbedLoader(sub, files),
		jet.WithTemplateNameExtensions([]string{"", ".jet"}),
	)
	injectGlobals(set)
	return set, nil
}
