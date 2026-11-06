package adminenums

import (
	"errors"
	"net/http"

	"go_wp/pkg/enums"
	"go_wp/pkg/facing"
)

const (
	MsgSuccess      = enums.MsgOperationSuccess
	MsgBadRequest   = enums.ErrInvalidParams
	MsgUnauthorized = "ErrUnauthorized"

	ErrInternal = "ErrInternal"
)

const (
	ErrCaptchaExpired   = "ErrCaptchaExpired"
	ErrBadCredentials   = "ErrInvalidPassword"
	ErrAccountLocked    = "ErrAccountLocked"
	ErrAccountDisabled  = "ErrAdminDisabled"
	ErrAdminNotFound    = "ErrAdminNotFound"
	ErrSuperAdminExists = "ErrSuperAdminExists"
	ErrFieldProtected   = "ErrFieldProtected"
	ErrEmailExists      = "ErrAdminEmailExists"
	ErrUsernameExists   = "ErrAdminUsernameExists"
	ErrPhoneExists      = "ErrAdminPhoneExists"
	ErrUserNotFound     = "ErrUserNotFound"
	ErrDeleteSelf       = "ErrAdminDeleteSelf"
	ErrDeleteSuperAdmin = "ErrAdminDeleteSuperAdmin"
	ErrSuperAdminOnly   = "ErrSuperAdminOnly"
	MsgLogoutSuccess    = "msg_admin_logout_success"
	MsgWrongUserType    = "ErrAdminInvalidUserIDType"
)

const (
	ErrRoleNotFound    = "ErrRoleNotFound"
	ErrRoleCodeExists  = "ErrRoleCodeExists"
	ErrRoleIsSystem    = "ErrRoleIsSystem"
	ErrRoleCodeNumeric = "ErrRoleCodeNumeric"
)

// ErrBulkDeleteEmpty 是「批量删除时一个有效 id 都没有」的拒绝 key。
// 声明归 enums（业务错误只在这里声明 key），文案由词库在 JSON 出口取词。
const ErrBulkDeleteEmpty = "admin.bulk.emptySelection"

// ErrBulkDeleteInvalidID 是「批量删除的 ids 里有不能当主键解析的原始值」的拒绝 key。
//
// 每个非法原始值都会作为一个明细项出现（见 service.InvalidIDsError），
// 出口把它翻成 data.items，让前端能点名「哪一个不对」，而不是静默丢掉。
const ErrBulkDeleteInvalidID = "admin.bulk.invalidSelection"

const (
	ErrMenuNotFound        = "ErrMenuNotFound"
	ErrMenuHasChildren     = "ErrMenuHasChildren"
	ErrMenuIsSystem        = "ErrMenuIsSystem"
	ErrMenuCircle          = "ErrMenuCircle"
	ErrMenuParentNotFound  = "ErrMenuParentNotFound"
	ErrMenuParentMustBeDir = "ErrMenuParentMustBeDir"
	ErrMenuDepthExceeded   = "ErrMenuDepthExceeded"
	ErrCodeNotBindable     = "ErrCodeNotBindable"
	ErrCodeRequired        = "ErrCodeRequired"
	ErrCodeNotEnabled      = "ErrCodeNotEnabled"
)

const (
	ErrPermissionNotFound = "ErrPermissionNotFound"
	ErrCodeExists         = "ErrCodeExists"
	ErrCodeImmutable      = "ErrCodeImmutable"
	ErrInvalidMethod      = "ErrInvalidMethod"
	ErrPermissionAssigned = "ErrPermissionAssigned"
	ErrMenuReferenced     = "ErrMenuReferenced"
)

const (
	ErrDeptNotFound    = "ErrDeptNotFound"
	ErrDeptHasChildren = "ErrDeptHasChildren"
	ErrDeptHasUsers    = "ErrDeptHasUsers"
	ErrDeptCircle      = "ErrDeptCircle"
	ErrDeptCodeExists  = "ErrDeptCodeExists"
)

const (
	ErrRuleNotFound        = "ErrRuleNotFound"
	ErrInvalidDomain       = "ErrInvalidDomain"
	ErrInvalidAssignment   = "ErrInvalidAssignment"
	ErrRuleConfigInvalid   = "ErrRuleConfigInvalid"
	ErrRuleFieldNotAllowed = "ErrRuleFieldNotAllowed"
	ErrRuleOpNotAllowed    = "ErrRuleOpNotAllowed"
	ErrRuleLogicNotAllowed = "ErrRuleLogicNotAllowed"
)

const (
	ErrSortFieldInvalid     = "admin.err.sortFieldInvalid"
	ErrSortDirectionInvalid = "admin.err.sortDirectionInvalid"
)

const (
	ErrI18nKeyEmpty   = "admin.err.i18nKeyEmpty"
	ErrI18nLangEmpty  = "admin.err.i18nLangEmpty"
	ErrI18nValueEmpty = "admin.err.i18nValueEmpty"

	// ErrI18nSelectionEmpty 是「批量删除一条都没选」。
	ErrI18nSelectionEmpty = "admin.err.i18nSelectionEmpty"

	// ErrI18nEntryMissing 是「要删的词条不存在」——整批拒绝时的主 key。
	ErrI18nEntryMissing = "admin.err.i18nEntryMissing"
)

