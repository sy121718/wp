package adminhttp

// admin_pages_login.go — 后台登录页（GET /admin/login）：渲染登录页骨架并注入验证码图片，debug 模式显示一键登录入口。

import (
	"net/http"
	"strings"

	"go_wp/config"
	"go_wp/internal/web/shell"
	"go_wp/pkg/captcha"

	"github.com/gin-gonic/gin"
)

// AdminLoginPage 后台登录页（独立布局，供未登录的页面请求 302 跳转，也支持直接访问）。
// 验证码经 /api/captcha 返回图片（答案不下发），此处渲染页面骨架即可；
// 若渲染入口已生成验证码图片则直接注入，避免首屏额外请求。
func AdminLoginPage(c *gin.Context) {
	id, image := captcha.Get().GenerateImage()
	// 登录页无会话（不走 Prepare 的权限集部分），同样需要语言数据：
	// 标题走 shell.login.title（缺词条回退「登录」）。
	c.HTML(http.StatusOK, "admin/login", shell.Prepare(c, gin.H{
		"title":         shell.TranslateFor(c)("shell.login.title", "登录"),
		"captcha_id":    id,
		"captcha_image": image,
		// debug 模式才显示一键登录入口（release 下路由压根不存在，显示了也是个死链）。
		"DevLogin": AdminDevLoginEnabled(),
	}))
}

// AdminDevLoginEnabled 是否处于 debug 模式（决定登录页是否显示一键登录入口）。
// 与 routers 里注册 /admin/dev-login 用的是同一个判断：两处必须一致。
func AdminDevLoginEnabled() bool {
	v, err := config.GetViper()
	return err == nil && strings.EqualFold(v.GetString("server.mode"), "debug")
}
