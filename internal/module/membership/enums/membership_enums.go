// Package membershipenums 统一管理 membership 模块的响应消息（BIZ-3）。
//
// 词条的**真源**是迁移 462a（sys_i18n），常量的值就是 item_key。
// 取值一律点分形态（membership.err.* / membership.msg.*）而不是裸 key（"ErrNotFound" 那种）：
// sys_i18n 的主键是 (item_key, lang)，裸 key 是全库共享的命名空间 ——
// navigation / block 都已经 seed 过 "ErrNotFound"，本模块再发同名 key 只会取到别人那句话
// （「导航项不存在」出现在会员页面上，没有任何报错）。点分命名空间是本仓较新的口径
// （cart / user 同形），裸 key 是历史形态，新模块不沿用。
package membershipenums

import (
	"fmt"
	"strings"

	"go_wp/pkg/facing"
)

// 成功回执（Msg* 常量名 + 点分 key，两套命名不可互换：老式形态的值就是常量名本身）。
const (
	MsgTierCreated      = "membership.msg.tierCreated"      // 等级已创建
	MsgTierUpdated      = "membership.msg.tierUpdated"      // 等级已更新
	MsgTierDeleted      = "membership.msg.tierDeleted"      // 等级已删除
	MsgEntitlementSaved = "membership.msg.entitlementSaved" // 权益已保存
	MsgAssignSet        = "membership.msg.assignSet"        // 已指定会员等级
	MsgAssignUnlocked   = "membership.msg.assignUnlocked"   // 已取消手工锁定
)

// 错误消息（值即 sys_i18n 的 item_key）。
const (
	// ErrInvalidParam 参数不合法（缺必填、格式错、超长）。
	ErrInvalidParam = "membership.err.invalidParam"
	// ErrNotFound 等级 / 归属不存在（作用域内查不到）。
	ErrNotFound = "membership.err.notFound"
	// ErrTierNameTaken 同工程内等级名已被占用（uq_membership_tiers_name）。
	ErrTierNameTaken = "membership.err.tierNameTaken"
	// ErrThresholdTaken 同工程内已有非默认等级用了同一门槛（uq_membership_tiers_threshold）。
	//
	// 撞门槛必须打回给人、不许静默：两个同门槛的档会让「满 1000 是白银还是黄金」
	// 取决于**查询顺序** —— 页面完全正常，只是同一个访客的等级会随索引扫描顺序漂移。
	ErrThresholdTaken = "membership.err.thresholdTaken"
	// ErrDefaultTierExists 该工程已有默认等级（uq_membership_tiers_default）。
	ErrDefaultTierExists = "membership.err.defaultTierExists"
	// ErrDefaultTierRequired 该工程必须**始终**有且只有一个默认等级，这一步会让它没有。
	//
	// 两个触发场景共用它：把唯一的默认等级改成非默认、以及删除默认等级而工程还有别的档。
	// 打回给人而不是自动把另一档提上来 —— 「哪一档该接替」是运营的决定，
	// 猜错会让所有未归属访客的等级在一夜之间变化而没人知道为什么。
	ErrDefaultTierRequired = "membership.err.defaultTierRequired"
	// ErrDefaultTierMissing 该工程没有默认等级 —— 读路径**不许**静默回退到 sort_order
	// 最小的那一档（它可能是运营已停用的），必须打回给人。
	// 后接「：<project_id>」，让操作者能直接定位是哪个工程缺配置。
	ErrDefaultTierMissing = "membership.err.defaultTierMissing"
	// ErrTierInUse 等级上还挂着会员归属，删除被拒（错误里带归属条数）。
	ErrTierInUse = "membership.err.tierInUse"
	// ErrEntitlementKind 权益类型不在本期支持的两种之内。
	ErrEntitlementKind = "membership.err.entitlementKind"
	// ErrEntitlementValue 权益取值越界（免运费只接受 0/1，折扣只接受 1..100）。
	//
	// 应用层先判一次不是为了替代数据库的 CHECK，而是为了给出**可行动**的说法：
	// 直接撞 CHECK 只会得到一句约束名。CHECK 仍在，是最后那道（脚本 / 手工 SQL 也绕不过）。
	ErrEntitlementValue = "membership.err.entitlementValue"
	// ErrThresholdInvalid 门槛取值不合法（负数）。
	ErrThresholdInvalid = "membership.err.thresholdInvalid"
	// ErrUserRequired 缺访客账号 id。
	ErrUserRequired = "membership.err.userRequired"
	// ErrProjectRequired 缺可作用域的工程 —— 等级与归属都按工程建键，
	// 没有工程就没有任何可查的范围。显式失败而不是静默返回「找不到」
	//（后者会把「没传工程」伪装成「这个等级不存在」）。
	ErrProjectRequired = "membership.err.projectRequired"
	// ErrManualNotLocked 该归属不是手工指定的，没有可解除的锁定。
	//
	// 单独一条而不是复用 ErrNotFound：手工解锁点了两次要能分辨「已解锁」与「本来就没有」，
	// 后者提示「已经解除了」是错的（运营会以为有人改过）。
	ErrManualNotLocked = "membership.err.manualNotLocked"
	// ErrRecalcUnavailable 归属重算不可用：消费额批量只读端口（订单侧的批量聚合）尚未注入。
	//
	// 显式报错而不是给一个 Scanned = 0 的结果：后者看起来像「大家都没消费」，
	// 而这两个状态的处置方式完全相反（一个等订单侧接线，一个去查订单状态口径）。
	ErrRecalcUnavailable = "membership.err.recalcUnavailable"
)

