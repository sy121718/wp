package templates

import (
	"bytes"
	"net/http"

	"go_wp/pkg/logger"

	"github.com/CloudyKit/jet/v6"
	ginrender "github.com/gin-gonic/gin/render"
)

func NewJetHTMLRender(viewDir string, isDev bool) ginrender.HTMLRender {
	var loader jet.Loader
	if isDev {
		loader = jet.NewOSFileSystemLoader(viewDir)
	} else if embedded, err := newAdminTemplateLoader(); err != nil {

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

	injectGlobals(set)
	return &jetHTMLRender{set: set}
}

type jetHTMLRender struct {
	set *jet.Set
}

func (r *jetHTMLRender) Instance(name string, data any) ginrender.Render {
	return &jetInstance{
		set:  r.set,
		name: name,
		data: data,
	}
}

type jetInstance struct {
	set  *jet.Set
	name string
	data any
}

func (i *jetInstance) Render(w http.ResponseWriter) error {
	t, err := i.set.GetTemplate(i.name)
	if err != nil {
		return i.renderError(w, err)
	}

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

func (i *jetInstance) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}
