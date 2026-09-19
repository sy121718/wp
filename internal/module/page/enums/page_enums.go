// Package pageenums 管理 page 模块业务消息。
package pageenums

import "strings"

const (
	// ErrInvalidParam 请求本身不合法（nil 请求、空/空白 ID 等），与资源存在性无关。
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
	// Name 展示名。
	Name string
	// Usage 这个槽位会被谁用到（写在后台页上，省得作者猜「绑了有什么用」）。
	Usage string
}

// SiteSlotDefs 全部槽位，顺序即后台展示顺序。
var SiteSlotDefs = []SiteSlotDef{
	{SiteSlotShop, "商品列表", "商品卡与导航的「全部商品」指向它"},
	{SiteSlotBlog, "文章列表", "文章列表页"},
	{SiteSlotCart, "购物车", "承载 cartView 片段；加购后跳转到这里"},
	{SiteSlotCheckout, "结算", "承载 checkout 片段；购物车的「去结算」指向它"},
	{SiteSlotAccount, "个人中心", "访客账号中心"},
	{SiteSlotLogin, "登录", "登录页；需要登录时跳转到这里"},
	{SiteSlotRegister, "注册", "注册页；登录页的「注册」指向它"},
	{SiteSlotForgot, "忘记密码", "申请重置密码的页面"},
	{SiteSlotReset, "重置密码", "带 token 的重置密码页"},
	{SiteSlotOrders, "我的订单", "访客订单列表（可与个人中心同页）"},
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

// SiteSlotName 槽位展示名（未知键回退键本身，便于排查）。
func SiteSlotName(key string) string {
	for _, d := range SiteSlotDefs {
		if d.Key == key {
			return d.Name
		}
	}
	return key
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
