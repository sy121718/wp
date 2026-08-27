// Package projectenums 统一管理 project 模块响应消息。
package projectenums

const (
	ErrProjectNotFound = "站点工程不存在"
	ErrInvalidSettings = "站点设置必须是合法的 JSON 对象"
	ErrInvalidName     = "站点工程名称不能为空且不能超过 200 个字符"
)

const (
	MsgProjectCreated = "站点工程创建成功"
	MsgProjectUpdated = "站点工程更新成功"
)