// 权益类型（与迁移 462 的 ck_membership_entitlements_value 一一对应）。
const (
	// KindFreeShipping 免运费：value_int 取 0 / 1。
	KindFreeShipping = "free_shipping"
	// KindDiscount 折扣：value_int 取 1..100，表示**扣减百分比**（20 = 打八折）。
	KindDiscount = "discount"
)

// 归属来源（与迁移 462 的 ck_membership_assignments_source 一一对应）。
const (
	// SourceAuto 日结按消费额重算得出。
	SourceAuto = "auto"
	// SourceManual 后台手工指定 —— 自动重算不会覆盖它。
	SourceManual = "manual"
)

// 展示词条（点分 key，值即 sys_i18n 的 item_key）。
//
// 这批不进白名单对账（不是 Err* / Msg* 前缀）：它们是**界面文案**，
// 由模板与后台页 handler 经 tr(key, 中文兜底) 取用。
//
// 词条 key 与模板字面量的**对应关系不在本文件**：来源显示名（admin.membership.assign.source.*）
// 与权益类型名（admin.membership.entitlement.kind.*）由模板按原始枚举值判分支后 tr() 取词
// （与 block 模块的 admin.blocks.kind.* 同形）。早先它们在这里各有一个常量，由 Go 侧取好词
// 再塞进行视图 —— 那样同一个值会在 Go 与模板两处各翻一次，且「页面用什么 key」这件事
// 被拆到两个包里。这里只保留 handler 真正要用的两个页面标题。
const (
	// PageTiersTitle / PageAssignmentsTitle 两个后台页的标题（传给模板的 title 键，
	// adminui.Prepare 的 injectI18n 会把它当 key 翻译；后台菜单的 title_key 另见 463）。
	PageTiersTitle       = "admin.membership.title"
	PageAssignmentsTitle = "admin.membership.assign.title"
)

// EntitlementKinds 本期支持的权益类型（上界封闭：新增一种要同时改这里、迁移 462 的 CHECK
// 与 462a 的词条，三处漏一处都会在运行时暴露）。
var EntitlementKinds = []string{KindFreeShipping, KindDiscount}

// errFallbacks 错误兜底（对外文案的中文那一半）：与常量同处声明，出口只调一次 pkg/facing.FactOf。
//
// 为什么不再有白名单（`var MembershipFacingMessages []string` + 逐条 switch）：
// 那是第二份真相 —— service 换了措辞、或某条错误改由别的模块提供时，白名单静默失配，
// 症状是把业务文案吞成归口、或把内部错误当文案透出，两边都不报错。
// 现在「能不能对外说」由 facing.Fact 的形状判定，「说什么」由这里的一句话给。
var errFallbacks = map[string]string{
	ErrInvalidParam:        "参数不合法：请检查后重试。",
	ErrNotFound:            "会员记录不存在：可能已被删除。",
	ErrTierNameTaken:       "等级名已被占用：同工程内不能重名。",
	ErrThresholdTaken:      "门槛值已被占用：同一工程内两个等级不能同门槛。",
	ErrDefaultTierExists:   "该工程已有默认等级：请先取消原有的默认等级。",
	ErrDefaultTierRequired: "该工程必须保留一个默认等级：请先把另一档设为默认。",
	ErrDefaultTierMissing:  "该工程还没有默认等级：请先在等级设置里指定一个。",
	ErrTierInUse:           "等级上还挂着会员归属：请先转移或解除这些归属再删除。",
	ErrEntitlementKind:     "权益类型不在支持范围内：只支持免运费与折扣。",
	ErrEntitlementValue:    "权益取值越界：免运费只接受 0/1，折扣只接受 1..100。",
	ErrThresholdInvalid:    "门槛值不合法：不能是负数。",
	ErrUserRequired:        "缺少会员账号：请重新选择后再操作。",
	ErrProjectRequired:     "缺少工程：等级与归属都按工程划分，请先选定工程。",
	ErrManualNotLocked:     "该归属不是手工指定的：没有可解除的锁定。",
	ErrRecalcUnavailable:   "归属重算暂不可用：订单侧消费额聚合尚未就绪，请稍后重试。",
}

// Err 声明一条自带对外文案的会员业务错误（兜底与本表同源）。
func Err(key string) error { return facing.New(key, errFallbacks[key]) }

// ErrDetail 把业务错误与**定位数据**拼成一条（key + 明细）。
//
// 明细是给操作者用的数据（哪个 project_id、哪个等级名、还剩几条归属），由本模块自己构造，
// 不含表名 / SQLSTATE。分隔符必须用 ASCII「: 」：pkg/facing 的复合形态判据只认这一种，
// 写成全角「：」会让整条过不了判据、被当成内部错误打成 500。
func ErrDetail(key, detail string) error {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return Err(key)
	}
	return fmt.Errorf("%w: %s", Err(key), detail)
}
