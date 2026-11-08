package view

import (
	"sync"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/facing"
)

// RenderScope 是「模板引擎那侧的渲染作用域」，由装配方在启动期注入。
//
// 为什么是端口而不是直接 import：根包只定义「视图结果 / 出口 / 协议」，
// 具体引擎（Jet）属于它的适配层（pkg/view/jet）。根包反向 import 子包会让
// 依赖方向倒过来，也会让换引擎变成改根包。这里只持有函数值，不认引擎实现。
//
// 所有字段都可以为 nil：未装配时页面照常渲染，只是取词 / 菜单为空，
// 与 SetAdminPort 的「缺失即空、不编造」同一口径。
type RenderScope struct {
	// Attach 在请求上下文里建立取词作用域（引擎侧按请求语言隔离）。
	Attach func(c *gin.Context)
	// Inject 把请求级变量（t / tf / tn / lang）注入渲染数据。
	Inject func(c *gin.Context, data map[string]any) map[string]any
	// Translate 返回本请求的取词闭包。
	Translate func(c *gin.Context) func(key, fallback string) string
	// LangRedirect 返回带语言前缀的重定向地址（无则空串）。
	LangRedirect func(c *gin.Context) string
	// RequestURI 返回本请求的路径 + 查询串，供日志与回跳使用。
	RequestURI func(c *gin.Context) string
	// BuildFailureData 把失败事实展开成失败模板的数据。
	// 返回 any：具体结构属于引擎适配层，根包不认识它，只原样交给模板。
	BuildFailureData func(status int, fact facing.Fact, err error) any
	// FactOf 记录失败日志并返回错误事实。
	FactOf func(c *gin.Context, scene string, err error) facing.Fact
	// InternalKey / InternalFallback 是归口错误的词条与兜底。
	InternalKey      string
	InternalFallback string
	// NavItemsOf 把应用的菜单树投影成模板可递归的导航事实。
	// 返回 any 是因为具体类型属于引擎适配层，根包不持有它。
	NavItemsOf func(nodes []AdminNavNode) any
}

var (
	scopeMu   sync.RWMutex
	scopePort RenderScope
)

// SetRenderScope 在装配期登记渲染作用域实现；只允许装配一次。
func SetRenderScope(p RenderScope) {
	scopeMu.Lock()
	defer scopeMu.Unlock()
	scopePort = p
}

func currentScope() RenderScope {
	scopeMu.RLock()
	defer scopeMu.RUnlock()
	return scopePort
}

// AttachScope 建立取词作用域；未装配时什么也不做。
func AttachScope(c *gin.Context) {
	if p := currentScope(); p.Attach != nil {
		p.Attach(c)
	}
}

// InjectScope 注入请求级变量；未装配时原样返回。
func InjectScope(c *gin.Context, data map[string]any) map[string]any {
	if p := currentScope(); p.Inject != nil {
		return p.Inject(c, data)
	}
	return data
}

// ScopeTranslate 返回本请求的取词闭包；未装配时返回兜底文案。
func ScopeTranslate(c *gin.Context) func(key, fallback string) string {
	if p := currentScope(); p.Translate != nil {
		return p.Translate(c)
	}
	return func(_, fallback string) string { return fallback }
}

// ScopeLangRedirect 返回语言重定向地址；未装配时为空。
func ScopeLangRedirect(c *gin.Context) string {
	if p := currentScope(); p.LangRedirect != nil {
		return p.LangRedirect(c)
	}
	return ""
}

// ScopeRequestURI 返回请求 URI；未装配时返回路由路径。
func ScopeRequestURI(c *gin.Context) string {
	if p := currentScope(); p.RequestURI != nil {
		return p.RequestURI(c)
	}
	if c != nil && c.Request != nil && c.Request.URL != nil {
		return c.Request.URL.RequestURI()
	}
	return ""
}

// ScopeFailureData 展开失败事实；未装配时返回最小数据（只有状态码与事实）。
func ScopeFailureData(status int, fact facing.Fact, err error) any {
	if p := currentScope(); p.BuildFailureData != nil {
		return p.BuildFailureData(status, fact, err)
	}
	return struct {
		Status   int
		Fact     facing.Fact
		Fallback string
	}{Status: status, Fact: fact, Fallback: fact.Fallback}
}

// ScopeFactOf 返回错误事实；未装配时用 facing 的通用事实。
func ScopeFactOf(c *gin.Context, scene string, err error) facing.Fact {
	if p := currentScope(); p.FactOf != nil {
		return p.FactOf(c, scene, err)
	}
	if fact, ok := facing.FactOf(err); ok {
		return fact
	}
	return facing.Fact{Key: ScopeInternalKey(), Fallback: ScopeInternalFallback()}
}

// ScopeInternalKey 是归口错误词条；未装配时用共享的归口 key。
func ScopeInternalKey() string {
	if p := currentScope(); p.InternalKey != "" {
		return p.InternalKey
	}
	return "uiInternalErr"
}

// ScopeInternalFallback 是归口错误兜底文案。
func ScopeInternalFallback() string {
	if p := currentScope(); p.InternalFallback != "" {
		return p.InternalFallback
	}
	return "系统内部错误，请稍后重试"
}

// ScopeNavItems 投影菜单树；未装配时返回 nil（模板按空菜单处理）。
func ScopeNavItems(nodes []AdminNavNode) any {
	if p := currentScope(); p.NavItemsOf != nil {
		return p.NavItemsOf(nodes)
	}
	return nil
}
