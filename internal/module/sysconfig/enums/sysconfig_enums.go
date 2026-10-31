package sysconfigenums

import "go_wp/pkg/facing"

const (
	MsgGroupSaved = "MsgGroupSaved"

	MsgListSuccess = "MsgListSuccess"

	MsgDetailSuccess = "MsgDetailSuccess"
)

const (
	ErrInvalidParam = "ErrInvalidParam"

	ErrVersionRequired = "ErrVersionRequired"

	ErrGroupNotFound = "ErrGroupNotFound"

	ErrVersionConflict = "ErrVersionConflict"
)

const (
	AdminSystemTitle = "admin.system.title"
)

const (
	AdminSystemSaved = "admin.system.saved"

	AdminSystemGroupMissing = "admin.system.group_missing"

	AdminSystemErrLangUnknown = "admin.system.err.lang_unknown"

	AdminSystemErrCurrencyUnknown = "admin.system.err.currency_unknown"

	AdminSystemErrCountryUnknown = "admin.system.err.country_unknown"
)

var errFallbacks = map[string]string{
	ErrInvalidParam:    "参数不合法：请检查后重试。",
	ErrVersionRequired: "缺少版本号：整组保存必须带上当前版本，避免覆盖别人的改动。",
	ErrGroupNotFound:   "配置分组不存在",
	ErrVersionConflict: "配置已被其他管理员改过：请刷新后重新提交。",
}

func Err(key string) error {
	return facing.New(key, errFallbacks[key])
}

func FallbackFor(key string) string { return errFallbacks[key] }
