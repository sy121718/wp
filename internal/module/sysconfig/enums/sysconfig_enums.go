// Package sysconfigenums 统一管理 sysconfig 模块的响应消息（模块必选目录）。
//
// 形态沿用 navigation / membership 的既有口径：老式常量「常量名即资源 key」
// （sys_i18n 里存同名 key），归口文案带模块前缀的点分 key。未接好 i18n 时，
// 调用点按 key 取词失败即回落到中文兜底。
package sysconfigenums

import "go_wp/pkg/facing"

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
)

// 页面标题（layout 的 <title>）：值为 sys_i18n key，词条由迁移 520 seed。
const (
	// AdminSystemTitle 系统设置页标题（模板 admin/system/settings.html）。
	AdminSystemTitle = "admin.system.title"
)

// 设置页写路径与参数校验的用户可见文案（值即 sys_i18n key）。
const (
	// AdminSystemSaved 保存成功回执（页面出口的成品句）。
	AdminSystemSaved = "admin.system.saved"
	// AdminSystemGroupMissing 配置分组缺失（迁移未执行）—— 整页可读提示。
	AdminSystemGroupMissing = "admin.system.group_missing"
	// AdminSystemErrLangUnknown 提交的默认语言不在字典里。
	AdminSystemErrLangUnknown = "admin.system.err.lang_unknown"
	// AdminSystemErrCurrencyUnknown 提交的默认货币不在字典里。
	AdminSystemErrCurrencyUnknown = "admin.system.err.currency_unknown"
	// AdminSystemErrCountryUnknown 提交的默认国家不在字典里。
	AdminSystemErrCountryUnknown = "admin.system.err.country_unknown"
)

// errFallbacks 业务错误的**中文兜底**：声明处与 key 成对给出，出口只调一次共享判据
// pkg/facing.FactOf。
//
// 为什么绑在声明处：兜底原文写在出口那一层时，「加一条业务错误」要改两个包，而第二个包
// （inbound/http）在 service 里完全看不见 —— 二开的人加完常量，页面上显示的是裸 key，
// 还不知道该去哪补那句话。原先的那份 SysConfigFacingMessages 白名单就是这个形态，
// 已删除：可透出性判据只有一份（pkg/facing.IsBusinessKey），本表只回答「显示什么」。
//
// 表与本人只增不减：新增一个 Err* 常量必须同时给一句中文，由 sysconfig_enums_test.go 对账。
var errFallbacks = map[string]string{
	ErrInvalidParam:    "参数不合法：请检查后重试。",
	ErrVersionRequired: "缺少版本号：整组保存必须带上当前版本，避免覆盖别人的改动。",
	ErrGroupNotFound:   "配置分组不存在",
	ErrVersionConflict: "配置已被其他管理员改过：请刷新后重新提交。",
}

// Err 声明一条自带对外文案的业务错误。
//
// 复合错误用 fmt.Errorf("%w: %s", Err(key), 明细)：facing.FactOf 按 ASCII 冒号切出明细段，
// 并保留 errors.Is / errors.As 能拿到的 key 与兜底。
func Err(key string) error {
	return facing.New(key, errFallbacks[key])
}
