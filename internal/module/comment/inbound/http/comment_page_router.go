package commenthttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"

	mw "go_wp/pkg/middleware"
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
		mw.PageAuthz("/api/comment/list"),
		pageview.Slot(CommentsPageTemplate, pageview.RenderPage),
		pageHandle.CommentsPage)
}
