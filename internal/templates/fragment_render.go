// Package templates — fragment 模板渲染（运行时动态片段 / 导航片段）。
//
// 与组件模板（components_embed.go）同构：go:embed 打进二进制，构建期/运行时
// 不依赖进程工作目录；进程级单例 Set，并发安全。供 runtimefragment capability
// 与 navigation Render 用 Jet 渲染 HTML 片段，替代手工字符串拼接。
package templates

import (
	"bytes"
	"embed"
	"io/fs"
	"sync"

	"github.com/CloudyKit/jet/v6"
)

// fragmentsFS fragment 模板 embed.FS（根为 templates 包目录）。
//
//go:embed fragments/*.jet
var fragmentsFS embed.FS

var (
	fragmentSetOnce sync.Once
	fragmentSet     *jet.Set
	fragmentSetErr  error
)

// fragmentTemplateSet 构建 fragment 模板 Set（进程级单例缓存）。
func fragmentTemplateSet() (*jet.Set, error) {
	fragmentSetOnce.Do(func() {
		loader, err := newFragmentLoader()
		if err != nil {
			fragmentSetErr = err
			return
		}
		set := jet.NewSet(
			loader,
			jet.WithTemplateNameExtensions([]string{"", ".jet"}),
		)
		injectGlobals(set)
		fragmentSet = set
	})
	return fragmentSet, fragmentSetErr
}

// newFragmentLoader fragment 模板的 embed loader。
func newFragmentLoader() (*embedLoader, error) {
	sub, err := fs.Sub(fragmentsFS, "fragments")
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(sub, ".")
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}
	return newEmbedLoader(sub, files), nil
}

// RenderFragment 渲染 fragment 模板为 HTML 字符串。
// name 为模板名（不含 .jet 扩展名），data 为模板数据（模板内 {{ .Field }} 访问，
// 默认 HTML 转义，等价 html.EscapeString）。
func RenderFragment(name string, data any) (string, error) {
	set, err := fragmentTemplateSet()
	if err != nil {
		return "", err
	}
	t, err := set.GetTemplate(name)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err = t.Execute(&buf, nil, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}
