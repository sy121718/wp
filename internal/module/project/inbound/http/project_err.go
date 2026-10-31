package projecthttp

import (
	"errors"
	"go_wp/internal/middleware/builtin"
	"net/http"

	projectenums "go_wp/internal/module/project/enums"
	service "go_wp/internal/module/project/service"
	"go_wp/pkg/logger"

	"github.com/gin-gonic/gin"
)

func projectErrStatusText(err error, fallbackKey string) (status int, key string) {
	switch {
	case err == nil:
		return http.StatusInternalServerError, fallbackKey

	case errors.Is(err, service.ErrThemeNotFound):
		return http.StatusNotFound, projectenums.ErrThemeNotFound
	case errors.Is(err, service.ErrThemeIsActive):
		return http.StatusBadRequest, projectenums.ErrThemeIsActive
	case errors.Is(err, service.ErrThemeNameRequired):
		return http.StatusBadRequest, projectenums.ErrThemeNameRequired
	case errors.Is(err, service.ErrThemeDuplicateName):
		return http.StatusBadRequest, projectenums.ErrThemeDuplicateName
	case errors.Is(err, service.ErrThemeProjectIDEmpty):
		return http.StatusBadRequest, projectenums.ErrThemeProjectIDEmpty
	case errors.Is(err, service.ErrInvalidThemeSettings):
		return http.StatusBadRequest, projectenums.ErrInvalidThemeSettings
	case errors.Is(err, service.ErrThemeProjectRequired):
		return http.StatusBadRequest, projectenums.ErrProjectRequired

	case errors.Is(err, service.ErrProjectNotFound):
		return http.StatusNotFound, projectenums.ErrProjectNotFound
	case errors.Is(err, service.ErrInvalidName):
		return http.StatusBadRequest, projectenums.ErrInvalidName
	case errors.Is(err, service.ErrInvalidSettings):
		return http.StatusBadRequest, projectenums.ErrInvalidSettings
	case errors.Is(err, service.ErrInvalidParam):
		return http.StatusBadRequest, projectenums.ErrInvalidParam

	case errors.Is(err, service.ErrLocaleNoTranslations):
		return http.StatusBadRequest, projectenums.ErrLocaleNoTranslations

	default:

		return http.StatusInternalServerError, fallbackKey
	}
}

func projectErrMsg(c *gin.Context, scene, fallbackKey string, err error) projectMsg {
	status, key := projectErrStatusText(err, fallbackKey)
	if err != nil && status == http.StatusInternalServerError {
		logger.Scene(scene).With("user_id", builtin.CurrentUserID(c)).
			With("path", c.Request.URL.Path).Error(err, "项目域页面操作失败")
	}
	return projectMsg{Key: key, Fallback: key}
}

var projectWriteTextKeys = []string{

	projectenums.ErrThemeNotFound,
	projectenums.ErrThemeIsActive,
	projectenums.ErrThemeNameRequired,
	projectenums.ErrThemeDuplicateName,
	projectenums.ErrThemeProjectIDEmpty,
	projectenums.ErrInvalidThemeSettings,
	projectenums.ErrProjectRequired,
	projectenums.ErrProjectNotFound,
	projectenums.ErrLocaleNoTranslations,
	projectenums.ErrInvalidName,
	projectenums.ErrInvalidSettings,
	projectenums.ErrInvalidParam,

	projectenums.ErrThemeInternal,
	projectenums.ErrProjectInternal,

	projectenums.ErrThemeIDRequired,
	projectenums.MsgThemeSettingsInvalid,
	projectenums.MsgThemeSettingsRefreshFailed,
	projectenums.ErrSiteSettingsNameRequired,
	projectenums.ErrGA4IDInvalid,
	projectenums.ErrGSCVerificationInvalid,
	projectenums.ErrNotFoundHTMLTooLong,
	projectenums.ErrLangURLModeInvalid,

	projectenums.ErrShippingBaseFeeInvalid,
	projectenums.ErrShippingFreeThresholdInvalid,

	projectenums.MsgSiteSettingsSaved,
	projectenums.ThemeOKCreated,
	projectenums.ThemeOKActivated,
	projectenums.ThemeOKDeleted,
	projectenums.SiteLocalesSaved,
	projectenums.ThemeSettingsOKSaved,
}
