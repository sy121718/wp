// Package templates 提供 Jet 模板引擎的 Gin HTMLRender 封装。
//
// 职责：
//   - 将 Jet v6 的 *jet.Set 包装为 gin render.HTMLRender 接口
//   - 开发模式下禁用模板缓存，修改模板文件即时生效
package templates

import (
	"net/http"

	"github.com/CloudyKit/jet/v6"
	ginrender "github.com/gin-gonic/gin/render"
)

// NewJetHTMLRender 创建 Gin HTMLRender 封装，底层使用 Jet 模板引擎。
//
// 参数：
//   - viewDir: 模板根目录的文件系统路径（相对于工作目录）
//   - isDev:   开发模式标记，true 时禁用模板缓存
//
// 模板文件扩展名为 .html（通过 WithTemplateNameExtensions 配置）。
func NewJetHTMLRender(viewDir string, isDev bool) ginrender.HTMLRender {
	loader := jet.NewOSFileSystemLoader(viewDir)
	set := jet.NewSet(
		loader,
		jet.DevelopmentMode(isDev),
		jet.WithTemplateNameExtensions([]string{"", ".html"}),
	)
	return &jetHTMLRender{set: set}
}

// jetHTMLRender 实现 gin render.HTMLRender 接口。
type jetHTMLRender struct {
	set *jet.Set
}

// Instance 为每次渲染创建一个独立的渲染实例。
// name 是相对于模板根目录的路径（如 "dashboard.jet"）。
func (r *jetHTMLRender) Instance(name string, data any) ginrender.Render {
	return &jetInstance{
		set:  r.set,
		name: name,
		data: data,
	}
}

// jetInstance 实现 gin render.Render 接口，负责单个模板的渲染。
type jetInstance struct {
	set  *jet.Set
	name string
	data any
}

// Render 执行 Jet 模板渲染并写入 HTTP 响应。
func (i *jetInstance) Render(w http.ResponseWriter) error {
	t, err := i.set.GetTemplate(i.name)
	if err != nil {
		return err
	}
	return t.Execute(w, nil, i.data)
}

// WriteContentType 设置响应头 Content-Type。
func (i *jetInstance) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}