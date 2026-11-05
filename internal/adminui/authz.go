package adminui

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	mw "go_wp/pkg/middleware"
)

// PageAuthz 委托框架的页面鉴权入口；拒绝出口由 pkg/middleware 统一。
func PageAuthz(obj string) gin.HandlerFunc {
	return mw.PageAuthz(obj)
}

// PageAuthzAs 委托框架的显式方法版本。
func PageAuthzAs(obj, act string) gin.HandlerFunc {
	return mw.PageAuthzAs(obj, act)
}

var _ = builtin.PageCasbinMiddleware
