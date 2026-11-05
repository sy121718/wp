package contenttemplateenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	ContenttemplateActivate = "contenttemplate:activate"
	ContenttemplateCreate = "contenttemplate:create"
	ContenttemplateDelete = "contenttemplate:delete"
	ContenttemplateGet = "contenttemplate:get"
	ContenttemplateList = "contenttemplate:list"
	ContenttemplateUpdate = "contenttemplate:update"
)
