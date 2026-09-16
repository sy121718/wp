// Package blockenums 统一管理 block 模块响应消息与错误消息。
package blockenums

const (
	MsgBlockTitle = "MsgBlockTitle" // 全局块

	ErrBlockParamRequired = "ErrBlockParamRequired" // 块参数不能为空
	ErrBlockNotFound      = "ErrBlockNotFound"      // 全局块不存在
	ErrProjectNotFound    = "ErrProjectNotFound"    // 工程不存在
	// ErrBlockProjectRequired 缺可作用域的工程（DB-009）：只带 id 的入口要逐工程定位归属，
	// 而工程清单为空或读不到。显式失败而不是静默返回「块不存在」——后者会把
	// 「读不到工程表」伪装成「块不存在」。
	ErrBlockProjectRequired = "ErrBlockProjectRequired" // 缺少可作用域的工程，无法定位块的工程归属
	ErrBlockNameRequired    = "ErrBlockNameRequired"    // 块名称不能为空
	ErrBlockInvalidDoc      = "ErrBlockInvalidDoc"      // 块文档不合法
	ErrBlockInvalidKind     = "ErrBlockInvalidKind"     // 块类型不合法
	ErrBlockInvalidCategory = "ErrBlockInvalidCategory" // 块分类不合法
	ErrBlockDuplicate       = "ErrBlockDuplicate"       // 同名块已存在

	ErrBlockInvalidReuseMode = "ErrBlockInvalidReuseMode" // 块复用方式不合法（仅支持 global/template）
	ErrBlockInUse            = "ErrBlockInUse"            // 全局块已被页面或主题引用，无法删除或切换为一次性复制；可强制删除（引用页面将退化为无该块）

	MsgBlockCloned = "MsgBlockCloned" // 已复制块内容，副本与源块独立

	// MsgInternalError 块服务内部错误统一兜底提示（禁止直出 err.Error() 泄露内部细节）。
	MsgInternalError = "MsgInternalError" // 系统内部错误，请稍后重试
)
