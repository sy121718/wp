package admindto

// RouteMeta 前端动态路由元信息。
// TitleKey 为 sys_i18n 的 route.* 资源 key：前端转 i18nKey 后可在切换语言时本地重译（i18n-issues 1-5）。
type RouteMeta struct {
	Title    string   `json:"title"`
	TitleKey string   `json:"title_key,omitempty"`
	Icon     string   `json:"icon,omitempty"`
	ShowLink bool     `json:"showLink"`
	Rank     int      `json:"rank,omitempty"`
	Auths    []string `json:"auths,omitempty"`
}

// RouteNode 前端动态路由节点。
//
// **没有 component 字段**：它是 soybean-admin（Vue SPA）时代的产物，后台改成
// HTMX + Jet SSR 之后，页面路由只认 Path（Go 侧路由表决定渲染哪个模板），
// 组件路径既没有生产者也没有消费者。将来若真的再接一套 SPA 前端，
// 需要为「菜单 → 前端组件」另建一份来源，而不是把这个字段复活成空壳。
type RouteNode struct {
	Path     string      `json:"path"`
	Name     string      `json:"name"`
	Redirect string      `json:"redirect,omitempty"`
	Meta     RouteMeta   `json:"meta"`
	Children []RouteNode `json:"children,omitempty"`
}
