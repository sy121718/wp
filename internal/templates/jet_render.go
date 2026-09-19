// Package templates 提供 Jet 模板引擎的 Gin HTMLRender 封装。
//
// 职责：
//   - 将 Jet v6 的 *jet.Set 包装为 gin render.HTMLRender 接口
//   - 开发模式下禁用模板缓存，修改模板文件即时生效
package templates

import (
	"bytes"
	"net/http"

	"go_wp/pkg/logger"

	"github.com/CloudyKit/jet/v6"
	ginrender "github.com/gin-gonic/gin/render"
)

// NewJetHTMLRender 创建 Gin HTMLRender 封装，底层使用 Jet 模板引擎。
//
// 参数：
//   - viewDir: 模板根目录的文件系统路径（相对于工作目录），**仅开发模式使用**
//   - isDev:   开发模式标记，true 时读磁盘并禁用模板缓存
//
// 模板来源按模式分流（审计 OSS-018）：开发模式读磁盘（改模板即时生效，这是开发期
// 最需要的反馈）；生产模式走 embed.FS —— 于是二进制自带后台模板，部署不再需要
// 附带 internal/templates 目录。两条路径下模板名解析结果一致（embed 的子目录
// 就是磁盘上的 admin/）。
//
// 模板文件扩展名为 .html（通过 WithTemplateNameExtensions 配置）。
func NewJetHTMLRender(viewDir string, isDev bool) ginrender.HTMLRender {
	var loader jet.Loader
	if isDev {
		loader = jet.NewOSFileSystemLoader(viewDir)
	} else if embedded, err := newAdminTemplateLoader(); err != nil {
		// embed 清单在编译期固定，这里失败即构建缺陷。回退磁盘并留明确日志，
		// 而不是让启动直接挂掉 —— 与其它资源缺失的处理口径一致。
		logger.Scene("init").Error(err, "后台模板 embed loader 构建失败，回退磁盘目录")
		loader = jet.NewOSFileSystemLoader(viewDir)
	} else {
		loader = embedded
	}
	set := jet.NewSet(
		loader,
		jet.DevelopmentMode(isDev),
		jet.WithTemplateNameExtensions([]string{"", ".html"}),
	)
	// 注入全局函数（formatNumber/formatBytes/assetURL），后台与工作台模板统一可用。
	injectGlobals(set)
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
		return i.renderError(w, err)
	}
	// 模板可能在输出部分内容后失败。先完整渲染，成功后才提交响应头与正文。
	var buf bytes.Buffer
	if err = t.Execute(&buf, nil, i.data); err != nil {
		return i.renderError(w, err)
	}
	i.WriteContentType(w)
	_, err = buf.WriteTo(w)
	return err
}

func (i *jetInstance) renderError(w http.ResponseWriter, err error) error {
	logger.Scene("template").With("template", i.name).Error(err, "页面模板渲染失败")
	http.Error(w, "页面暂时无法显示，请稍后重试", http.StatusInternalServerError)
	return err
}

// WriteContentType 设置响应头 Content-Type。
func (i *jetInstance) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}
