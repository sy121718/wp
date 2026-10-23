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
	admindto "go_wp/internal/module/admin/dto"
	"go_wp/internal/text"
	"go_wp/pkg/binding"
	viewpkg "go_wp/pkg/view"
)

// 侧栏状态 cookie 名（随装配一起迁入，取值语义见 sidebarOpen / sidebarPinned）。
const (
	sidebarCookieOpen   = "sidebar_open"
	sidebarCookiePinned = "sidebar_pinned"
)

// CSRF 当前请求的 CSRF token；取不到时返回空串（提交会被中间件拒，与其它后台页一致）。
//
// 单独的取值口是给「只需要 token 的那一块片段数据」用的（抽屉表单的隐藏域）：
// 那些地方不该为了一个 token 去装配整份页壳。
func CSRF(c *gin.Context) string {
	return binding.CSRFToken(c)
}

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

	data = text.Inject(c, data)

	data["csrf_token"] = CSRF(c)

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
	data := Prepare(c, ViewMap(view))
	injectPageList(data, view)
	return data
}

// injectPageList 把视图自报的分页事实放进渲染数据（视图没实现 view.PageList 时什么也不做）。
//
// 视图自己已经塞了 Pagination 键（存量 gin.H 写法）时不覆盖：那是调用点显式给的值，
// 比契约的默认值更具体。这条让迁移可以分批做 —— 迁一个页面就删一处手写赋值，
// 没迁的页面行为不变。
func injectPageList(data gin.H, view any) {
	page, ok := viewpkg.PageListOf(view)
	if !ok {
		return
	}
	if _, exists := data[viewpkg.PaginationKey]; exists {
		return
	}
	data[viewpkg.PaginationKey] = page
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
	// 具名的 map 视图（例如 `type inventoryLoadFailedView gin.H`：同一份键集 + 自带
	// PageStatus / PageNoStore 的落库判断）。只要底层仍是 map[string]…，按 map 处理 ——
	// 否则「视图要带渲染结论」与「视图要能拼键」这两条就只能在控制器里二选一。
	if rv.Kind() == reflect.Map && rv.Type().Key().Kind() == reflect.String {
		out := make(gin.H, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = iter.Value().Interface()
		}
		return out
	}
	// 这里不需要再解一次指针：上面的循环已经把 rv 解到非指针；
	// map 分支提前 return，走到这里的只可能是 struct 或其它。
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

// PrepareBare 为**不带页壳**的后台模板注入取词函数（lang / t / tf），页壳那套键一概不注入。
//
// 为什么单独一个入口：这些模板不带页壳，Prepare 里的 CSRF 隐藏域 / 权限码 / 导航树 / 侧栏
// cookie 对它是白算；但它同样要按请求语言取词，而 t/tf **只能经 data 注入** ——
// 语言是请求级的，Set.AddGlobal 的全局函数拿不到当前请求的语言（见 injectI18n 的说明）。
//
// 调用点：htmx 局部更新（adminui.RenderBare）与任何 `c.HTML(..., data)` 的不带页壳渲染。
//
// 漏注入的后果是**静默**的：模板里 `{{tr := .["t"]}}` 拿到 nil，该模板所有取词一起变
// 空串 —— 页面结构完整、状态码 200、日志干净（见 internal/templates/CLAUDE.md 的实测记录）。
// 控制器因此被迫在 Go 侧提前取词、把成品串塞进槽位；接上本函数之后槽位可以只放事实
// （adminui.Msg），取词统一回到渲染期。
func PrepareBare(c *gin.Context, data gin.H) gin.H {
	return text.Inject(c, data)
}

// translateFor 返回 Go 侧文案翻译函数（分页条等由 Go 拼接、不经模板的文案）。
// c 为 nil 时按默认语言返回，绝不 panic。
//
// 实现在 internal/text（与模板注入的 t 同一份）：文案线要能被业务模块直接调用，
// 挂在 adminui 上会逼着每个控制器 import 整个后台壳。
func translateFor(c *gin.Context) func(key, fallback string) string {
	return text.TranslateFunc(c)
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

// requestURIOf 当前请求的站内 URI（含 query）；异常时回控制面首页。
//
// 实现在 internal/text（语言回跳与日志都要它）：日志要真实 URI，语言回跳要经
// LangRedirect 收敛后的值 —— 两者不能共用一份（拿日志值当回跳目标就是开放重定向）。
func requestURIOf(c *gin.Context) string {
	return text.RequestURI(c)
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
