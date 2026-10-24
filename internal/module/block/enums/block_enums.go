// Package blockenums 统一管理 block 模块响应消息与错误消息。
package blockenums

import "go_wp/pkg/facing"

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
	// MsgBlockCreated 新建块成功后的提示页正文（成功后自动进工作台编辑内容）。
	//
	// 新增常量按未迁形态取值（值 = 常量名）：它同时被 pkg/response 的形态判据接受，
	// 也不在 check-i18n-keys-seeded.sh 的 `admin.*` 扫描范围内。词条尚未 seed 时页面走
	// fallback 中文兜底（与 plugin 新增 notice key 的过渡做法一致）。
	MsgBlockCreated = "MsgBlockCreated" // 块已创建，正在打开编辑器

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

	// —— 借自 admin.pages.* 的影响面阶段词条（block 侧 stale 抽屉展示同一列 stage）——
	//
	// 阶段取值由 page service 写进 pages.rebuild_failed_stage（迁移 474），它的展示文案在
	// page/enums 的 RebuildStageLabel 里已有一份。block 侧的 stale 抽屉按既有取舍
	// 「三处各持一份、改一处同步另两处」保留本模块的常量（见 block_page.go 的说明）：
	// 引 page/enums 会造出那条取舍正在回避的跨模块依赖，而这里只是两个字符串。
	ImpactStagePlan  = "admin.pages.impact.stage_plan"  // 计划阶段（站点语言清单或旧发布范围读不到）
	ImpactStageBuild = "admin.pages.impact.stage_build" // 构建 / 发布阶段
)

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrBlockParamRequired` 这样的裸 key
// （这批常量的值就是常量名本身，旧式形态）。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
var errFallbacks = map[string]string{
	ErrBlockParamRequired:    "块参数不能为空",
	ErrBlockNotFound:         "全局块不存在",
	ErrProjectNotFound:       "工程不存在",
	ErrBlockProjectRequired:  "缺少可作用域的站点工程，无法定位全局块的工程归属",
	ErrBlockNameRequired:     "块名称不能为空",
	ErrBlockInvalidDoc:       "块文档不合法",
	ErrBlockInvalidKind:      "块类型不合法",
	ErrBlockInvalidCategory:  "块分类不合法",
	ErrBlockDuplicate:        "同名块已存在",
	ErrBlockInvalidReuseMode: "块复用方式不合法（仅支持 global/template）",
	ErrBlockInUse:            "全局块已被页面或主题引用，无法删除或切换为一次性复制；可强制删除（引用页面将退化为无该块）",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }
