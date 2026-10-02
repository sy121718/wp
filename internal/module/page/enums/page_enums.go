// Package pageenums 管理 page 模块业务消息。
package pageenums

import "strings"

const (
	// ErrInvalidParam 请求本身不合法（nil 请求、空/空白 ID 等），与资源存在性无关。
	// —— 页面级语言排除（迁移 491）——
	//
	// 四条哨兵的取值即 i18n key（与其它 page 错误同形）；未接好词条前，后台页在调用点
	// 给中文兜底（page_langs_handle.go 的 pageLangErrText），不显示裸 key。
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

	// 系统页面槽位（BIZ-1）。
	ErrInvalidSlot  = "ErrInvalidSlot"  // 槽位键不在白名单内
	ErrSlotPageMiss = "ErrSlotPageMiss" // 槽位要绑的页面不存在或不属于本工程

	// 重定向管理（审计 SEO-025）。
	ErrRedirectUnavailable = "ErrRedirectUnavailable" // 路由能力未接入，无法管理重定向
	ErrRedirectNotFound    = "ErrRedirectNotFound"    // 这条重定向不存在
	ErrRedirectOccupied    = "ErrRedirectOccupied"    // 源路径已被占用
	ErrRedirectTargetMiss  = "ErrRedirectTargetMiss"  // 目标路径不存在或未激活
	ErrRedirectLoop        = "ErrRedirectLoop"        // 重定向成环
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
// 这几句是 handler 按计数拼出的整句回执（进 ?done= / ?err=），不是 service 错误，
// 因此不进 pageFacingKeys（那一张是「业务错误能不能原样透出」的白名单）。
//
// 中文原文留在 inbound/http/page_err.go 与 page_redirect_handle.go
// （与写读共用的那份模板结构体绑在一起），这里只声明 key。
const (
	// 页面列表批量删除的四个结论分支（四条都是读侧候选，走 ?done= / ?err=）。
	BulkPageNoneSelected = "page.bulk.pageNoneSelected"
	BulkPageAllDeleted   = "page.bulk.pageAllDeleted"
	BulkPageAllSkipped   = "page.bulk.pageAllSkipped"
	BulkPagePartial      = "page.bulk.pagePartial"
	// BulkPageMissingID 单条删除时缺少页面 id（?err= 上的参数级提示）。
	BulkPageMissingID = "page.bulk.pageMissingID"

	// 页面表单的必填校验回执（handler 自造、进 ?err=，同样**不是** service 错误）。
	//
	// 中文原文留在 inbound/http/page_err.go 的候选结构体里（写侧取词与读侧候选共用那一份，
	// 两处各抄一份的下场是词条一改措辞候选就静默失配）。
	//
	// PageFormProjectNameRequired 新建站点工程时名称为空。
	PageFormProjectNameRequired = "page.form.projectNameRequired"
	// PageFormPathRequired 新建页面时路径为空。
	PageFormPathRequired = "page.form.pathRequired"

	// 重定向批量删除的四个结论分支。它走的是 ?ok=bulk&dn=N&sk=M 计数回带
	//（文案由服务端按计数重拼），伪造面比列表页的 ?done= 少一层，但同样要 key 化 ——
	// 否则英文界面上这四个分支永远显示中文。
	BulkRedirectNoneSelected = "page.bulk.redirectNoneSelected"
	BulkRedirectAllDeleted   = "page.bulk.redirectAllDeleted"
	BulkRedirectAllSkipped   = "page.bulk.redirectAllSkipped"
	BulkRedirectPartial      = "page.bulk.redirectPartial"
)

// 系统页面槽位：把「结算页是哪一页」这类事实固定下来（BIZ-1）。
//
// 键名会进数据库、API 参数与后台表单 —— **新增可以，改名等于破坏既有绑定**
// （迁移 138 的 CHECK 约束里另有一份白名单，两处必须同步）。
const (
	SiteSlotShop     = "shop"
	SiteSlotBlog     = "blog"
	SiteSlotCart     = "cart"
	SiteSlotCheckout = "checkout"
	SiteSlotAccount  = "account"
	SiteSlotLogin    = "login"
	SiteSlotRegister = "register"
	SiteSlotForgot   = "forgot"
	SiteSlotReset    = "reset"
	SiteSlotOrders   = "orders"
)

// SiteSlotDef 槽位定义（后台按这个顺序与名称渲染）。
type SiteSlotDef struct {
	Key string
	// NameKey 展示名的 i18n key；Name 是**中文兜底**（词条缺失时页面上显示的原文）。
	//
	// 为什么表里同时留 key 与兜底：本表在 enums 包里、没有请求语言，
	// contract 的 SiteSlotItem.SlotName 承载的是 **key**（见 page/service/page_slot.go），
	// 取词落在后台页面层（page/inbound/http/site_slot_handle.go）。把中文写进 contract
	// 就等于把一种语言焊进跨模块数据。
	NameKey string
	Name    string
	// UsageKey 用途说明的 i18n key；Usage 是中文兜底。
	UsageKey string
	Usage    string
}

// SiteSlotDefs 全部槽位，顺序即后台展示顺序。
var SiteSlotDefs = []SiteSlotDef{
	{Key: SiteSlotShop, NameKey: "admin.site_slots.slot.shop.name", Name: "商品列表",
		UsageKey: "admin.site_slots.slot.shop.usage", Usage: "商品卡与导航的「全部商品」指向它"},
	{Key: SiteSlotBlog, NameKey: "admin.site_slots.slot.blog.name", Name: "文章列表",
		UsageKey: "admin.site_slots.slot.blog.usage", Usage: "文章列表页"},
	{Key: SiteSlotCart, NameKey: "admin.site_slots.slot.cart.name", Name: "购物车",
		UsageKey: "admin.site_slots.slot.cart.usage", Usage: "承载 cartView 片段；加购后跳转到这里"},
	{Key: SiteSlotCheckout, NameKey: "admin.site_slots.slot.checkout.name", Name: "结算",
		UsageKey: "admin.site_slots.slot.checkout.usage", Usage: "承载 checkout 片段；购物车的「去结算」指向它"},
	{Key: SiteSlotAccount, NameKey: "admin.site_slots.slot.account.name", Name: "个人中心",
		UsageKey: "admin.site_slots.slot.account.usage", Usage: "访客账号中心"},
	{Key: SiteSlotLogin, NameKey: "admin.site_slots.slot.login.name", Name: "登录",
		UsageKey: "admin.site_slots.slot.login.usage", Usage: "登录页；需要登录时跳转到这里"},
	{Key: SiteSlotRegister, NameKey: "admin.site_slots.slot.register.name", Name: "注册",
		UsageKey: "admin.site_slots.slot.register.usage", Usage: "注册页；登录页的「注册」指向它"},
	{Key: SiteSlotForgot, NameKey: "admin.site_slots.slot.forgot.name", Name: "忘记密码",
		UsageKey: "admin.site_slots.slot.forgot.usage", Usage: "申请重置密码的页面"},
	{Key: SiteSlotReset, NameKey: "admin.site_slots.slot.reset.name", Name: "重置密码",
		UsageKey: "admin.site_slots.slot.reset.usage", Usage: "带 token 的重置密码页"},
	{Key: SiteSlotOrders, NameKey: "admin.site_slots.slot.orders.name", Name: "我的订单",
		UsageKey: "admin.site_slots.slot.orders.usage", Usage: "访客订单列表（可与个人中心同页）"},
}

// IsSiteSlot 判断键是否在槽位白名单内。
func IsSiteSlot(key string) bool {
	for _, d := range SiteSlotDefs {
		if d.Key == key {
			return true
		}
	}
	return false
}

// SiteSlotName 槽位展示名的**中文兜底**（未知 key 回退 key 本身，便于排查）。
//
// 入参是展示名的 i18n key（SiteSlotDef.NameKey）而不是槽位键 —— 名字与用途两条
// 兜底文案都只在本表写一次，取词的调用点（page/inbound/http/site_slot_handle.go）
// 只负责翻，不负责再抄一份中文。
func SiteSlotName(nameKey string) string {
	for _, d := range SiteSlotDefs {
		if d.NameKey == nameKey {
			return d.Name
		}
	}
	return nameKey
}

// SiteSlotUsage 槽位用途的中文兜底（未知 key 回退 key 本身）。
func SiteSlotUsage(usageKey string) string {
	for _, d := range SiteSlotDefs {
		if d.UsageKey == usageKey {
			return d.Usage
		}
	}
	return usageKey
}

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

// —— 定时上下线（PIPE-7）——
//
// 业务错误 key：与其它 Err* 同族（值即 i18n key，读侧取词后展示）。
// 到点执行失败时的 last_error 也存这一族的 key（见 ScheduleFailureFallbacks）——
// page_schedules.last_error 会显示在后台，存原文等于把内部错误摆到页面上。
const (
	// ErrScheduleNotFound 这条排定不存在（或不属于该页面）。
	ErrScheduleNotFound = "ErrScheduleNotFound" // 排定不存在
	// ErrScheduleInPast 排定时间必须晚于当前时刻（「到点」已经过去 = 这次排定永远不会执行）。
	ErrScheduleInPast = "ErrScheduleInPast" // 排定时间必须晚于当前时刻
	// ErrScheduleActionInvalid 排定动作不在白名单内（只接受 publish / offline）。
	ErrScheduleActionInvalid = "ErrScheduleActionInvalid" // 排定动作不合法
	// ErrScheduleRunning 这条排定正在执行（已被认领），无法取消。
	ErrScheduleRunning = "ErrScheduleRunning" // 排定正在执行，无法取消
	// ErrScheduleOccupied 该页面该语言已有待执行 / 执行中的同类排定。
	//
	// 与「重排」的区别：同动作的 pending 会被新排定**取代**（旧决策作废，不报错），
	// 只有 running 才拒绝 —— 执行者手上还拿着那一条的租约与快照。
	ErrScheduleOccupied = "ErrScheduleOccupied" // 同类排定正在执行，请稍后再试
	// ErrScheduleApplyFailed 到点执行失败的**归口** key（内部错误统一用它，
	// 原文只进日志；具体可归因的失败用更精确的 key，如 ErrRebuildRequired）。
	ErrScheduleApplyFailed = "ErrScheduleApplyFailed" // 排定执行失败，请查看服务日志
)

const (
	// MsgScheduleSet 排定成功（到点自动执行）。
	MsgScheduleSet = "MsgScheduleSet" // 已排定，到点自动执行
	// MsgScheduleCanceled 排定已取消。
	MsgScheduleCanceled = "MsgScheduleCanceled" // 排定已取消
)

// ScheduleFailureFallbacks 到点执行失败的 last_error 词条 → 中文兜底。
//
// 与 ErrDetailFallbacks（page_err_detail.go）同形：键是写侧写进 page_schedules.last_error 的
// 取值集合，值是 i18n 未初始化 / 词条缺失时页面上显示的原文。
//
// **两个消费者共用这一份**：service 写 last_error 时只写 key（不写原文），
// 后台读侧（pages_handle.go 的排定投影）用本表取词 —— 两边各抄一份清单的下场是
// 「新增一个失败原因，页面上显示成裸 key」且不报错。
//
// 取值刻意全是既有业务错误 key（不加新词条即可用）：到点失败的两类成因就是
// 「产物不再代表当前草稿」与「访问面/占用出问题」，它们的文案早已存在。
var ScheduleFailureFallbacks = map[string]string{
	ErrRebuildRequired:     "草稿已变更，请重新构建后再发布",
	ErrNoStagedArtifact:    "无暂存产物，请先构建",
	ErrPathOccupied:        "页面访问路径已被占用",
	ErrPageNotFound:        "页面不存在",
	ErrScheduleApplyFailed: "排定执行失败，请查看服务日志",
}
