// Package adminenums admin 模块（管理员/角色/权限点/菜单/部门）的业务消息。
// 常量值 = sys_i18n 稳定资源 key；注释保留中文（内置默认值/开发期可读）。
package adminenums

// --- 通用 ---

const (
	MsgSuccess      = "msg_operation_success" // 操作成功
	MsgBadRequest   = "ErrInvalidParams"      // 请求参数错误
	MsgUnauthorized = "ErrUnauthorized"       // 未登录或登录已过期
	// ErrInternal 未归类的内部错误（SQL / 约束名 / 文件路径等）对外归口文案。
	// 细节只进日志：handler 不再把 err.Error() 拼进响应（审计 CQ-009/CQ-010 的 admin 收口）。
	// 与 user / cart 的 user.err.internal / cart.err.internal 同义；本模块其余常量沿用
	// 「常量名即资源 key」的形态，所以这里保持同形（形态判定见 pkg/response.IsBusinessError）。
	ErrInternal = "ErrInternal"
)

// AdminFacingMessages 可以原样展示给前端的管理面业务文案（**白名单**）。
//
// 存在的理由：service 的业务错误全部来自本包，而基础设施错误的原文
// （PostgreSQL 的 23505、约束名 uk_sys_role_code、表名、文件路径）不该出网。
// handler 侧的归口助手（internal/module/admin/inbound/http/admin_err.go）拿这张表做白名单：
// 命中 → 原样透出（前端据此提示「哪一项不合法」）；未命中 → 记日志 + ErrInternal 归口文案。
//
// 白名单而不是黑名单：漏写只会让前端看到一句通用提示（一眼可见，且
// admin_enums_test.go 会按本文件逐个常量对账），黑名单漏写则会把内部细节摆到页面上。
//
// 不含三类：成功文案（MsgSuccess / MsgLogoutSuccess，永远不会作为错误返回）、
// 归口文案 ErrInternal（它是未命中时的返回值，不是业务文案）、以及任何非字符串常量。
var AdminFacingMessages = []string{
	// 通用
	MsgBadRequest, MsgUnauthorized,
	// 管理员
	ErrCaptchaExpired, ErrBadCredentials, ErrAccountLocked, ErrAccountDisabled,
	ErrAdminNotFound, ErrSuperAdminExists, ErrFieldProtected, ErrEmailExists,
	ErrUsernameExists, ErrPhoneExists, ErrUserNotFound, ErrDeleteSelf,
	ErrDeleteSuperAdmin, ErrSuperAdminOnly, MsgWrongUserType,
	// 角色
	ErrRoleNotFound, ErrRoleCodeExists, ErrRoleIsSystem, ErrRoleCodeNumeric,
	// 菜单
	ErrMenuNotFound, ErrMenuHasChildren, ErrMenuIsSystem, ErrMenuCircle,
	ErrMenuParentNotFound, ErrMenuParentMustBeDir, ErrMenuDepthExceeded,
	ErrCodeNotBindable, ErrCodeRequired, ErrCodeNotEnabled,
	// 权限点
	ErrPermissionNotFound, ErrCodeExists, ErrCodeImmutable, ErrInvalidMethod,
	ErrPermissionAssigned, ErrMenuReferenced,
	// 部门
	ErrDeptNotFound, ErrDeptHasChildren, ErrDeptHasUsers, ErrDeptCircle, ErrDeptCodeExists,
	// 数据权限规则
	ErrRuleNotFound, ErrInvalidDomain, ErrInvalidAssignment, ErrRuleConfigInvalid,
	ErrRuleFieldNotAllowed, ErrRuleOpNotAllowed, ErrRuleLogicNotAllowed,
	// 列表排序参数与文案词条表单
	ErrSortFieldInvalid, ErrSortDirectionInvalid,
	ErrI18nKeyEmpty, ErrI18nLangEmpty, ErrI18nValueEmpty,
}

// --- 管理员 ---

