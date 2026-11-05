package adminhttp

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	adminenums "go_wp/internal/module/admin/enums"
	"go_wp/pkg/logger"
	r "go_wp/pkg/response"
)

const adminErrScene = "admin"

// adminFail 把失败交给统一 JSON 出口，状态码由错误事实决定。
//
// 之前这里按「业务/非业务」二分成 400/500，把 404 与 409 都压成了 400；
// 现在直接交 ErrorStatusAuto，让它按声明里的状态码写真实状态。
func adminFail(c *gin.Context, err error) {
	r.ErrorStatusAuto(c, adminErrScene, err)
}

func adminBindFail(c *gin.Context, err error) {
	if err != nil {

		logger.Scene(adminErrScene).
			With("user_id", builtin.CurrentUserID(c)).
			With("detail", err.Error()).
			Warn("admin 接口请求绑定失败")
	}
	r.ErrorStatusAuto(c, adminErrScene, adminenums.BadRequest())
}
