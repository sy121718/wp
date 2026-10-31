// Package pageenums 管理 page 模块业务消息。
package pageenums

import "go_wp/pkg/facing"

import "strings"

const (
	ErrActionTargetInUse = "page.err.actionTargetInUse"
	// ErrInvalidParam 请求本身不合法（nil 请求、空/空白 ID 等），与资源存在性无关。
	// —— 页面级语言排除（迁移 491）——
	//
	// 四条哨兵的取值即 i18n key（与其它 page 错误同形）；未接好词条前，后台页在调用点
	// 给中文兜底（由 pkg/facing 的哨兵自带，见 page/service/page_lang.go），不显示裸 key。
	//
	// ErrPageLangExcluded 该语言已被本页排除：发布 / 构建入口据此跳过。
	// 它是**正常业务状态**（作者主动撤下该语言），批量发布记成 skipped 而不是失败。
	ErrPageLangExcluded = "ErrPageLangExcluded"
	// ErrCannotExcludeDefaultLang 不允许排除站点默认语言：它的产物承载 x-default，
	// 且 default_plain 方案下「默认语言无前缀」是路径映射的锚点，排除它会让所有互指
	// 指向一条永远不会有产物的路径。
	ErrCannotExcludeDefaultLang = "ErrCannotExcludeDefaultLang"
	// ErrLangAlreadyExcluded 该语言此前已被排除（幂等入口不重复下线）。
	ErrLangAlreadyExcluded = "ErrLangAlreadyExcluded"
	// ErrLangNotExcluded 该语言不在本页的排除集合里（无从恢复）。
	ErrLangNotExcluded = "ErrLangNotExcluded"
	ErrInvalidParam    = "ErrInvalidParam"    // 请求参数无效
	ErrPageNotFound    = "ErrPageNotFound"    // 页面不存在
	ErrProjectNotFound = "ErrProjectNotFound" // 站点工程不存在
	// ErrProjectRequired 未指定工程且无法解析出唯一工程（0 个或多个工程）。
	//
	// DB-009 第三批：page 有一批入口的签名里没有工程参数（跨工程扇出类：整站标记待重建、
	// 按主题/块标记、全站草稿扫描）。它们的正确做法是**逐工程独立作用域**执行 —— 枚举
	// 工程表逐个设 app.project_id，绝不退回「不限工程」形态（换非超级角色后那是静默 0 行，
	// 表现为「改了块但页面不被标记」且没有任何日志）。工程表读不到或为空时在这里显式失败。
	ErrProjectRequired      = "ErrProjectRequired"      // 需要显式工程作用域
	ErrInvalidKind          = "ErrInvalidKind"          // 页面类型与内容目标不匹配
	ErrInvalidDocument      = "ErrInvalidDocument"      // 页面草稿文档不合法
	ErrInvalidPath          = "ErrInvalidPath"          // 页面访问路径不合法
	ErrDraftVersionConflict = "ErrDraftVersionConflict" // 草稿版本已更新，请刷新后重试
	ErrPathOccupied         = "ErrPathOccupied"         // 页面访问路径已被占用
	// ErrRevisionNotFound 修订历史里没有请求的版本（面板显示的是快照列表，列表过期后常见）。
	ErrRevisionNotFound = "ErrRevisionNotFound" // 修订版本不存在
	// ErrBlueprintUnavailable 蓝图能力未接入（装配缺失）：从蓝图建页时明确报错，
	// 而不是静默建成空页 —— 空页在后台看起来像「新建成功但内容是白的」。
	ErrBlueprintUnavailable = "ErrBlueprintUnavailable" // 蓝图能力未接入
	// ErrBlueprintInvalid 蓝图不存在或没有可用版本。
	ErrBlueprintInvalid   = "ErrBlueprintInvalid"   // 蓝图不可用
	ErrNoStagedArtifact   = "ErrNoStagedArtifact"   // 无暂存产物，请先构建
	ErrRollbackTargetMiss = "ErrRollbackTargetMiss" // 回滚目标产物不存在
	ErrRebuildRequired    = "ErrRebuildRequired"    // 草稿已变更，请重新构建后再发布
	// ErrInputMoved 编译之后、激活之前输入又变了（草稿 / 译文 / 菜单依赖任一推进了
	// pages.input_version），本次产物依据的已不是最新输入 —— 拒绝激活，页面保持 stale。
	//
	// 与 ErrRebuildRequired 的区分：后者是「产物与当前草稿不一致，请重新构建」；
	// 前者是「我编译完到这里为止，输入又前进了一代」，重试时机由队列/用户决定。
	ErrInputMoved = "ErrInputMoved" // 输入已变更，请重新构建后再发布
	// ErrRebuildOverloaded 本轮同步重建超过了单批上限，超出部分已入队异步执行。
	//
	// 它不是故障：入队是正常处置（队列会重试到收敛）。调用方看到它时应当理解为
	// 「本批没有全部同步完成」，而不是「这次依赖变更失败了」。
	ErrRebuildOverloaded = "ErrRebuildOverloaded" // 本轮重建超过单批上限，其余已入队

	// 系统页面槽位（BIZ-1）。
	ErrInvalidSlot  = "ErrInvalidSlot"  // 槽位键不在白名单内
	ErrSlotPageMiss = "ErrSlotPageMiss" // 槽位要绑的页面不存在或不属于本工程

	// 重定向管理（审计 SEO-025）。
	ErrRedirectUnavailable = "ErrRedirectUnavailable" // 路由能力未接入，无法管理重定向
	ErrRedirectNotFound    = "ErrRedirectNotFound"    // 这条重定向不存在
	ErrRedirectOccupied    = "ErrRedirectOccupied"    // 源路径已被占用
	ErrRedirectTargetMiss  = "ErrRedirectTargetMiss"  // 目标路径不存在或未激活
	ErrRedirectLoop        = "ErrRedirectLoop"        // 重定向成环
	// ErrRedirectConflict 删除时归属已变：锁内复验发现这条重定向行或它的访问面链接
	// 已不归本次处置（并发删除 / 被新发布接管）。它是冲突而不是"不存在" ——
	// 后者会让操作者以为自己点错了。
	ErrRedirectConflict = "ErrRedirectConflict" // 重定向已被其它操作改动
)

