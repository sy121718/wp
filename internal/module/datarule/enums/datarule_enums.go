// Package dataruleenums datarule 模块的业务消息。
package dataruleenums

const (
	ErrRuleNotFound      = "数据规则不存在"
	ErrInvalidDomain     = "不支持的数据域"
	ErrInvalidField      = "字段不在白名单中"
	ErrInvalidOp         = "不支持的操作符"
	ErrInvalidAssignment = "无效的数据规则分配目标"
)

const (
	MsgSuccess    = "success"
	MsgBadRequest = "请求参数错误"
)
