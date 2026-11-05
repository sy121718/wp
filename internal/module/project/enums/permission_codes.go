package projectenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	ProjectCreate = "project:create"
	ProjectDetail = "project:detail"
	ProjectList = "project:list"
	ProjectThemeActivate = "project:theme_activate"
	ProjectThemeActive = "project:theme_active"
	ProjectThemeCreate = "project:theme_create"
	ProjectThemeDelete = "project:theme_delete"
	ProjectThemeList = "project:theme_list"
	ProjectThemeUpdate = "project:theme_update"
	ProjectUpdate = "project:update"
)