const (
	// ErrAdminSelectionEmpty 是「批量操作一个人都没选」：空选择不是「改了 0 条」的成功。
	ErrAdminSelectionEmpty = "admin.err.adminSelectionEmpty"

	// ErrAdminBatchTooMany 是「批量操作人数超过上限」。
	ErrAdminBatchTooMany = "admin.err.adminBatchTooMany"
)

const (
	BulkDoneKey = "admin.bulk.done"

	BulkPartialKey = "admin.bulk.partial"
)

const (
	BulkNounAdmin      = "admin.bulk.noun.admin"
	BulkNounRole       = "admin.bulk.noun.role"
	BulkNounPermission = "admin.bulk.noun.permission"
	BulkNounMenu       = "admin.bulk.noun.menu"
	BulkNounDept       = "admin.bulk.noun.dept"
	BulkNounDatarule   = "admin.bulk.noun.datarule"
)

const (
	BulkI18nNoneSelected = "admin.i18nBulk.noneSelected"
	BulkI18nAllDeleted   = "admin.i18nBulk.allDeleted"
	BulkI18nAllSkipped   = "admin.i18nBulk.allSkipped"
	BulkI18nPartial      = "admin.i18nBulk.partial"
)

const (
	DatarulesEditorFieldPlaceholder = "admin.datarules.editor.field_placeholder"
	DatarulesEditorFieldRequired    = "admin.datarules.editor.field_required"

	DatarulesBackToEdit = "admin.datarules.back_to_edit"

	ShellLoginTitle = "shell.login.title"

	DevLoginLoopbackOnly  = "admin.dev_login.loopback_only"
	DevLoginFailed        = "admin.dev_login.failed"
	DevLoginSessionFailed = "admin.dev_login.session_failed"
	DevLoginCSRFFailed    = "admin.dev_login.csrf_failed"
)

func Err(key string) error {
	err := facing.New(key)
	if status := StatusOf(key); status != 0 {
		return facing.WithStatus(err, status)
	}
	return err
}

// BadRequest 是「请求参数不合法」的错误声明：key + 状态码只在这里给。
//
// 状态码是错误事实的一部分，不写在调用点 —— 之前各 handler 用
// response.ErrorWithMessage(c, 400, ...) 把状态码钉在参数校验分支上，
// 声明里的说法反而管不了它。现在调用点只写 enums.BadRequest()，出口按声明出状态。
func BadRequest() error {
	return facing.WithStatus(facing.New(MsgBadRequest), http.StatusBadRequest)
}

// i18nMissingEntriesError 携带「哪几条词条不存在」，并实现 facing.BlockedError，
// 让统一出口把明细放进 data.items。
//
// 这是 controller 侧的声明：词库层（pkg/i18n）不认识 facing，它只交 MissingEntry 事实。
type i18nMissingEntriesError struct {
	items []facing.BlockedItem
}

func (e *i18nMissingEntriesError) Error() string { return "批量删除的词条不存在" }

func (e *i18nMissingEntriesError) BlockedItems() []facing.BlockedItem { return e.items }

// I18nEntryRef 是 controller 交给本函数的最小事实：哪条词条没找到。
type I18nEntryRef struct {
	Key  string
	Lang string
}

// MissingI18nEntries 声明「批量删除里有词条不存在」：主 key + 404，明细进 data.items。
//
// 为什么不是 409：用户下一步是改选（刷新列表看现在还有什么），与「对象存在但
// 当前状态不允许删」是两种处置。整批不删，所以状态取最该先说的那一条。
func MissingI18nEntries(missing []I18nEntryRef) error {
	items := make([]facing.BlockedItem, 0, len(missing))
	for _, m := range missing {
		name := m.Key
		if m.Lang != "" {
			name = m.Key + "|" + m.Lang
		}
		items = append(items, facing.BlockedItem{
			ID:        name,
			Name:      name,
			ReasonKey: ErrI18nEntryMissing,
		})
	}
	base := facing.WithStatus(facing.New(ErrI18nEntryMissing), http.StatusNotFound)
	return errors.Join(base, &i18nMissingEntriesError{items: items})
}

// StatusOf 给出业务 key 的错误声明状态码；0 表示这个 key 没声明状态。
//
// 批量删除要知道「这批里最该先说的那条是什么状态」，靠的不是比较字符串，
// 而是每个 key 自己声明的状态（与 product/enums 同一套做法）。
// 没声明的 key 返回 0，调用方按冲突处理。
func StatusOf(key string) int {
	switch key {
	// 查无此对象：用户下一步是改选，先说这个。
	case ErrAdminNotFound, ErrRoleNotFound, ErrPermissionNotFound,
		ErrMenuNotFound, ErrDeptNotFound, ErrRuleNotFound, ErrI18nEntryMissing:
		return http.StatusNotFound

	// 请求本身没给出可操作的东西：改请求，不是改数据。
	case ErrI18nSelectionEmpty, ErrAdminSelectionEmpty, ErrAdminBatchTooMany:
		return http.StatusBadRequest

	// 存在但当前状态不允许删：用户下一步是先去解除引用/关系，再回来删。
	case ErrRoleIsSystem, ErrMenuIsSystem, ErrMenuHasChildren, ErrDeptHasChildren,
		ErrDeptHasUsers, ErrDeleteSelf, ErrDeleteSuperAdmin,
		ErrPermissionAssigned, ErrMenuReferenced:
		return http.StatusConflict
	}
	return 0
}
