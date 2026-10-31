package commenthttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	commentcontract "go_wp/internal/module/comment/contract"
	commentmodel "go_wp/internal/module/comment/model"
	commentservice "go_wp/internal/module/comment/service"
	"go_wp/internal/permission"
)

func SetupCommentRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	projects commentProjectLister, entityTypes []commentcontract.EntityType) commentcontract.CommentService {
	if rg == nil {
		return nil
	}
	svc := commentservice.NewService(commentmodel.NewModel(db), entityTypes)
	handle := NewHandle(svc)

	g := rg.Group("/comment")

	g.GET("/list", permission.CommentList, handle.AdminList)

	g.POST("/review", permission.CommentReview, handle.Review)

	SetupCommentPages(pages, svc, projects)

	return svc
}
