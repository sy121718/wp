package commenthttp

// comment_page_router.go — 评论审核页（/admin/comments）的注册落点。
//
// 只做注册：`/admin` 组的中间件链（Session + CSRF + 权限上下文）由装配层统一挂好，
// handler 在 comment_page.go。路由注册只出现在 *_router.go，门禁
// scripts/check-route-registration-placement.sh 守这条。
//
// 页面 GET 使用列表权限；写操作只经 JSON API。

import (
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	commentcontract "go_wp/internal/module/comment/contract"
)

// SetupCommentPages 注册评论审核页；pages 为 nil 时整体跳过。
func SetupCommentPages(pages *gin.RouterGroup, svc commentcontract.CommentService,
	projects commentProjectLister) {
	if pages == nil {
		return
	}
	pageHandle := NewCommentPageHandle(svc, projects)
	pages.GET("/comments", adminui.PageAuthz("/api/comment/list"), pageHandle.CommentsPage)
}
