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

var errFallbacks = map[string]string{
	ErrProjectNotFound:              "工程不存在",
	ErrInvalidSettings:              "站点设置必须是合法的 JSON 对象",
	ErrInvalidName:                  "站点工程名称不能为空且不能超过 200 个字符",
	ErrInvalidParam:                 "请求参数无效",
	ErrProjectInternal:              "工程服务内部错误",
	ErrProjectRequired:              "请先选择要操作的站点工程",
	ErrThemeNameRequired:            "主题名称不能为空",
	ErrThemeNotFound:                "主题不存在",
	ErrThemeIsActive:                "激活主题不可删除，请先切换到其他主题",
	ErrThemeDuplicateName:           "同名主题已存在",
	ErrThemeProjectIDEmpty:          "工程 ID 不能为空",
	ErrInvalidThemeSettings:         "无效的主题设置",
	ErrThemeInternal:                "主题服务内部错误",
	ErrThemeIDRequired:              "缺少主题 id",
	ErrSiteSettingsNameRequired:     "工程与站点名称不能为空",
	ErrGA4IDInvalid:                 "GA4 测量 ID 格式不合法（形如 G-XXXXXXXXXX，只允许字母与数字）",
	ErrGSCVerificationInvalid:       "Search Console 验证 token 格式不合法（base64url：字母、数字、- 与 _，8~128 位）",
	ErrNotFoundHTMLTooLong:          "自定义 404 页内容过长（上限 32 KiB）",
	ErrLangURLModeInvalid:           "语言 URL 方案取值非法（可选 off / default_plain / all_prefix）",
	ErrLocaleNoTranslations:         "该语言还没有任何界面词条，不能启用：请先在「文案管理」里补齐该语言的词条，再回来启用。",
	ErrShippingBaseFeeInvalid:       "基础运费必须是不小于 0 的数字（单位：元，最多两位小数），且不超过 10000 元",
	ErrShippingFreeThresholdInvalid: "满额免运费门槛必须是不小于 0 的数字（单位：元，最多两位小数），且不超过 10000 元；留空或 0 表示不启用",
}

func Err(key string) error { return facing.New(key, errFallbacks[key]) }

func FallbackFor(key string) string { return errFallbacks[key] }
