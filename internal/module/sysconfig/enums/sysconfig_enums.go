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

func Err(key string) error {
	return facing.New(key)
}
