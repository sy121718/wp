package projectenums

import "go_wp/pkg/facing"

const (
	ErrProjectNotFound = "ErrProjectNotFound"
	ErrInvalidSettings = "ErrInvalidSettings"
	ErrInvalidName     = "ErrInvalidName"

	ErrInvalidParam = "ErrInvalidParam"

	ErrProjectInternal = "ErrProjectInternal"

	ErrProjectRequired = "ErrProjectRequired"
)

const (
	ErrThemeNameRequired    = "ErrThemeNameRequired"
	ErrThemeNotFound        = "ErrThemeNotFound"
	ErrThemeIsActive        = "ErrThemeIsActive"
	ErrThemeDuplicateName   = "ErrThemeDuplicateName"
	ErrThemeProjectIDEmpty  = "ErrThemeProjectIDEmpty"
	ErrInvalidThemeSettings = "ErrInvalidThemeSettings"

	ErrThemeInternal = "ErrThemeInternal"
)

const ()

const (
	MsgProjectCreated = "MsgProjectCreated"
	MsgProjectUpdated = "MsgProjectUpdated"
)

const (
	ErrThemeIDRequired = "ErrThemeIDRequired"

	MsgThemeSettingsInvalid = "MsgThemeSettingsInvalid"

	MsgThemeSettingsRefreshFailed = "MsgThemeSettingsRefreshFailed"

	ErrSiteSettingsNameRequired = "ErrSiteSettingsNameRequired"
	ErrGA4IDInvalid             = "ErrGA4IDInvalid"
	ErrGSCVerificationInvalid   = "ErrGSCVerificationInvalid"
	ErrNotFoundHTMLTooLong      = "ErrNotFoundHTMLTooLong"
	ErrLangURLModeInvalid       = "ErrLangURLModeInvalid"

	ErrLocaleNoTranslations = "ErrLocaleNoTranslations"

	ErrShippingBaseFeeInvalid       = "ErrShippingBaseFeeInvalid"
	ErrShippingFreeThresholdInvalid = "ErrShippingFreeThresholdInvalid"

	MsgSiteSettingsSaved = "MsgSiteSettingsSaved"
)

const (
	ThemeOKCreated   = "admin.theme.ok.created"
	ThemeOKActivated = "admin.theme.ok.activated"
	ThemeOKDeleted   = "admin.theme.ok.deleted"
)

const (
	ThemeSettingsStructureCurrent = "admin.theme_settings.structure.current"
	ThemeSettingsBlockUnset       = "admin.theme_settings.block.unset"

	SEOScoreIssueFormat    = "admin.seo.score.issueFormat"
	SEOScoreSerpTitleEmpty = "admin.seo.score.serpTitleEmpty"
	SEOScoreSerpDescEmpty  = "admin.seo.score.serpDescEmpty"

	ThemeSettingsOKSaved         = "admin.theme_settings.ok.saved"
	ThemeSettingsErrFieldInvalid = "admin.theme_settings.err.fieldInvalid"

	SiteLocalesSaved = "admin.settings.locales.saved"
)

func Err(key string) error { return facing.New(key) }