const (
	MsgPageCreated     = "MsgPageCreated"     // 页面创建成功
	MsgPageDeleted     = "MsgPageDeleted"     // 页面删除成功
	MsgDraftSaved      = "MsgDraftSaved"      // 草稿保存成功
	MsgPageDetail      = "MsgPageDetail"      // 页面查询成功
	MsgRevisionsListed = "MsgRevisionsListed" // 页面修订查询成功
	MsgBuildReady      = "MsgBuildReady"      // 构建完成，产物已暂存
	MsgPublished       = "MsgPublished"       // 发布成功
	MsgRollbackDone    = "MsgRollbackDone"    // 回滚成功
	MsgURLUpdated      = "MsgURLUpdated"      // 访问路径已更新
	MsgSiteSlotBound   = "MsgSiteSlotBound"   // 系统页面已绑定
	MsgSiteSlotUnbound = "MsgSiteSlotUnbound" // 系统页面已解绑
	MsgRedirectCreated = "MsgRedirectCreated" // 重定向已新增
	MsgRedirectDeleted = "MsgRedirectDeleted" // 重定向已删除
	MsgRedirectMerged  = "MsgRedirectMerged"  // 重定向链已合并
)

// --- 批量操作的结论文案（页面回执，不是错误白名单）---
//
// 与其它模块的 Bulk* 同口径：值 = sys_i18n 的 item_key，**不带 Err / Msg 前缀** ——
// 这几句是 handler 按计数拼出的整句回执（经 shell.RenderJump 渲染进整页提示），
// 不是 service 错误，因此不进错误白名单。
//
// 中文原文留在 inbound/http/page_jump.go 与 pages_handle.go（key + 兜底的成对登记）
// （与写侧取词用的那份模板结构体绑在一起），这里只声明 key。
const (
	// 页面列表批量删除的四个结论分支。
	BulkPageNoneSelected = "page.bulk.pageNoneSelected"
	BulkPageAllDeleted   = "page.bulk.pageAllDeleted"
	BulkPageAllSkipped   = "page.bulk.pageAllSkipped"
	BulkPagePartial      = "page.bulk.pagePartial"
	// BulkPageMissingID 单条删除时缺少页面 id（参数级提示）。
	BulkPageMissingID = "page.bulk.pageMissingID"

	// 页面表单的必填校验回执（handler 自造，同样**不是** service 错误）。
	//
	// 中文原文留在 inbound/http/page_jump.go 的 pageMsg 里（写侧取词的唯一一份）。
	//
	// PageFormProjectNameRequired 新建站点工程时名称为空。
	PageFormProjectNameRequired = "page.form.projectNameRequired"
	// PageFormPathRequired 新建页面时路径为空。
	PageFormPathRequired = "page.form.pathRequired"

	// 重定向批量删除的四个结论分支（文案由服务端按计数重拼，进整页提示）——
	// key 化是为了让英文界面不永远显示中文。
	BulkRedirectNoneSelected = "page.bulk.redirectNoneSelected"
	BulkRedirectAllDeleted   = "page.bulk.redirectAllDeleted"
	BulkRedirectAllSkipped   = "page.bulk.redirectAllSkipped"
	BulkRedirectPartial      = "page.bulk.redirectPartial"
)

// MsgInternalError handler 内部错误统一兜底提示（禁止直出 err.Error() 泄露内部细节）。
const MsgInternalError = "MsgInternalError" // 系统内部错误，请稍后重试

