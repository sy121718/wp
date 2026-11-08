package view

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"go_wp/pkg/bulkids"
	"go_wp/pkg/facing"
	"go_wp/pkg/logger"
	"go_wp/pkg/pagination"
	"go_wp/pkg/reqparams"
	"go_wp/pkg/response"
	jetpkg "go_wp/pkg/view/jet"
)

// 后台页面渲染的公共事实在这里装配一次；应用只在装配期把请求事实的来源注入。
//
// 这里不读库、不取词、不派生展示状态：它只把中间件已经算好的权限 / 菜单 /
// CSRF 事实原样放进渲染数据。此前这些实现放在 internal/adminui，pkg 无法引用，
// 页面渲染因此分裂成两套入口；合并到这里后只有一套。

const (
	// PermSetKey / ButtonsKey / NavTreeKey 是应用中间件写入请求上下文的键名。
	// 值必须与中间件一致，否则页面权限集会恒为空 map（静默失效）。
	PermSetKey = "perm_set"
	ButtonsKey = "button_set"
	NavTreeKey = "nav_tree"

	SidebarCookieOpen   = "sidebar_open"
	SidebarCookiePinned = "sidebar_pinned"
)

// AdminNavNode 是导航原始事实：只带渲染所需字段，不做激活 / 展开 / 首链接派生。
type AdminNavNode struct {
	ID       uint64
	Title    string
	TitleKey string
	Path     string
	Icon     string
	Hidden   bool
	Children []AdminNavNode
}

// AdminPort 是应用在装配期注入的请求事实来源。
//
// 每个字段只回答一件事；缺失时页面照常渲染，只是对应的事实为空
// （权限按钮不可见、菜单为空），不会编造或回落到放行。
type AdminPort struct {
	// CSRFToken 返回本请求可用的 CSRF token；失败返回空串。
	CSRFToken func(c *gin.Context) string
	// NavTree 返回本次请求已授权的菜单原始树。
	NavTree func(c *gin.Context) []AdminNavNode
}

var adminPort AdminPort

// SetAdminPort 在路由装配处登记页面的请求事实来源。只允许装配一次。
func SetAdminPort(p AdminPort) { adminPort = p }

// CSRF 返回本请求的 CSRF token；未装配端口时返回空串（模板输出空值，不报错）。
func CSRF(c *gin.Context) string {
	if adminPort.CSRFToken == nil {
		return ""
	}
	return adminPort.CSRFToken(c)
}

// Facts 是「先算数据、再渲染」的处理器需要的那几项请求事实。
type Facts struct {
	CSRFToken    string
	T            func(key, fallback string) string
	PermSet      map[string]bool
	Buttons      map[string]bool
	LangRedirect string
	RequestPath  string
}

// PageFacts 返回本次请求的取词函数、CSRF 与权限集合。
func PageFacts(c *gin.Context) Facts {
	f := Facts{
		T:            jetpkg.RequestTranslateFunc(c),
		CSRFToken:    CSRF(c),
		PermSet:      map[string]bool{},
		Buttons:      map[string]bool{},
		LangRedirect: jetpkg.LangRedirect(c),
		RequestPath:  jetpkg.RequestURI(c),
	}
	if v, ok := c.Get(PermSetKey); ok {
		if m, ok := v.(map[string]bool); ok {
			f.PermSet = m
		}
	}
	if v, ok := c.Get(ButtonsKey); ok {
		if m, ok := v.(map[string]bool); ok {
			f.Buttons = m
		}
	}
	return f
}

// PreparePage 把请求级公共事实注入渲染数据（整页与片段共用同一份事实）。
func PreparePage(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}
	data = jetpkg.Inject(c, data)
	data["csrf_token"] = CSRF(c)

	set := map[string]bool{}
	if v, ok := c.Get(PermSetKey); ok {
		if m, ok := v.(map[string]bool); ok {
			set = m
		}
	}
	data["PermSet"] = set

	buttons := map[string]bool{}
	if v, ok := c.Get(ButtonsKey); ok {
		if m, ok := v.(map[string]bool); ok {
			buttons = m
		}
	}
	data["Buttons"] = buttons

	navItems := NavItemsOf(adminNavTree(c))
	data["NavItems"] = navItems
	if c.Request != nil && c.Request.URL != nil {
		data["NavCurrentPath"] = c.Request.URL.Path
	} else {
		data["NavCurrentPath"] = ""
	}

	data["SidebarOpen"] = sidebarOpen(c)
	data["SidebarPinned"] = sidebarPinned(c)
	return data
}

// PrepareBare 只注入取词作用域（不带权限、菜单与侧栏事实），供独立片段使用。
func PrepareBare(c *gin.Context, data gin.H) gin.H {
	return jetpkg.Inject(c, data)
}

