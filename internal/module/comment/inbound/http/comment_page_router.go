package commenthttp

// comment_page_router.go — 评论审核页（/admin/comments）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 comment_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面写动作复用接口权限点（真源是 comment_router.go 的 rg 注册动作）：
// CasbinMiddlewareForPath 的参数就是那条 api 路由的路径，与注册动作算出来的绝对路径逐字一致。
// 路径换了而权限点没换，结果是页面按钮人人可见但提交必 403，或者更糟 ——
// 提交成功但用的是另一个权限。页面 GET 的 Casbin 待补（见 docs/02-Z §4.3）。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/middleware/builtin"
	commentcontract "go_wp/internal/module/comment/contract"
)

// SetupCommentPages 注册评论审核页；pages 为 nil 时整体跳过。
func SetupCommentPages(pages *gin.RouterGroup, svc commentcontract.CommentService,
	projects commentProjectLister) {
	if pages == nil {
		return
	}
	pageHandle := NewCommentPageHandle(svc, projects)
	pages.GET("/comments", pageHandle.CommentsPage)
	pages.POST("/comments/review",
		builtin.CasbinMiddlewareForPath("/api/comment/review"), pageHandle.Review)
}
