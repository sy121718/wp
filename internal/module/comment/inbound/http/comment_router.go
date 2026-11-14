package commenthttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	commentcontract "go_wp/internal/module/comment/contract"
	commentenums "go_wp/internal/module/comment/enums"
	commentmodel "go_wp/internal/module/comment/model"
	commentservice "go_wp/internal/module/comment/service"
	mw "go_wp/pkg/middleware"
	pageview "go_wp/pkg/view"
)

func SetupCommentRoutes(rg *mw.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	projects commentProjectLister, entityTypes []commentcontract.EntityType) commentcontract.CommentService {
	if rg == nil {
		return nil
	}
	svc := commentservice.NewService(commentmodel.NewModel(db), entityTypes)
	handle := NewHandle(svc)

	g := rg.Group("/comment")

	g.GET("/list", commentenums.CommentList, handle.AdminList)

	g.POST("/review", commentenums.CommentReview, pageview.HTMLFragment, handle.Review)

	SetupCommentPages(rg.Registry(), pages, svc, projects)

	return svc
}
