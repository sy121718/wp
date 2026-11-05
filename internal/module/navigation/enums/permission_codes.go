package navigationenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	NavigationCreate = "navigation:create"
	NavigationDelete = "navigation:delete"
	NavigationDeleteBulk = "navigation:deleteBulk"
	NavigationGet = "navigation:get"
	NavigationList = "navigation:list"
	NavigationMove = "navigation:move"
	NavigationSourceAdd = "navigation:sourceAdd"
	NavigationTranslate = "navigation:translate"
	NavigationUpdate = "navigation:update"
)
