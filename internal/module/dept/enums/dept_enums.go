// Package deptenums dept 模块的业务消息。
package deptenums

const (
	ErrDeptNotFound      = "部门不存在"
	ErrDeptHasChildren   = "该部门下有子部门，无法删除"
	ErrDeptHasUsers      = "该部门下有用户，无法删除"
	ErrDeptCircle        = "不能将部门移动到自身或其子级下"
	ErrDeptCodeExists    = "部门编码已存在"
)

const (
	MsgSuccess    = "success"
	MsgBadRequest = "请求参数错误"
)