const (
	ErrCaptchaExpired   = "ErrCaptchaExpired"         // 验证码错误或已过期
	ErrBadCredentials   = "ErrInvalidPassword"        // 用户名或密码错误
	ErrAccountLocked    = "ErrAccountLocked"          // 账号已被锁定，请 %s 后重试（带参，key|param 协议）
	ErrAccountDisabled  = "ErrAdminDisabled"          // 账号已被禁用
	ErrAdminNotFound    = "ErrAdminNotFound"          // 管理员不存在
	ErrSuperAdminExists = "ErrSuperAdminExists"       // 系统已存在超级管理员，不能重复创建
	ErrFieldProtected   = "ErrFieldProtected"         // 不允许外部修改（受保护字段）
	ErrEmailExists      = "ErrAdminEmailExists"       // 该邮箱已存在
	ErrUsernameExists   = "ErrAdminUsernameExists"    // 用户名已存在，请修改
	ErrPhoneExists      = "ErrAdminPhoneExists"       // 手机号码重复，请修改
	ErrUserNotFound     = "ErrUserNotFound"           // 用户不存在
	ErrDeleteSelf       = "ErrAdminDeleteSelf"        // 不能删除当前登录管理员
	ErrDeleteSuperAdmin = "ErrAdminDeleteSuperAdmin"  // 不能删除超级管理员
	ErrSuperAdminOnly   = "ErrSuperAdminOnly"         // 无权操作超管账号（仅超管可操作超管账号/超管角色）
	MsgLogoutSuccess    = "msg_admin_logout_success"  // 退出成功
	MsgWrongUserType    = "ErrAdminInvalidUserIDType" // 用户ID类型错误
)

// --- 角色 ---

const (
	ErrRoleNotFound    = "ErrRoleNotFound"    // 角色不存在
	ErrRoleCodeExists  = "ErrRoleCodeExists"  // 角色编码已存在
	ErrRoleIsSystem    = "ErrRoleIsSystem"    // 系统内置角色不可删除
	ErrRoleCodeNumeric = "ErrRoleCodeNumeric" // 角色编码不能为纯数字
)

// --- 菜单 ---

const (
	ErrMenuNotFound        = "ErrMenuNotFound"        // 菜单不存在
	ErrMenuHasChildren     = "ErrMenuHasChildren"     // 该菜单下有子菜单，无法删除
	ErrMenuIsSystem        = "ErrMenuIsSystem"        // 系统内置菜单不可删除或修改类型
	ErrMenuCircle          = "ErrMenuCircle"          // 不能将菜单移动到自身或其子级下
	ErrMenuParentNotFound  = "ErrMenuParentNotFound"  // 父级菜单不存在
	ErrMenuParentMustBeDir = "ErrMenuParentMustBeDir" // 菜单/外链的父级必须是目录
	ErrMenuDepthExceeded   = "ErrMenuDepthExceeded"   // 菜单层级超过上限（3 级）
	ErrCodeNotBindable     = "ErrCodeNotBindable"     // 目录、iframe 和外链不能绑定权限编码
	ErrCodeRequired        = "ErrCodeRequired"        // 菜单和按钮类型必须绑定权限编码
	ErrCodeNotEnabled      = "ErrCodeNotEnabled"      // 绑定的权限编码不存在或未启用
)

// --- 权限点 ---

const (
	ErrPermissionNotFound = "ErrPermissionNotFound" // 权限点不存在
	ErrCodeExists         = "ErrCodeExists"         // 权限编码已存在
	ErrCodeImmutable      = "ErrCodeImmutable"      // 权限编码创建后不可修改
	ErrInvalidMethod      = "ErrInvalidMethod"      // 请求方法只允许 GET 或 POST
	ErrPermissionAssigned = "ErrPermissionAssigned" // 该权限已分配，请先解除角色和用户授权
	ErrMenuReferenced     = "ErrMenuReferenced"     // 该权限被菜单引用，无法删除
)

// --- 部门 ---

const (
	ErrDeptNotFound    = "ErrDeptNotFound"    // 部门不存在
	ErrDeptHasChildren = "ErrDeptHasChildren" // 该部门下有子部门，无法删除
	ErrDeptHasUsers    = "ErrDeptHasUsers"    // 该部门下有用户，无法删除
	ErrDeptCircle      = "ErrDeptCircle"      // 不能将部门移动到自身或其子级下
	ErrDeptCodeExists  = "ErrDeptCodeExists"  // 部门编码已存在
)

// --- 数据权限规则 ---

const (
	ErrRuleNotFound        = "ErrRuleNotFound"        // 数据规则不存在
	ErrInvalidDomain       = "ErrInvalidDomain"       // 不支持的数据域
	ErrInvalidAssignment   = "ErrInvalidAssignment"   // 无效的数据规则分配目标
	ErrRuleConfigInvalid   = "ErrRuleConfigInvalid"   // 数据规则配置不合法
	ErrRuleFieldNotAllowed = "ErrRuleFieldNotAllowed" // 规则引用了数据域白名单之外的字段
	ErrRuleOpNotAllowed    = "ErrRuleOpNotAllowed"    // 规则使用了该字段不支持的操作符
	ErrRuleLogicNotAllowed = "ErrRuleLogicNotAllowed" // 条件组的组合逻辑只能是 AND 或 OR
)

