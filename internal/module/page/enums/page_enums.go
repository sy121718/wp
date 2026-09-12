// Package pageenums 管理 page 模块业务消息。
package pageenums

const (
	// ErrInvalidParam 请求本身不合法（nil 请求、空/空白 ID 等），与资源存在性无关。
	ErrInvalidParam         = "ErrInvalidParam"         // 请求参数无效
	ErrPageNotFound         = "ErrPageNotFound"         // 页面不存在
	ErrProjectNotFound      = "ErrProjectNotFound"      // 站点工程不存在
	ErrInvalidKind          = "ErrInvalidKind"          // 页面类型与内容目标不匹配
	ErrInvalidDocument      = "ErrInvalidDocument"      // 页面草稿文档不合法
	ErrInvalidPath          = "ErrInvalidPath"          // 页面访问路径不合法
	ErrDraftVersionConflict = "ErrDraftVersionConflict" // 草稿版本已更新，请刷新后重试
	ErrPathOccupied         = "ErrPathOccupied"         // 页面访问路径已被占用
	ErrNoStagedArtifact     = "ErrNoStagedArtifact"     // 无暂存产物，请先构建
	ErrRollbackTargetMiss   = "ErrRollbackTargetMiss"   // 回滚目标产物不存在
	ErrRebuildRequired      = "ErrRebuildRequired"      // 草稿已变更，请重新构建后再发布

	// 系统页面槽位（BIZ-1）。
	ErrInvalidSlot  = "ErrInvalidSlot"  // 槽位键不在白名单内
	ErrSlotPageMiss = "ErrSlotPageMiss" // 槽位要绑的页面不存在或不属于本工程
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
