package commenthttp

import (
	"github.com/gin-gonic/gin"
	pageview "go_wp/pkg/view"

	commentcontract "go_wp/internal/module/comment/contract"
	mw "go_wp/pkg/middleware"
)

const CommentsPageTemplate = "admin/comment/comments.html"

func SetupCommentPages(reg *mw.Registry, pages *gin.RouterGroup, svc commentcontract.CommentService,
	projects commentProjectLister) {
	if pages == nil {
		return
	}
	pageHandle := NewCommentPageHandle(svc, projects)
	pages.GET("/comments",
		mw.PageAuthz("/api/comment/list"),
		pageview.Slot(CommentsPageTemplate, pageview.RenderPage),
		pageHandle.CommentsPage)
	mw.DeclarePageGetOn(reg, "/admin/comments", "/api/comment/list")
}
