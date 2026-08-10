// Package permissionenums permission 模块的业务消息。
// 当前阶段用硬编码中文常量占位。
package permissionenums

// --- 业务错误消息（service 层） ---

const (
	ErrPermissionNotFound     = "权限点不存在"
	ErrCodeExists             = "权限编码已存在"
	ErrCodeImmutable          = "权限编码创建后不可修改"
	ErrInvalidMethod          = "请求方法只允许 GET 或 POST"
	ErrPermissionAssigned     = "该权限已分配，请先解除角色和用户授权"
	ErrMenuReferenced         = "该权限被菜单引用，无法删除"
	ErrMenuCheckerUnavailable = "菜单引用检查未初始化"
)

// --- 响应消息（handler 层） ---

const (
	MsgSuccess    = "success"
	MsgBadRequest = "请求参数错误"
)
