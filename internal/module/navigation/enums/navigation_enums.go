// Package navigationenums 统一管理 navigation 模块响应消息（0-C）。
package navigationenums

import "strings"

// 成功消息（未接 i18n 前中文常量）。
const (
	MsgCreateSuccess = "MsgCreateSuccess" // 导航项创建成功
	MsgUpdateSuccess = "MsgUpdateSuccess" // 导航项更新成功
	MsgListSuccess   = "MsgListSuccess"   // 导航列表获取成功
	MsgDetailSuccess = "MsgDetailSuccess" // 导航详情获取成功
	MsgDeleteSuccess = "MsgDeleteSuccess" // 导航项删除成功
)

// 错误消息。
const (
	ErrInvalidParam = "ErrInvalidParam" // 参数错误
	ErrNotFound     = "ErrNotFound"     // 导航项不存在
	ErrInvalidKind  = "ErrInvalidKind"  // 非法的导航类型，仅支持 header 或 footer
	ErrPathTaken    = "ErrPathTaken"    // 同工程同类型下已存在相同路径的导航项
	// ErrInvalidSource 非法的菜单项来源（或非 custom 来源未指定来源实体）。
	ErrInvalidSource = "ErrInvalidSource" // 非法的菜单项来源，仅支持 custom/page/article/product/category/block，且非 custom 必须指定来源实体
	// ErrInvalidTarget 非法的打开方式。
	ErrInvalidTarget = "ErrInvalidTarget" // 非法的打开方式，仅支持 self 或 blank
	// ErrInvalidParent 非法的父引用（自引用 / 成环 / 跨工程 / 跨类型 / 父项不存在）。
	ErrInvalidParent = "ErrInvalidParent" // 非法的父引用：父项必须存在、同工程、同类型，且不得形成环
	// ErrProjectRequired 缺可作用域的工程（DB-009）：只带 id 的入口要逐工程定位归属，
	// 而工程清单为空或读不到。显式失败而不是静默返回「找不到」——后者会把
	// 「读不到工程表」伪装成「导航项不存在」。
	ErrProjectRequired = "ErrProjectRequired" // 缺少可作用域的工程，无法定位导航项的工程归属
	// ErrStaleVersion 乐观锁冲突：调用方手上的 update_time 与库内当前值不一致。
	//
	// 后台菜单页与工作台检查器是同一份数据的两个入口，两处同时改此前是静默覆盖
	//（last-write-wins）。冲突一律**打回给人**：不自动合并、不加后缀、不丢弃其中一方。
	// 文案后接「：<菜单项标题>」的定位信息（形态与 shell.FacingNotice 的形态 3 对齐：
	// 页面路径经 ?err= 回带时靠「候选文案 + ：」命中，所以本词条内不带 %s 占位符）。
	ErrStaleVersion = "ErrStaleVersion"
	// ErrInternal 未归类的内部错误（SQL / 表名 / 约束名 / 文件路径等）对外归口文案。
	//
	// 值刻意带模块前缀：sys_i18n 的主键是 (item_key, lang)，裸 key "ErrInternal" 已被
	// admin 批占用（迁移 268）；形态与 cart / user 的 "cart.err.internal" 一致。
	ErrInternal = "navigation.err.internal" // 操作失败，请稍后重试（细节只进日志）
)

// NavigationFacingMessages 可以原样展示给前端的导航业务文案（**白名单**）。
//
// 命中 → 原样透出（前端据此提示「哪一项不合法」「路径已被占用」）；
// 未命中 → inbound/http 的 navigationErrText 记结构化日志并返回 ErrInternal。
// 方向是安全的：漏写一个常量只会让前端看到一句通用提示（一眼可见），
// 而不会把 service 上抛的 PostgreSQL 原文（表名 / 约束名 / SQLSTATE）透出去。
//
// ErrInternal 本身不进白名单：它是未命中时的返回值，不是业务文案。
var NavigationFacingMessages = []string{
	ErrInvalidParam, ErrNotFound, ErrInvalidKind, ErrPathTaken,
	ErrInvalidSource, ErrInvalidTarget, ErrInvalidParent, ErrProjectRequired,
	ErrStaleVersion,
}

// —— 白名单的两种读法（本模块内共享，别在各处再抄一份判据）——
//
// 白名单命中判定与「key + 定位信息」的拆分此前只存在于 inbound/http 的一处循环里；
// 消费者侧（工作台检查器要就地建菜单项）需要的正是同一份判据 —— 它拿不到本模块的
// enums，只能经 contract 的出口取文案。把判据放在白名单旁边，两侧就不会各判一套。

// FacingDetailSep 业务文案与定位信息的分隔符（写侧固定用全角「：」）。
// 半角「: 」只出现在读侧（shell.FacingNotice 的形态 3 两种都认）。
const FacingDetailSep = "："

// HitFacingMessage 判定错误文本是否命中白名单，命中返回原文。
// 三种形态：整串相等 / key|param 带参 / key：<定位信息>。
func HitFacingMessage(msg string) (string, bool) {
	for _, m := range NavigationFacingMessages {
		if msg == m || strings.HasPrefix(msg, m+"|") ||
			strings.HasPrefix(msg, m+FacingDetailSep) || strings.HasPrefix(msg, m+": ") {
			return msg, true
		}
	}
	return "", false
}

// SplitFacingDetail 从白名单文案里拆出「key + 定位信息」；不是该形态返回 ok=false。
func SplitFacingDetail(msg string) (key, detail string, ok bool) {
	for _, m := range NavigationFacingMessages {
		if rest, found := strings.CutPrefix(msg, m+FacingDetailSep); found {
			return m, rest, true
		}
		if rest, found := strings.CutPrefix(msg, m+": "); found {
			return m, rest, true
		}
	}
	return "", "", false
}
