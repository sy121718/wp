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
	"sync"

	"github.com/CloudyKit/jet/v6"
)

// componentsFS 组件模板 embed.FS（根为 templates 包目录）。
//
//go:embed components/*.jet
var componentsFS embed.FS

// embeddedComponentSetOnce 组件模板 Set 进程级单例。
//
// embed.FS 内容在编译期冻结，Set 的模板编译缓存使用 sync.Map（并发安全），
// Execute 从 sync.Pool 取 Runtime；因此一个 Set 可安全地被全部构建/预览
// 请求共享。此前每次预览都重建 Set（fs.Sub + ReadDir + 重新解析模板），
// 是可视化编辑器 hot path 的固定开销，这里一次构建、全局复用。
var (
	embeddedComponentSetOnce sync.Once
	embeddedComponentSet     *jet.Set
	embeddedComponentSetErr  error
)

// NewEmbeddedComponentSet 从 embed.FS 构建组件模板 Set（进程级单例缓存）。
//
//   - 不依赖进程工作目录（生产/测试任意 cwd 均正确）；
//   - 非 dev 模式：Set 缓存编译后的模板（确定性 + 构建性能）；
//   - 扩展名 .jet；
//   - 并发安全：同进程内全部调用返回同一 *jet.Set，模板在首次使用时 lazy 编译。
func NewEmbeddedComponentSet() (*jet.Set, error) {
	embeddedComponentSetOnce.Do(func() {
		embeddedComponentSet, embeddedComponentSetErr = buildEmbeddedComponentSet()
	})
	return embeddedComponentSet, embeddedComponentSetErr
}

// buildEmbeddedComponentSet 实际构建逻辑（仅首调执行一次）。
func buildEmbeddedComponentSet() (*jet.Set, error) {
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