// ViewMap 把视图对象投影成渲染数据。
func ViewMap(v any) gin.H {
	return gin.H(MapOf(v))
}

// RenderPage 是整页渲染的统一入口：视图结果的状态与 no-store 语义在这里生效。
func RenderPage(c *gin.Context, tpl string, data any) {
	status := http.StatusOK
	if r, ok := data.(Result); ok {
		if s := r.PageStatus(); s > 0 {
			status = s
		}
		if r.PageNoStore() {
			c.Header("Cache-Control", "no-store")
		}
	}
	c.HTML(status, tpl, prepareView(c, data))
}

// RenderBare 渲染不带页面壳的独立视图。
func RenderBare(c *gin.Context, tpl string, data any) {
	c.HTML(http.StatusOK, tpl, PrepareBare(c, ViewMap(data)))
}

// RenderFragment 是 HTML 片段的唯一渲染入口：取词、CSRF 与权限事实统一注入。
//
// 片段与整页共用同一份请求事实：模板里的 .csrf_token / .PermSet / .Buttons
// 在两种出口下都必须存在，否则片段会静默渲染成空。
func RenderFragment(c *gin.Context, tpl string, data any) {
	c.HTML(http.StatusOK, tpl, prepareView(c, data))
}

// RenderFailure 只传失败事实给 Jet；错误模板不依赖登录态、菜单或业务查询。
func RenderFailure(c *gin.Context, status int, scene string, err error) {
	if c.Writer.Written() {
		return
	}
	jetpkg.AttachScope(c)
	data := jetpkg.BuildFailureData(status, jetpkg.FactOf(c, scene, err), err)
	c.Header("Cache-Control", "no-store")
	tpl := "admin/errors/page.html"
	if IsFragment(c) {
		tpl = "admin/errors/failure.html"
	}
	c.HTML(status, tpl, data)
	if !c.Writer.Written() {
		// 错误模板自身失败：不递归、不改回 JSON；浏览器基座负责空响应的通用反馈。
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusInternalServerError)
		c.Writer.WriteHeaderNow()
	}
}

func prepareView(c *gin.Context, data any) gin.H {
	out := PreparePage(c, ViewMap(data))
	AttachPagination(out, data)
	return out
}

func adminNavTree(c *gin.Context) []AdminNavNode {
	if adminPort.NavTree != nil {
		return adminPort.NavTree(c)
	}
	if v, ok := c.Get(NavTreeKey); ok {
		if tree, ok := v.([]AdminNavNode); ok {
			return tree
		}
	}
	return nil
}

// AdminNavNodeFrom 是应用把自有菜单树转成渲染事实的最小入口。
//
// 它让「没有装配 AdminPort 的测试与轻量装配」也能直接交出导航事实；
// 转换本身不读库、不取词，只做字段搬移。
func AdminNavNodeFrom(id uint64, title, titleKey, path, icon string, hidden bool, children []AdminNavNode) AdminNavNode {
	return AdminNavNode{
		ID: id, Title: title, TitleKey: titleKey, Path: path, Icon: icon,
		Hidden: hidden, Children: children,
	}
}

// NavItemsOf 把原始菜单树投影成导航事实（不做激活 / 展开 / 首链接派生）。
func NavItemsOf(nodes []AdminNavNode) []jetpkg.NavItem {
	out := make([]jetpkg.NavItem, 0, len(nodes))
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		item := jetpkg.NavItem{
			ID: n.ID, Title: n.Title, TitleKey: n.TitleKey,
			Path: n.Path, Icon: n.Icon,
		}
		item.Children = NavItemsOf(n.Children)
		if len(item.Children) == 0 && item.Path == "" {
			continue
		}
		out = append(out, item)
	}
	return out
}

func hasNavChildren(items []jetpkg.NavItem) bool {
	for _, item := range items {
		if len(item.Children) > 0 {
			return true
		}
	}
	return false
}

func sidebarOpen(c *gin.Context) bool {
	if c.Request == nil {
		return true
	}
	v, err := c.Cookie(SidebarCookieOpen)
	if err != nil || v == "" {
		return true
	}
	return v != "0"
}

func sidebarPinned(c *gin.Context) bool {
	if c.Request == nil {
		return false
	}
	v, _ := c.Cookie(SidebarCookiePinned)
	return v == "1"
}

// 归口错误消息事实：key + 兜底，供页面侧拼装受控提示。
const (
	// MsgInternalError 是页面归口错误的词条 key。
	MsgInternalError = jetpkg.InternalKey
	// InternalErrorFallback 是缺词时的兜底文案。
	InternalErrorFallback = response.InternalFallback
)

