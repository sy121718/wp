package adminui

// prepare.go — 后台外壳装配（task-92 批 2a 自 internal/shell 迁入）。
//
// 为什么归这里：Prepare 的产物只有两样 —— 注入给模板的壳键（多语言 / CSRF / 权限码 /
// 按钮码 / 侧栏状态）与交给 BuildNav 的导航树。视图构造（NavGroup / NavNode / 当前页标记 /
// 隐藏项过滤）自 task-72 起就在本包，装配留在 shell 只会让「外壳」跨两个包各持一半。
// 迁完本包就是后台外壳的唯一一侧：装配（本文件）、视图（nav.go）、拒绝出口（reject.go）。
//
// 权限上下文键由写入方 internal/middleware/adminctx 声明；本包直接引用它们，
// 依赖方向保持 adminui → adminctx → admincontract。侧栏 cookie 名仍归本包。
//
// 本文件不做任何展示行为改动：注入键的名字、类型、取值规则与迁出前逐字节一致
// （task-92 批 2a 是纯结构搬迁）。

import (
	"reflect"

	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/adminctx"
	"go_wp/internal/middleware/builtin"
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/templates"
	"go_wp/pkg/response"
	"go_wp/pkg/safepath"
)

// 侧栏状态 cookie 名（随装配一起迁入，取值语义见 sidebarOpen / sidebarPinned）。
const (
	sidebarCookieOpen   = "sidebar_open"
	sidebarCookiePinned = "sidebar_pinned"
)

// Prepare 为页面模板数据补上外壳所需的全部键，所有后台页面渲染入口统一调用。
//
// 注入内容：lang / t / tf / langs / lang_redirect（多语言，标题为 i18n key 时一并翻译；tf 带
// 具名占位符参数，供提示页渲染带计数的结论文案）、csrf_token（layout 的 hx-headers）、PermSet（当前用户有效权限码）、
// Buttons（当前用户有权触发的按钮码 —— 模板判断按钮显隐用它，不写权限码）、
// NavGroups（侧栏导航树，已按权限过滤）、SidebarOpen / SidebarPinned / HasSubnav。
//
// 兜底：token 取不到时置空串（Jet 的 {{ .["csrf_token"] }} 对空值安全，提交会被 CSRF
// 中间件拒，与其它后台页一致）；i18n 未初始化或词条缺失时 t 返回模板内中文原文，绝不报错。
func Prepare(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}

	data = injectI18n(c, data)

	if tok, err := builtin.GetCSRFToken(c); err == nil {
		data["csrf_token"] = tok
	} else {
		data["csrf_token"] = ""
	}

	set := map[string]bool{}
	if v, ok := c.Get(adminctx.PermSetKey); ok {
		if m, ok := v.(map[string]bool); ok {
			set = m
		}
	}
	data["PermSet"] = set

	// 按钮码集合：与 PermSet 同源（都由 PermContextMiddleware 按用户有效权限码算出），
	// 但键是菜单侧的按钮码 —— 模板写 {{if isset(.Buttons["product.create"])}}。
	buttons := map[string]bool{}
	if v, ok := c.Get(adminctx.ButtonsKey); ok {
		if m, ok := v.(map[string]bool); ok {
			buttons = m
		}
	}
	data["Buttons"] = buttons

	// 侧栏导航树：数据是 sys_menus（中间件已按权限过滤），视图构造与当前页标记在
	// nav.go 的 BuildNav；本文件只提供上下文里的树与取词函数。
	navGroups := BuildNav(navTreeOf(c), c.Request.URL.Path, translateFor(c))
	data["NavGroups"] = navGroups

	// 侧边栏展开态由 cookie 决定，服务端渲染首屏即正确（无「先展开后收起」闪烁）。
	open := sidebarOpen(c)
	pinned := sidebarPinned(c)
	// 当前页不属于任何目录分组（如仪表盘这类直接链接页）且未固定时默认收起，
	// 否则二级栏无 is-active 分组会整块空白。
	if open && !pinned && !hasActiveDirGroup(navGroups) {
		open = false
	}
	data["SidebarOpen"] = open
	data["SidebarPinned"] = pinned
	// 二级栏渲染开关：仅当当前页属于某个目录分组时才渲染二级栏 DOM。
	data["HasSubnav"] = hasActiveDirGroup(navGroups)

	return data
}

