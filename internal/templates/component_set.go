package templates

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/CloudyKit/jet/v6"
)

// NewComponentSet 从**目录**构建组件模板 Set（开发与测试用：改模板即时生效，无需重编译）。
//
// 这里刻意复用 embedLoader（而不是 jet 自带的 NewOSFileSystemLoader）：
// 组件自带的模板（组件目录里 //go:embed、经 core.RegisterTemplate 注册）由 embedLoader 统一合并，
// 两套 loader 并存会让「从目录加载」的路径拿不到那些模板 —— 表现为构建期 template not found，
// 而且只有在跑对应测试时才暴露。一个能力只该有一条实现。
func NewComponentSet(dir string) (*jet.Set, error) {
	fsys := os.DirFS(dir)
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("读取组件模板目录 %s 失败: %w", dir, err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	set := jet.NewSet(
		newEmbedLoader(fsys, files),
		jet.WithTemplateNameExtensions([]string{"", ".jet"}),
	)
	injectGlobals(set)
	return set, nil
}
