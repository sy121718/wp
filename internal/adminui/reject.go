// Package adminui 承载后台页面渲染与外壳装配；失败统一转交 JSON 响应出口。
package adminui

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	"go_wp/pkg/enums"
	"go_wp/pkg/response"
)

// Reject 将页面鉴权拒绝交给 JSON 出口；状态由认证错误声明处携带。
// Accept 与 HX 请求头不改变失败协议，前端自行决定后续导航。
func Reject() builtin.PageRejectFunc {
	return func(c *gin.Context, _ int, key string) {
		c.Header("Cache-Control", "no-store")
		response.ErrorStatusAuto(c, "adminui", enums.AuthError(key))
	}
}
