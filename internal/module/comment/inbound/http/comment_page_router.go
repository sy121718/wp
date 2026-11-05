package commenthttp

import (
	pageview "go_wp/pkg/view"
	"github.com/gin-gonic/gin"

	"go_wp/internal/adminui"
	commentcontract "go_wp/internal/module/comment/contract"
)

const CommentsPageTemplate = "admin/comment/comments.html"

func SetupCommentPages(pages *gin.RouterGroup, svc commentcontract.CommentService,
	projects commentProjectLister) {
	if pages == nil {
		return
	}
	pageHandle := NewCommentPageHandle(svc, projects)
	pages.GET("/comments",
		adminui.PageAuthz("/api/comment/list"),
		pageview.Slot(CommentsPageTemplate, adminui.RenderPage),
		pageHandle.CommentsPage)
}
