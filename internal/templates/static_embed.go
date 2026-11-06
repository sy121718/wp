package templates

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sync"

	"go_wp/pkg/builder/editor"
	uipkg "go_wp/pkg/view/ui"
)

//go:embed all:static
var staticAssetsFS embed.FS

var (
	staticCompositeOnce sync.Once
	staticComposite     *uipkg.CompositeFS
	staticCompositeErr  error
	staticVersionOnce   sync.Once
	staticVersionValue  string
)

// StaticAssets 返回唯一公共 UI 资产源：pkg 公共资产 + 应用专用资产。
//
// 两层按逻辑键合成一个 FS：公共控件/主题在 pkg，业务专用脚本（工作台、自动化、
// 富文本等）在应用；同名在构造时报错，不做逐层 fallback 覆盖。
// 运行时 /static 与构建期内联都从这一份读。
func StaticAssets() (*uipkg.CompositeFS, error) {
	staticCompositeOnce.Do(func() {
		app, err := fs.Sub(staticAssetsFS, "static")
		if err != nil {
			staticCompositeErr = fmt.Errorf("应用静态资源 embed 子目录失败: %w", err)
			return
		}
		staticComposite, staticCompositeErr = uipkg.NewCompositeFS(
			uipkg.Layer{Name: "pkg/view/ui", FS: uipkg.AssetsFS()},
			uipkg.Layer{Name: "pkg/builder/editor", FS: editor.AssetsFS()},
			uipkg.Layer{Name: "internal/templates", FS: app},
		)
	})
	return staticComposite, staticCompositeErr
}

// UICSS 返回公共控件基座样式，供构建期内联。
func UICSS() string {
	b, err := AssetFile("css/ui.css")
	if err != nil {
		panic(err)
	}
	return string(b)
}

// AssetFile 按逻辑键读一个静态资产（pkg 公共 + 应用专用）。
func AssetFile(name string) ([]byte, error) {
	assets, err := StaticAssets()
	if err != nil {
		return nil, err
	}
	b, err := fs.ReadFile(assets, path.Clean(name))
	if err != nil {
		return nil, fmt.Errorf("读取静态资产 %s: %w", name, err)
	}
	return b, nil
}

// StaticJSVersion 返回最终复合资产（公共 UI + 编辑器工具 + 应用宿主适配）的内容哈希，
// 不读私有磁盘 mtime；结果在进程内缓存一次。
func StaticJSVersion() string {
	staticVersionOnce.Do(func() {
		assets, err := StaticAssets()
		if err != nil {
			staticVersionValue = "0"
			return
		}
		version, err := editor.Version(assets)
		if err != nil || version == "" {
			staticVersionValue = "0"
			return
		}
		staticVersionValue = version
	})
	return staticVersionValue
}

// StaticJS 保留既有签名，按逻辑键读 JS（含子目录，如 ui/select.js）。
func StaticJS(name string) (string, error) {
	b, err := AssetFile("js/" + name)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