// Page kind 白名单与 migration 080 pages_content_contract_check 对齐。
// product / category 已移除：商品页走 PresentationInstance 自动发布，不走手工 Page。
const (
	PageKindHome     = "home"
	PageKindPage     = "page"
	PageKindArticle  = "article"
	PageKindTag      = "tag"
	PageKindArchive  = "archive"
	PageKindSearch   = "search"
	PageKindNotFound = "notFound"
)

// PageKinds 返回全部合法 Page kind（字典序，确定性输出）。
func PageKinds() []string {
	return []string{
		PageKindArchive,
		PageKindArticle,
		PageKindHome,
		PageKindNotFound,
		PageKindPage,
		PageKindSearch,
		PageKindTag,
	}
}

// ValidatePageContentContract 校验 kind 与 content_target 组合是否符合 pages 表 CHECK。
func ValidatePageContentContract(kind, targetType string, targetID *string) bool {
	if targetID != nil && strings.TrimSpace(*targetID) == "" {
		return false
	}
	noTarget := targetType == "none" && targetID == nil
	hasTarget := func(expected string) bool {
		return targetType == expected && targetID != nil && strings.TrimSpace(*targetID) != ""
	}
	switch kind {
	case PageKindHome, PageKindArchive, PageKindSearch, PageKindNotFound:
		return noTarget
	case PageKindPage, PageKindArticle, PageKindTag:
		return hasTarget(kind)
	default:
		return false
	}
}

// errFallbacks 错误 key → 中文兜底（**与 sys_i18n 的 zh-CN 词条逐字一致**）。
//
// 兜底跟**声明处**走（pkg/facing.New），出口只调一次 facing.FactOf。词条库读不到时
// businessText 只能退回声明处给的那句话 —— 没有它，客户端拿到的是 `ErrPageLangExcluded` 这样的裸 key
// （这批常量的值就是常量名本身，旧式形态）。
//
// 逐字一致的理由：兜底只在词条缺席时可见，与词条不一致会让人以为「词条写坏了」。
var errFallbacks = map[string]string{
	ErrActionTargetInUse:        "这个目标仍被页面或区块引用，请先替换引用后再删除",
	ErrPageLangExcluded:         "该语言已从这一页的产出范围里排除，请先恢复再发布",
	ErrCannotExcludeDefaultLang: "站点默认语言不能排除：产物的原文就是它",
	ErrLangAlreadyExcluded:      "该语言已经被排除过了",
	ErrLangNotExcluded:          "该语言没有被排除，无需恢复",
	ErrInvalidParam:             "请求参数无效",
	ErrPageNotFound:             "页面不存在",
	ErrProjectNotFound:          "工程不存在",
	ErrProjectRequired:          "请先选择要操作的站点工程",
	ErrInvalidKind:              "不支持的页面类型",
	ErrInvalidDocument:          "页面草稿文档不合法",
	ErrInvalidPath:              "页面访问路径不合法",
	ErrDraftVersionConflict:     "草稿版本已更新，请刷新后重试",
	ErrPathOccupied:             "页面访问路径已被占用",
	ErrRevisionNotFound:         "修订版本不存在或已被清理",
	ErrBlueprintUnavailable:     "蓝图能力未接入，暂时无法从蓝图创建页面",
	ErrBlueprintInvalid:         "蓝图不存在或没有可用版本，请换一个蓝图",
	ErrNoStagedArtifact:         "无暂存产物，请先构建",
	ErrRollbackTargetMiss:       "回滚目标产物不存在",
	ErrRebuildRequired:          "草稿已变更，请重新构建后再发布",
	ErrInputMoved:               "编译期间内容已更新，本次发布已取消，请重新构建后再发布",
	ErrRebuildOverloaded:        "本轮需要重建的页面超过单批上限，其余已排入队列",
	ErrInvalidSlot:              "槽位键不在允许范围内，请回到列表页重新选择",
	ErrSlotPageMiss:             "要绑定的页面不存在、已被删除，或不属于当前工程",
	ErrRedirectUnavailable:      "重定向能力未接入，暂时无法管理重定向",
	ErrRedirectNotFound:         "该重定向不存在，可能已被删除",
	ErrRedirectOccupied:         "源路径已被占用，不能为它重复配置重定向",
	ErrRedirectTargetMiss:       "目标路径不存在或尚未激活，请先确认目标页面已发布",
	ErrRedirectLoop:             "该重定向会形成循环跳转，已拒绝",
	ErrRedirectConflict:         "该重定向刚被其它操作改动，请刷新后重试",
}

// Err 声明一条自带对外文案的业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// FallbackFor 兜底表取值（没有 error 对象、只有 key 的场景用：逐条原因、页面事实）。
// 未登记返回空串 —— 调用方据此回落自己的最后一句，不要在这里编一句通用文案。
func FallbackFor(key string) string { return errFallbacks[key] }

// ActionTargetInUse 在声明处固定被引用目标的冲突状态。
func ActionTargetInUse() error { return facing.WithStatus(Err(ErrActionTargetInUse), 409) }
