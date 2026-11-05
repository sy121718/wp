package templates

import (
	jetengine "github.com/CloudyKit/jet/v6"
	ginrender "github.com/gin-gonic/gin/render"

	"go_wp/pkg/view/jet"
)

// 适配层：应用装配的模板源与逻辑键留在 internal/templates，
// 引擎、作用域、显示函数与失败接线在 pkg/view/jet。

type RenderScope = jet.RenderScope

type ScopeCarrier = jet.ScopeCarrier

type ScopeWriter = jet.ScopeWriter

type TemplateRenderError = jet.TemplateRenderError

// NewJetHTMLRender 用应用装配的模板源构造渲染器。
//
// 开发模式走磁盘目录；release 使用生产 embed loader（缺依赖时明确报错，不静默空渲染）。
func NewJetHTMLRender(viewDir string, isDev bool) ginrender.HTMLRender {
	if !isDev {
		loader, err := newAdminTemplateLoader()
		if err == nil {
			return jet.NewJetHTMLRenderWithSource(jet.Source{Dir: viewDir, ReleaseLoader: loader}, false)
		}
	}
	// 开发期同样经过复合 FS：公共 UI 在 pkg/view/ui，业务模板在磁盘目录，
	// 逻辑键保持一致，不靠目录镜像。
	loader, err := newDevTemplateLoader(viewDir)
	if err != nil {
		return jet.NewJetHTMLRenderWithSource(jet.Source{Dir: viewDir}, true)
	}
	return jet.NewJetHTMLRenderWithSource(jet.Source{Dir: viewDir, ReleaseLoader: loader}, true)
}

var (
	WithRenderScope            = jet.WithRenderScope
	FailedRenderMiddleware     = jet.FailedRenderMiddleware
	AsTemplateRenderError      = jet.AsTemplateRenderError
	NewJetHTMLRenderWithSource = jet.NewJetHTMLRenderWithSource
	NewJetHTMLRenderWithSet    = jet.NewJetHTMLRenderWithSet
	InjectGlobalsForTest       = jet.InjectGlobalsForTest
	TranslateFunc              = jet.TranslateFunc
	LanguageOptions            = jet.LanguageOptions
	PagerOf                    = jet.PagerOf
)

type LanguageOption = jet.LanguageOption

type Source = jet.Source

// 测试与同包辅助需要的内部钩子：把 pkg 层实现按原名称暴露给 internal/templates 的测试。
func injectGlobals(set *jetengine.Set) { jet.InjectGlobalsForTest(set) }

func stringify(v any) string { return jet.Stringify(v) }

func formatTime(raw any) string { return jet.FormatTime(raw) }

func isNil(v any) bool { return jet.IsNil(v) }

type Pager = jet.Pager
