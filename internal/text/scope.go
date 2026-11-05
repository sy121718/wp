package text

import (
	"fmt"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/i18n"
	"go_wp/pkg/response"
	jet "go_wp/pkg/view/jet"
)

// Scope 按请求组装一次模板取词上下文。
//
// 这是「语言 → 取词函数」唯一的组装点：函数表本身归 internal/templates
// （共享渲染基础设施），语言来源归 response.RequestLanguage（cookie > query > Accept-Language）。
// 页面数据 map 里的 t/tf/tn（Inject）和渲染器执行期的 VarMap（RenderScope）
// 都从这里取同一份函数，避免两处各写一遍、接出来的句子不一致。
//
// 每次调用都返回新闭包，闭包里捕获的是本次请求的 lang 与 gin.Context：
// 它不写进任何进程级容器，所以并发请求之间不会互相改语言。
func Scope(c *gin.Context) jet.RenderScope {
	lang := Lang(c)
	t := jet.TranslateFunc(lang)

	tf := func(key, fallback string, args []string) string {
		return response.ArgsText(c, key, fallback, args)
	}

	tn := func(key, fallback string, args map[string]any) string {
		params := make(map[string]string, len(args))
		for k, v := range args {
			params[k] = fmt.Sprint(v)
		}
		return i18n.FillTranslate(t, key, fallback, params)
	}

	return jet.RenderScope{
		Lang:           lang,
		Translate:      t,
		TranslateArgs:  tf,
		TranslateNamed: tn,
	}
}

// AttachScope 把本次请求的取词上下文挂到 ResponseWriter 上，
// 供渲染器在 Execute 时作为 VarMap 读取（见 jet.ScopeWriter）。
//
// 它装配在模板渲染中间件里，因此对「控制器直接 c.HTML」与
// 「pkg/view、internal/adminui 渲染」两条合法出口一视同仁。
func AttachScope(c *gin.Context) {
	if c == nil {
		return
	}
	jet.WithRenderScope(c, Scope(c))
}