// PrepareView 页面渲染入口：把**视图结构体**摊平成 gin.H 再走 Prepare。
//
// 为什么需要它：控制器只该做「绑定并校验请求 → 调 service → 渲染」四件事，页面取数与视图
// 装配属业务层（`internal/module/CLAUDE.md` §dto：金额、折扣、状态的展示串在 service 组装响应时
// 写进 dto，页面只读字段）。视图因此是 dto 里的结构体，而模板要的是一层键（`{{.Projects}}`）——
// 中间这一层摊平只有一处实现，别让每个页面 handler 各写一份 `gin.H{...}` 装配。
//
// 摊平规则（ViewMap）：导出字段 → 键名 = 字段名，或字段的 `view:"名字"` 标签；嵌套结构体
// **不递归**摊平（模板写 `{{.LoadError.Key}}`）；未导出字段跳过；nil 视图 → 空 map；
// `gin.H` / `map[string]any` 原样透传（存量 handler 仍在用）。非结构体传入属**编程错误**，
// 直接 panic（与 `order` 的 `selectExpr` 同判据：写对了永不触发，写错了第一次跑到就炸）。
//
// 模板侧零改动：键名与原先手写的 gin.H 一致，`{{.X}}` 写法不变。
func PrepareView(c *gin.Context, view any) gin.H {
	return Prepare(c, ViewMap(view))
}

// ViewMap 把视图结构体摊平成一层的 gin.H（键名规则见 PrepareView）。
func ViewMap(view any) gin.H {
	switch v := view.(type) {
	case nil:
		return gin.H{}
	case gin.H:
		return v
	case map[string]any:
		out := make(gin.H, len(v))
		for k, val := range v {
			out[k] = val
		}
		return out
	}

	rv := reflect.ValueOf(view)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return gin.H{}
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		panic("adminui.ViewMap: 视图必须是结构体、结构体指针或 gin.H/map[string]any，" +
			"收到 " + rv.Kind().String() + "（视图装配属于 dto，不要在控制器里手工拼 map）")
	}

	rt := rv.Type()
	out := make(gin.H, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		name := field.Name
		if tag := field.Tag.Get("view"); tag != "" {
			name = tag
		}
		out[name] = rv.Field(i).Interface()
	}
	return out
}

// PrepareFragment 为后台**片段**模板注入取词函数（lang / t / tf），页壳那套键一概不注入。
//
// 为什么单独一个入口：片段不带页壳，Prepare 里的 CSRF 隐藏域 / 权限码 / 导航树 / 侧栏
// cookie 对它是白算；但片段同样要按请求语言取词，而 t/tf **只能经 data 注入** ——
// 语言是请求级的，Set.AddGlobal 的全局函数拿不到当前请求的语言（见 injectI18n 的说明）。
//
// 调用点：所有 `c.HTML(..., "fragments/xxx", data)` 的后台片段渲染。
//
// 漏注入的后果是**静默**的：模板里 `{{tr := .["t"]}}` 拿到 nil，该片段所有取词一起变
// 空串 —— 页面结构完整、状态码 200、日志干净（见 internal/templates/CLAUDE.md 的实测记录）。
// 控制器因此被迫在 Go 侧提前取词、把成品串塞进槽位；接上本函数之后槽位可以只放事实
// （adminui.Msg），取词统一回到渲染期。
func PrepareFragment(c *gin.Context, data gin.H) gin.H {
	if data == nil {
		data = gin.H{}
	}
	return injectI18n(c, data)
}

// injectI18n 注入多语言键（lang / t / tf / langs / lang_redirect，标题是 i18n key 时翻译）。
//
// t 的签名是 func(key, fallback string) string，模板用 {{ .["t"]("shell.brand", "管理后台") }}。
// 实测取翻译路径的取舍见 internal/templates/i18n_jet_test.go。
func injectI18n(c *gin.Context, data gin.H) gin.H {
	lang := response.RequestLanguage(c)
	t := templates.TranslateFunc(lang)

	data["lang"] = lang
	data["t"] = t
	// tf 是「取词 + `%s` 占位符填充」：签名 func(key, fallback string, args []string) string。
	//
	// 为什么需要它：跳转提示页的结论文案带计数（「已删除 %s 个页面。」），而 Go 侧不再
	// 拼整句 —— 控制器只给 key + 兜底 + 参数（见 internal/shell/jump.go 的 Jump）。
	// 走 data 注入而不是 Set.AddGlobal：语言是**请求级**的，全局函数拿不到当前语言，
	// 按请求切语言就得改共享状态（并发下 -race 报 DATA RACE），理由同 t。
	data["tf"] = func(key, fallback string, args []string) string {
		return TranslateArgs(c, key, fallback, args)
	}
	data["langs"] = templates.LanguageOptions(lang)
	// 语言切换表单的 redirect 隐藏域：当前页 URI 经 langRedirect 收敛
	//（判据在 pkg/safepath，本函数只负责「不合法时回落当前请求路径、再回落控制面首页」）。
	data["lang_redirect"] = langRedirect(c)

	// 页面标题：enums 常量已是 key（如 MsgPagesTitle），此处按当前语言翻译；
	// 非 key 的字面量标题（如登录页）走 fallback 原样返回，不改变行为。
	if title, ok := data["title"].(string); ok && title != "" {
		data["title"] = t(title, title)
	}
	return data
}

