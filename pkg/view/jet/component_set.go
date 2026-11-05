package jet

import (
	"fmt"
	"io/fs"
	"os"

	"github.com/CloudyKit/jet/v6"
)

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
	return NewJetSetFromLoader(NewFSLoader(fsys, files), false, []string{"", ".jet"}), nil
}

// NewComponentSetWithOwned 构造组件模板 Set，并让装配方合并进程内模板源。
func NewComponentSetWithOwned(dir string, withOwned func(*FSLoader) *FSLoader) (*jet.Set, error) {
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
	loader := NewFSLoader(fsys, files)
	if withOwned != nil {
		loader = withOwned(loader)
	}
	return NewJetSetFromLoader(loader, false, []string{"", ".jet"}), nil
}
