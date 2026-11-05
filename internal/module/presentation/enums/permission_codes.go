package presentationenums

// 权限标识：本模块操作的所有者声明。
// 值不改；HTTP/MCP/令牌等跨入口共用同一份常量。

const (
	PresentationCreate = "presentation:create"
	PresentationDelete = "presentation:delete"
	PresentationGet = "presentation:get"
	PresentationGetByEntity = "presentation:get_by_entity"
	PresentationList = "presentation:list"
	PresentationPreview = "presentation:preview"
	PresentationRebuild = "presentation:rebuild"
	PresentationUpdateURL = "presentation:update_url"
)
