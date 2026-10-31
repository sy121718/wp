package enums

import (
	"errors"
	"net/http"

	"go_wp/pkg/facing"
)

func AuthError(key string) error {
	var fallback string
	var status int
	switch key {
	case ErrUnauthorized:
		fallback, status = "未登录或登录已过期", http.StatusUnauthorized
	case ErrEnvironmentAbnormal:
		fallback, status = "环境异常，请重新登录", http.StatusUnauthorized
	case ErrSessionExpired:
		fallback, status = "登录会话已失效，请重新登录", http.StatusUnauthorized
	case ErrForcedLogout:
		fallback, status = "账号已被强制下线", http.StatusUnauthorized
	case ErrUserInfoMissing:
		fallback, status = "未获取到用户信息", http.StatusUnauthorized
	case ErrPermissionDenied:
		fallback, status = "无权限访问", http.StatusForbidden
	case ErrAuthzNotReady:
		fallback, status = "权限系统未初始化", http.StatusInternalServerError
	case ErrAuthzFailed:
		fallback, status = "权限验证失败", http.StatusInternalServerError
	case ErrAuthUnavailable:
		fallback, status = "认证状态暂时不可用", http.StatusServiceUnavailable
	default:
		return errors.New("未声明的认证错误")
	}
	return facing.WithStatus(facing.New(key, fallback), status)
}