// --- 列表排序参数 ---
//
// 这一组此前是 service 里的中文原文（errors.New("无效的排序字段")）：文案不来自本包、
// 于是既不进白名单（页面/接口一律被归口成「操作失败，请稍后重试」），也无法翻译。
// 参数错误是**客户端输入问题**，必须让调用方看见「哪一项不对」——
// 与白名单里其它业务文案同一条判据，只是来源从 service 的业务判定换成了入参校验。

const (
	ErrSortFieldInvalid     = "admin.err.sortFieldInvalid"     // 无效的排序字段
	ErrSortDirectionInvalid = "admin.err.sortDirectionInvalid" // 无效的排序方向
)

// --- 文案词条表单 ---
//
// 空 key / 空语言 / 空内容此前共用 MsgFieldRequired（「必填字段不能为空」）：
// 运营点保存后只知道「有个字段没填」，页面上却不说是哪一个 —— 三行表单只能逐个试。
// 三者拆成三条文案：判据相同（都是客户端输入问题），差别只在「说得够不够具体」。

const (
	ErrI18nKeyEmpty   = "admin.err.i18nKeyEmpty"   // 词条 key 不能为空
	ErrI18nLangEmpty  = "admin.err.i18nLangEmpty"  // 词条语言不能为空
	ErrI18nValueEmpty = "admin.err.i18nValueEmpty" // 词条内容不能为空
)

// --- 批量操作的结论文案（页面回执，不是错误白名单）---
//
// 这一组与上面那些常量同形（值 = sys_i18n 的 item_key），但**刻意不带 Err / Msg 前缀**：
// admin_enums_test.go 按前缀逐个常量对账 AdminFacingMessages，而那张白名单管的是
// 「service 返回的错误能不能透出」。批量结论是 handler 按计数自己拼出的整句回执
// （进 ?done= / ?err=），既不是 service 错误、也不该进错误白名单 ——
// 误加进去会让「读侧候选必须能由写侧复现」那条对账用例变红。
//
// 为什么 key 放 enums 而不是 handler 包：模块的对外文案 key 一律以 enums 为唯一登记处，
// 回执文案同属对外文案，不该是例外。中文原文（模板 + 名词）留在 inbound/http ——
// 它与「写侧按当前语言取词」的那一份绑在同一个结构体里（见 admin_err.go 的 adminBulkText）。
const (
	// BulkDoneKey 批量删除全部成功的结论模板（两个 %s：删除数、名词译文）。
	BulkDoneKey = "admin.bulk.done"
	// BulkPartialKey 批量删除部分成功的结论模板（三个 %s：删除数、名词译文、未删除数）。
	BulkPartialKey = "admin.bulk.partial"
)

// 批量删除文案里的名词 key（写侧代入模板，读侧候选同样先代入再归一）。
const (
	BulkNounAdmin      = "admin.bulk.noun.admin"
	BulkNounRole       = "admin.bulk.noun.role"
	BulkNounPermission = "admin.bulk.noun.permission"
	BulkNounMenu       = "admin.bulk.noun.menu"
	BulkNounDept       = "admin.bulk.noun.dept"
	BulkNounDatarule   = "admin.bulk.noun.datarule"
)

// 词条页批量删除的四个结论分支。
const (
	BulkI18nNoneSelected = "admin.i18nBulk.noneSelected"
	BulkI18nAllDeleted   = "admin.i18nBulk.allDeleted"
	BulkI18nAllSkipped   = "admin.i18nBulk.allSkipped"
	BulkI18nPartial      = "admin.i18nBulk.partial"
)

// —— 点分 key 常量（新式）——
//
// 值是 sys_i18n 的 item_key（文案真源在迁移 451），命名按「去掉 `admin.` 模块前缀
// 后的语义路径」：包名 adminenums 已给出模块上下文。
//
// 与上面那批 `ErrXxx = "ErrXxx"` / `MsgXxx = "MsgXxx"` 分开成组：老式形态的值就是
// 常量名本身（`sys_i18n` 里存同名 key），两者混在同一前缀下会让人以为值也是常量名。
// 中文兜底留在调用点（词条缺失时的回落），不在这里。
const (
	// 数据规则配置编辑器的下拉占位与前置提示（字段与操作符两个下拉各一条）。
	DatarulesEditorFieldPlaceholder = "admin.datarules.editor.field_placeholder" // 请选择字段
	DatarulesEditorFieldRequired    = "admin.datarules.editor.field_required"    // 请先选择字段
)
