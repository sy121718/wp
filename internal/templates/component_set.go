package templates

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
	set := jet.NewSet(
		newEmbedLoader(fsys, files),
		jet.WithTemplateNameExtensions([]string{"", ".jet"}),
	)
	injectGlobals(set)
	return set, nil
}
