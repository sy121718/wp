// Package blockenums 统一管理 block 模块响应消息与错误消息。
package blockenums

const (
	MsgBlockTitle = "全局块"

	ErrBlockParamRequired   = "块参数不能为空"
	ErrBlockNotFound        = "全局块不存在"
	ErrProjectNotFound      = "工程不存在"
	ErrBlockNameRequired    = "块名称不能为空"
	ErrBlockInvalidDoc      = "块文档不合法"
	ErrBlockInvalidKind     = "块类型不合法"
	ErrBlockInvalidCategory = "块分类不合法"
	ErrBlockDuplicate       = "同名块已存在"

	// MsgInternalError 块服务内部错误统一兜底提示（禁止直出 err.Error() 泄露内部细节）。
	MsgInternalError = "系统内部错误，请稍后重试"
)
