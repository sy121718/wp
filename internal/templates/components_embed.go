package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"sync"

	"github.com/CloudyKit/jet/v6"
)

//go:embed components/*.jet
var componentsFS embed.FS

var (
	embeddedComponentSetOnce sync.Once
	embeddedComponentSet     *jet.Set
	embeddedComponentSetErr  error
)

func NewEmbeddedComponentSet() (*jet.Set, error) {
	embeddedComponentSetOnce.Do(func() {
		embeddedComponentSet, embeddedComponentSetErr = buildEmbeddedComponentSet()
	})
	return embeddedComponentSet, embeddedComponentSetErr
}

func buildEmbeddedComponentSet() (*jet.Set, error) {
	loader, err := newEmbeddedComponentLoader()
	if err != nil {
		return nil, err
	}
	set := jet.NewSet(
		loader,
		jet.WithTemplateNameExtensions([]string{"", ".jet"}),
	)
	injectGlobals(set)
	return set, nil
}

func newEmbeddedComponentLoader() (*embedLoader, error) {
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

	return newEmbedLoader(sub, files), nil
}
