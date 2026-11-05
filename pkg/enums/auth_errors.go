package enums

import (
	"errors"
	"net/http"

	"go_wp/pkg/facing"
)

// AuthError 只声明 key 与状态码；文案由词库（sys_i18n）在 JSON 出口统一取词。
func AuthError(key string) error {
	var status int
	switch key {
	case ErrUnauthorized, ErrEnvironmentAbnormal, ErrSessionExpired, ErrForcedLogout, ErrUserInfoMissing:
		status = http.StatusUnauthorized
	case ErrPermissionDenied:
		status = http.StatusForbidden
	case ErrAuthzNotReady, ErrAuthzFailed:
		status = http.StatusInternalServerError
	case ErrAuthUnavailable:
		status = http.StatusServiceUnavailable
	default:
		return errors.New("未声明的认证错误")
	}
	return facing.WithStatus(facing.New(key), status)
}
