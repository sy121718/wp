package unit

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/CloudyKit/jet/v6"

	"go_wp/internal/templates"
	"go_wp/pkg/builder"
	uiassets "go_wp/pkg/view/ui"
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

// realSource 读取真实的前端源码。
//
// 公共 UI 资产（ui/…）的属主已迁到 pkg/view/ui/assets（唯一源，/static 与构建期内联
// 共用这一份）；其余脚本仍在应用的 static/js 下。按属主分开读，避免复制副本。
func realSource(t *testing.T, name string) string {
	t.Helper()
	if strings.HasPrefix(name, "ui/") {
		// 逻辑键与 /static URL 一致：ui/x.js 在资产树里是 js/ui/x.js。
		b, err := uiassets.AssetFile("js/" + name)
		if err != nil {
			t.Fatalf("读取公共 UI 资产 %s 失败: %v", name, err)
		}
		return string(b)
	}
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