// ErrFact 是页面侧使用的错误事实形状。
type ErrFact = facing.Fact

// PageErrorFact 返回本次请求的错误事实（直连请求级取词作用域）。
func PageErrorFact(c *gin.Context, scene string, err error) ErrFact {
	return jetpkg.FactOf(c, scene, err)
}

// PageErrorFactQuiet 返回错误事实，但不按场景记日志。
func PageErrorFactQuiet(_ *gin.Context, _ string, err error) ErrFact {
	if err == nil {
		return ErrFact{}
	}
	if fact, ok := facing.FactOf(err); ok {
		return fact
	}
	return ErrFact{Key: MsgInternalError, Fallback: InternalErrorFallback}
}

// ErrorFactText 按请求语言取词后返回错误文案。
func ErrorFactText(c *gin.Context, fact ErrFact) string {
	if fact.Key == "" {
		return ""
	}
	return jetpkg.RequestTranslateFunc(c)(fact.Key, fact.Fallback)
}

// PageErrorText 是 PageErrorFact + ErrorFactText 的组合。
func PageErrorText(c *gin.Context, scene string, err error) string {
	return ErrorFactText(c, PageErrorFact(c, scene, err))
}

// PageErrorTextQuiet 是 PageErrorFactQuiet + ErrorFactText 的组合。
func PageErrorTextQuiet(c *gin.Context, scene string, err error) string {
	return ErrorFactText(c, PageErrorFactQuiet(c, scene, err))
}

// InternalErrorFact 返回归口错误事实。
func InternalErrorFact() ErrFact {
	return ErrFact{Key: MsgInternalError, Fallback: InternalErrorFallback}
}

// InternalMsg 是归口错误消息事实，供页面侧拼装受控提示。
func InternalMsg() facing.Msg {
	return facing.Msg{Key: MsgInternalError, Fallback: InternalErrorFallback}
}

// BulkIDsFacingText 把批量 id 超限 / 读取失败转成受控文案。
func BulkIDsFacingText(c *gin.Context, err error) string {
	var tooMany *bulkids.IDsError
	if errors.As(err, &tooMany) {
		logger.Scene("view").
			With("path", jetpkg.RequestURI(c)).
			With("count", tooMany.Count).
			With("max", tooMany.Max).
			Warn("批量操作被拒：提交的 id 数量超过上限")
		return response.MessageOf(c, "view", err)
	}
	if err != nil {
		logger.Scene("view").
			With("path", jetpkg.RequestURI(c)).
			Error(err, "批量 id 读取失败：未命中受控错误，只对外给归口文案")
	}
	return response.MessageOf(c, "view", err)
}

// AttachPaginationFacts 把计算好的分页事实放进渲染数据；
// path 非空时按筛选条件重建基础链接。
func AttachPaginationFacts(data gin.H, path string, filters map[string]string, p pagination.Page) gin.H {
	if data == nil {
		return data
	}
	if path != "" {
		p.Base = reqparams.FilterBaseURL(path, filters)
	}
	data[paginationKey] = p
	return data
}

// AttachPaginationView 把分页事实写回视图对象的分页字段；写不上返回 false。
func AttachPaginationView(viewResult any, path string, filters map[string]string, p pagination.Page) bool {
	if viewResult == nil {
		return false
	}
	if path != "" {
		p.Base = reqparams.FilterBaseURL(path, filters)
	}
	return AttachPage(viewResult, p)
}

// SuccessTemplate 是唯一共享的成功提示组件（只输出一条消息，不解释业务 data）。
const SuccessTemplate = "admin/partials/write_success.html"

// NewSuccessWriter 返回统一成功出口：默认「操作成功」，自定义消息只消费明确消息事实。
//
// 它不接模板名、业务 data、实体 ID、HTML、URL 或导航配置；
// 成功后刷新 / 关闭 / 导航由发起页面声明、共享前端基座执行。
func NewSuccessWriter() SuccessWriter {
	return func(c *gin.Context, fact SuccessFact) {
		if c == nil {
			return
		}
		jetpkg.AttachScope(c)
		c.Header("Cache-Control", "no-store")
		c.HTML(http.StatusOK, SuccessTemplate, gin.H{
			"HasSuccessMessage": fact.Key != "" || fact.Fallback != "",
			"SuccessKey":        fact.Key,
			"SuccessFallback":   fact.Fallback,
			"SuccessArgs":       fact.Args,
		})
	}
}

// WriteSuccess 兼容既有的直写调用点，统一走同一提示组件。
func WriteSuccess(c *gin.Context) {
	SuccessWith(c, SuccessFact{})
}