// translateFor 返回 Go 侧文案翻译函数（分页条等由 Go 拼接、不经模板的文案）。
// c 为 nil 时按默认语言返回，绝不 panic。
func translateFor(c *gin.Context) func(key, fallback string) string {
	return templates.TranslateFunc(response.RequestLanguage(c))
}

// navTreeOf 取本请求的授权菜单树；未注入（未登录 / 构建失败 / 非页面路由）时返回 nil。
//
// 授权树由 adminctx.PermContextMiddleware 注入，读取方共用 adminctx.NavTreeKey；
// 本包只在装配内部取树，不额外导出读取器。
func navTreeOf(c *gin.Context) []admindto.MenuTreeNode {
	if v, ok := c.Get(adminctx.NavTreeKey); ok {
		if tree, ok := v.([]admindto.MenuTreeNode); ok {
			return tree
		}
	}
	return nil
}

// langRedirect 语言切换表单 redirect 隐藏域的取值（渲染侧）。
//
// 判据只有一份，在 pkg/safepath.LangRedirectPath（task-75 迁出，两侧共用）：站内相对路径
// 且不超过 MaxReturnPathBytes 才通过。本函数只补渲染侧的两级兜底 —— 丢弃整个 URI（含超长
// query）回落到当前请求的**路径**；路径本身也异常时回控制面首页。
//
// 当前取值链：layout 的 redirect 隐藏域 → Prepare → injectI18n → 本函数；
// GET /admin/lang 的 adminSafeLangRedirect 再校验 query。shell 的零消费者包装已删除；
// 日志仍用 shell.requestURI 记录真实 URI，不应复用隐藏域的收敛值。
func langRedirect(c *gin.Context) string {
	if c == nil {
		return adminHomePath
	}
	if p := safepath.LangRedirectPath(requestURIOf(c)); p != "" {
		return p
	}
	return requestPathOrAdminHome(c)
}

// requestPathOrAdminHome 回落到当前请求的路径（丢掉 query）：回跳仍回到发起页；
// 路径本身异常（空 / 非同源 / 超长）时回控制面首页。
func requestPathOrAdminHome(c *gin.Context) string {
	if c != nil && c.Request != nil && c.Request.URL != nil {
		if p := c.Request.URL.Path; safepath.IsSameOriginPath(p) && len(p) <= safepath.MaxReturnPathBytes {
			return p
		}
	}
	return adminHomePath
}

// requestURIOf 返回当前请求的站内 URI（含 query）；异常时回控制面首页。
func requestURIOf(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return adminHomePath
	}
	uri := c.Request.URL.RequestURI()
	if uri == "" {
		return adminHomePath
	}
	return uri
}

// hasActiveDirGroup 判断导航树中是否存在「当前页所在的目录分组」（有二级内容的组）。
// 仪表盘这类直接链接页返回 false。
func hasActiveDirGroup(groups []NavGroup) bool {
	for _, g := range groups {
		if g.Active && len(g.Nodes) > 0 {
			return true
		}
	}
	return false
}

// sidebarOpen 侧边栏是否展开：cookie 缺省为展开（首次访问体验）。
func sidebarOpen(c *gin.Context) bool {
	v, err := c.Cookie(sidebarCookieOpen)
	if err != nil || v == "" {
		return true
	}
	return v != "0"
}

// sidebarPinned 侧边栏是否固定（固定后导航不再自动收起）。
func sidebarPinned(c *gin.Context) bool {
	v, _ := c.Cookie(sidebarCookiePinned)
	return v == "1"
}
