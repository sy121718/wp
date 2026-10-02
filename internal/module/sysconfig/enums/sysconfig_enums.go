// Package sysconfigenums 统一管理 sysconfig 模块的响应消息（模块必选目录）。
//
// 形态沿用 navigation / membership 的既有口径：老式常量「常量名即资源 key」
// （sys_i18n 里存同名 key），归口文案带模块前缀的点分 key。未接好 i18n 时，
// 调用点按 key 取词失败即回落到中文兜底。
package sysconfigenums

// 成功消息。
const (
	// MsgGroupSaved 配置保存成功。
	MsgGroupSaved = "MsgGroupSaved"
	// MsgListSuccess 配置列表获取成功。
	MsgListSuccess = "MsgListSuccess"
	// MsgDetailSuccess 配置详情获取成功。
	MsgDetailSuccess = "MsgDetailSuccess"
)

// 错误消息。
const (
	// ErrInvalidParam 参数错误（分组键为空 / 数据为空）。
	ErrInvalidParam = "ErrInvalidParam"
	// ErrVersionRequired 缺少乐观锁版本号：不带版本的整组保存会被拒绝，
	// 而不是退化成「覆盖别人的改动」。
	ErrVersionRequired = "ErrVersionRequired"
	// ErrGroupNotFound 配置分组不存在（本模块不凭空建组：组由 seed 建立，
	// 后台保存只能改已存在的组）。
	ErrGroupNotFound = "ErrGroupNotFound"
	// ErrVersionConflict 乐观锁冲突：调用方手里的 version 与库内当前值不一致。
	//
	// 冲突一律**打回给人**：不自动合并、不静默覆盖、不重试 —— 两个管理员改同一组
	// 的不同键时，后写者静默胜出正是本列要消灭的故障（迁移 484 文件头）。
	ErrVersionConflict = "ErrVersionConflict"
	// ErrInternal 未归类的内部错误（SQL / 表名 / 约束名等）对外归口文案。
	//
	// 值刻意带模块前缀：sys_i18n 的主键是 (item_key, lang)，裸 key "ErrInternal"
	// 已被别的批占用。
	ErrInternal = "sysconfig.err.internal" // 操作失败，请稍后重试（细节只进日志）
)

// SysConfigFacingMessages 可以原样展示给前端的业务文案（**白名单**）。
//
// 命中 → 原样透出；未命中 → inbound 的归口助手记结构化日志并返回 ErrInternal。
// 方向是安全的：漏写一条只会让前端看到一句通用提示（一眼可见），而不会把 PostgreSQL
// 原文（表名 / 约束名 / SQLSTATE）透出去。ErrInternal 本身不进白名单 —— 它是未命中时的
// 返回值，不是业务文案。对账测试见 sysconfig_enums_test.go。
var SysConfigFacingMessages = []string{
	ErrInvalidParam, ErrVersionRequired, ErrGroupNotFound, ErrVersionConflict,
}
