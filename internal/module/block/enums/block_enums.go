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

	ErrBlockInvalidReuseMode = "块复用方式不合法（仅支持 global/template）"
	ErrBlockInUse            = "全局块已被页面或主题引用，无法删除或切换为一次性复制；可强制删除（引用页面将退化为无该块）"

	MsgBlockCloned = "已复制块内容，副本与源块独立"

	// MsgInternalError 块服务内部错误统一兜底提示（禁止直出 err.Error() 泄露内部细节）。
	MsgInternalError = "系统内部错误，请稍后重试"
)
