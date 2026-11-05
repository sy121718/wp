package jet

import (
	"bytes"
	"fmt"
	"net/http"

	"go_wp/pkg/logger"

	"github.com/CloudyKit/jet/v6"
	ginrender "github.com/gin-gonic/gin/render"
)

// Source 是应用装配时提供的模板源。
//
//   - Dir 非空时为磁盘目录（开发模式），或 Release 时的回退目录；
//   - Release 时优先使用 ReleaseLoader（生产 embed 或应用装配的 FS），错误时明确记录并回退 Dir。
type Source struct {
	Dir           string
	ReleaseLoader Loader
}

// NewJetHTMLRenderWithSource 接受应用装配好的模板源。
//
// 装配方显式给出的 loader（ReleaseLoader）优先：开发期的复合 FS 与生产的 embed
// 都走它，逻辑键一致。没有 loader 时才回退磁盘目录。
func NewJetHTMLRenderWithSource(src Source, isDev bool) ginrender.HTMLRender {
	var loader jet.Loader
	switch {
	case src.ReleaseLoader != nil:
		loader = src.ReleaseLoader
	case isDev:
		loader = jet.NewOSFileSystemLoader(src.Dir)
	default:
		logger.Scene("init").Warn("release 未提供 embed loader，回退磁盘目录")
		loader = jet.NewOSFileSystemLoader(src.Dir)
	}
	set := jet.NewSet(
		loader,
		jet.DevelopmentMode(isDev),
		jet.WithTemplateNameExtensions([]string{"", ".html"}),
	)

	// 这里只装「与请求无关」的函数：金额、时间、分页窗口等纯显示格式。
	// t / tf / tn 不能装在这里 —— Jet 的 Set 在进程内被所有请求共用，
	// 挂上去的取词函数会绑死一种语言，跨请求必然串。
	injectGlobals(set)
	return &jetHTMLRender{set: set}
}

// NewJetSetFromLoader 用调用方提供的 loader 构造 Set（含纯显示全局函数）。
func NewJetSetFromLoader(loader Loader, isDev bool, exts []string) *jet.Set {
	set := jet.NewSet(
		loader,
		jet.DevelopmentMode(isDev),
		jet.WithTemplateNameExtensions(exts),
	)
	injectGlobals(set)
	return set
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

func (i *jetInstance) Render(w http.ResponseWriter) (err error) {
	// Jet 会重新抛出部分反射/运行时 panic；同样归为未写出正文的模板失败。
	defer func() {
		if recovered := recover(); recovered != nil {
			err = i.renderError(fmt.Errorf("Jet 执行异常：%v", recovered))
		}
	}()
	t, err := i.set.GetTemplate(i.name)
	if err != nil {
		return i.renderError(err)
	}

	// 取词函数只从当前请求携带的渲染上下文来，不从 Set 的全局来。
	// 载体是 ResponseWriter 的包装（见 adminui.HTMLRender / view 的接线），
	// 因为 render.Render 接口只拿得到 http.ResponseWriter。
	var vars jet.VarMap
	if carrier, ok := w.(ScopeCarrier); ok {
		if scope, ok := carrier.RenderScope(); ok {
			vars = scope.VarMap()
		}
	}

	var buf bytes.Buffer
	if err = t.Execute(&buf, vars, i.data); err != nil {
		return i.renderError(err)
	}
	i.WriteContentType(w)
	_, err = buf.WriteTo(w)
	return err
}

// renderError 不自己写响应。
//
// 这里只记录模板名与原 err 并返回可辨识的错误类型；
// 共享失败出口按路由协议渲染错误 HTML 或数据 JSON，不输出模板路径与内部原文。
//
// 不在这里 http.Error 的理由还有一条：gin 在 Render 返回 err 后会 c.Error(err) + c.Abort()，
// 若此时已经写过状态行与 text/plain 正文，出口侧就再也改不回来了。
func (i *jetInstance) renderError(err error) error {
	logger.Scene("template").With("template", i.name).Error(err, "页面模板渲染失败")
	return &TemplateRenderError{Template: i.name, Err: err}
}

func (i *jetInstance) WriteContentType(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

// NewJetHTMLRenderWithSet 用现成的 *jet.Set 构造渲染器。
//
// 存在的理由是测试：坏模板要在 Execute 期失败，而磁盘 loader 在没有文件时
// 早在 GetTemplate 就报错，那是另一条分支。测试用内存 loader 造出真正的执行期失败。
// 生产装配仍走 NewJetHTMLRender。
func NewJetHTMLRenderWithSet(set *jet.Set) ginrender.HTMLRender {
	return &jetHTMLRender{set: set}
}

// InjectGlobalsForTest 给测试用的 Set 补上与生产一致的全局显示函数
// （money / dateTime / pager 等），不含 t / tf / tn —— 那三个是按请求注入的。
func InjectGlobalsForTest(set *jet.Set) {
	if set == nil {
		return
	}
	injectGlobals(set)
}
