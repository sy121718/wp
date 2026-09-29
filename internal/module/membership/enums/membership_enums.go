// Package membershipenums 统一管理 membership 模块的响应消息（BIZ-3）。
//
// 词条的**真源**是迁移 462a（sys_i18n），常量的值就是 item_key。
// 取值一律点分形态（membership.err.* / membership.msg.*）而不是裸 key（"ErrNotFound" 那种）：
// sys_i18n 的主键是 (item_key, lang)，裸 key 是全库共享的命名空间 ——
// navigation / block 都已经 seed 过 "ErrNotFound"，本模块再发同名 key 只会取到别人那句话
// （「导航项不存在」出现在会员页面上，没有任何报错）。点分命名空间是本仓较新的口径
// （cart / user 同形），裸 key 是历史形态，新模块不沿用。
package membershipenums

import "strings"

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
	// ErrInternal 未归类的内部错误（SQL / 表名 / 约束名 / 索引名等）对外归口文案。
	//
	// 值带模块前缀（点分）：裸 "ErrInternal" 已被 admin 批占用（迁移 268）。
	ErrInternal = "membership.err.internal"
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
const (
	// SourceLabelAuto / SourceLabelManual 归属来源的展示名（后台归属列表与筛选下拉）。
	SourceLabelAuto   = "admin.membership.assign.source.auto"
	SourceLabelManual = "admin.membership.assign.source.manual"
	// PageTiersTitle / PageAssignmentsTitle 两个后台页的标题（传给模板的 title 键，
	// shell.Prepare 的 injectI18n 会把它当 key 翻译；后台菜单的 title_key 另见 463）。
	PageTiersTitle       = "admin.membership.title"
	PageAssignmentsTitle = "admin.membership.assign.title"
	// KindLabelFreeShipping / KindLabelDiscount 权益类型的展示名（等级列表的权益列）。
	KindLabelFreeShipping = "admin.membership.entitlement.kind.freeShipping"
	KindLabelDiscount     = "admin.membership.entitlement.kind.discount"
)

// EntitlementKinds 本期支持的权益类型（上界封闭：新增一种要同时改这里、迁移 462 的 CHECK
// 与 462a 的词条，三处漏一处都会在运行时暴露）。
var EntitlementKinds = []string{KindFreeShipping, KindDiscount}

// MembershipFacingMessages 可以原样展示给前端的会员业务文案（**白名单**）。
//
// 命中 → 原样透出（前端据此提示「门槛撞了」「这个工程没配默认等级」）；
// 未命中 → inbound/http 的 membershipErrText 记结构化日志并返回 ErrInternal。
// 方向是安全的：漏写一条只会让前端看到一句通用提示（一眼可见），
// 而不会把 service 上抛的 PostgreSQL 原文（表名 / 约束名 / SQLSTATE）透出去。
//
// ErrInternal 本身不进白名单：它是未命中时的返回值，不是业务文案。
// Msg* 也不进：它们是成功回执，永远不会作为错误响应返回（对账测试里逐条写明理由）。
var MembershipFacingMessages = []string{
	ErrInvalidParam, ErrNotFound, ErrTierNameTaken, ErrThresholdTaken,
	ErrDefaultTierExists, ErrDefaultTierRequired, ErrDefaultTierMissing, ErrTierInUse,
	ErrEntitlementKind, ErrEntitlementValue, ErrThresholdInvalid,
	ErrUserRequired, ErrProjectRequired, ErrManualNotLocked, ErrRecalcUnavailable,
}

// FacingDetailSep 业务文案与定位信息的分隔符（写侧固定用全角「：」）。
//
// 半角「: 」只出现在读侧（shell.FacingNotice 的形态 3 两种都认）——写读两侧都不必
// 因为语言切换而失配。
const FacingDetailSep = "："

// WithDetail 把业务 key 与定位信息拼成一条可回带的文案（key：<定位>）。
//
// 定位信息是**给操作者用的数据**（哪个 project_id、哪个等级名、还剩几条归属），
// 不是内部错误原文：它由本模块自己构造，不含表名 / SQLSTATE。原文只进日志。
func WithDetail(key, detail string) string {
	detail = strings.TrimSpace(detail)
	if detail == "" {
		return key
	}
	return key + FacingDetailSep + detail
}

// HitFacingMessage 判定错误文本是否命中白名单，命中返回原文。
// 三种形态：整串相等 / key|param 带参 / key：<定位信息>（半角「: 」也认，兼容读侧归一）。
func HitFacingMessage(msg string) (string, bool) {
	for _, m := range MembershipFacingMessages {
		if msg == m || strings.HasPrefix(msg, m+"|") ||
			strings.HasPrefix(msg, m+FacingDetailSep) || strings.HasPrefix(msg, m+": ") {
			return msg, true
		}
	}
	return "", false
}

// SplitFacingDetail 从白名单文案里拆出「key + 定位信息」；不是该形态返回 ok=false。
func SplitFacingDetail(msg string) (key, detail string, ok bool) {
	for _, m := range MembershipFacingMessages {
		if rest, found := strings.CutPrefix(msg, m+FacingDetailSep); found {
			return m, rest, true
		}
		if rest, found := strings.CutPrefix(msg, m+": "); found {
			return m, rest, true
		}
	}
	return "", "", false
}
