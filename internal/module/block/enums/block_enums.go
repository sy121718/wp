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

	// 有效源码引用的类别文案（审计 ARCH-02，块删除保护的定位提示）。
	//
	// 为什么单独给类别建词条：拒绝删除时要回答「是哪一类引用在挡路」——
	// 「页面文档树」与「内容模板」的解除路径完全不同（改页面 vs 改模板），
	// 只说「被引用」等于让操作者自己把整站文档翻一遍。
	// 值是 i18n key，真文案在 sys_i18n（迁移 297）。
	MsgBlockUsagePageDocument         = "MsgBlockUsagePageDocument"         // 页面文档树
	MsgBlockUsagePageStructure        = "MsgBlockUsagePageStructure"        // 页面的页眉/页脚/槽位绑定
	MsgBlockUsagePageRevision         = "MsgBlockUsagePageRevision"         // 页面的历史修订（可回滚的源码历史）
	MsgBlockUsageThemeSlot            = "MsgBlockUsageThemeSlot"            // 主题的页眉/页脚槽位绑定
	MsgBlockUsageBlockDocument        = "MsgBlockUsageBlockDocument"        // 其它全局块的文档树
	MsgBlockUsageContentTemplate      = "MsgBlockUsageContentTemplate"      // 内容模板
	MsgBlockUsagePresentationInstance = "MsgBlockUsagePresentationInstance" // 自动发布实例

	// MsgInternalError 块服务内部错误统一兜底提示（禁止直出 err.Error() 泄露内部细节）。
	MsgInternalError = "MsgInternalError" // 系统内部错误，请稍后重试

	// —— 点分 key 常量（新式）——
	//
	// 值是 sys_i18n 的 item_key（文案在迁移里），**不带 `Msg` 前缀**：本文件上面那批 `Msg*`
	// 是老式形态（值就是常量名本身，`sys_i18n` 里存同名 key），两者混在同一个前缀下会让人
	// 以为值也是 `MsgXxx`。点分 key 的常量按「去掉模块前缀后的语义路径」命名。
	ImpactUnavailableNoPageCapability  = "admin.blocks.impact.unavailable.noPageCapability"  // 页面能力未装配，无法统计影响面
	ImpactUnavailableProjectReadFailed = "admin.blocks.impact.unavailable.projectReadFailed" // 读取站点工程失败，无法统计影响面
	RefCountUnknown                    = "admin.blocks.refCount.unknown"                     // 引用数未知
	RefCountNone                       = "admin.blocks.refCount.none"                        // 未被页面引用
	RefCountPages                      = "admin.blocks.refCount.pages"                       // 引用数（{count} 个页面引用）
	UsageMore                          = "admin.blocks.usage.more"                           // 引用处数过多时的省略后缀（ 等 {count} 处）
	ContentSavedRebuildQueued          = "admin.blocks.content.savedRebuild"                 // 已保存，关联页面将标记为待重建
)
