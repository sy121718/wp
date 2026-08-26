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
type RouteNode struct {
	Path      string      `json:"path"`
	Name      string      `json:"name"`
	Component string      `json:"component,omitempty"`
	Redirect  string      `json:"redirect,omitempty"`
	Meta      RouteMeta   `json:"meta"`
	Children  []RouteNode `json:"children,omitempty"`
}
