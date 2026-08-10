// Package roleenums role 模块的业务消息。
package roleenums

const (
	ErrRoleNotFound    = "角色不存在"
	ErrRoleCodeExists  = "角色编码已存在"
	ErrRoleIsSystem    = "系统内置角色不可删除"
	ErrRoleCodeNumeric = "角色编码不能为纯数字"
)

const (
	MsgSuccess    = "success"
	MsgBadRequest = "请求参数错误"
)