package templates

import (
	"embed"
	"fmt"
	"path"
)

//go:embed static/js/*.js static/js/ui/*.js
var staticJSFS embed.FS

//go:embed static/css/ui.css
var uiCSSSource string

func UICSS() string { return uiCSSSource }

func StaticJS(name string) (string, error) {
	b, err := staticJSFS.ReadFile(path.Join("static/js", name))
	if err != nil {
		return "", fmt.Errorf("读取静态脚本 %s: %w", name, err)
	}
	return string(b), nil
}
