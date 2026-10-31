package unit

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/builder"
	"go_wp/internal/templates"
)

var (
	csetOnce sync.Once
	cset     *jet.Set
	csetErr  error
)

func componentSet(t *testing.T) *jet.Set {
	t.Helper()
	csetOnce.Do(func() {
		cset, csetErr = templates.NewComponentSet("../../../../internal/templates/components")
	})
	if csetErr != nil {
		t.Fatalf("加载组件模板 Set 失败: %v", csetErr)
	}
	return cset
}

func realSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("../../../../internal/templates/static/js", name))
	if err != nil {
		t.Fatalf("读取前端源码 %s 失败: %v", name, err)
	}
	return string(b)
}

func compile(t *testing.T, p *builder.Page, opts ...builder.CompileOption) (*builder.CompiledPage, error) {
	t.Helper()
	uiSources := make(map[string]string)
	for _, file := range builder.UIAssetFiles() {
		uiSources[file] = realSource(t, "ui/"+file)
	}
	opts = append([]builder.CompileOption{
		builder.WithComponentSet(componentSet(t)),

		builder.WithEnhanceSource(realSource(t, "enhance.js")),

		builder.WithUIStyle(templates.UICSS()),
		builder.WithUISources(uiSources),
	}, opts...)
	return builder.Compile(p, opts...)
}
