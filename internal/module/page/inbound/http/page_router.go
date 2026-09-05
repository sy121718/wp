package pagehttp

import (
	"go_wp/internal/middleware/builtin"

	blockcontract "go_wp/internal/module/block/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	projectcontract "go_wp/internal/module/project/contract"

	artifactcontract "go_wp/internal/module/artifact/contract"
	pubcontract "go_wp/internal/module/publication/contract"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupPageRoutes 自装配 page 模块并注册草稿、修订与发布路由。
func SetupPageRoutes(rg *gin.RouterGroup, db *gorm.DB,
	artifacts artifactcontract.ArtifactService,
	routes pubcontract.PublicationService,
	projectService projectcontract.ProjectService,
	blocks blockcontract.BlockService) pagecontract.PageService {
	model := pagemodel.NewPageModel(db)
	svc := pageservice.NewService(model, artifacts, routes, projectService, blocks)
	handle := NewHandle(svc)

	g := rg.Group("/page", builtin.SessionAuthMiddleware())
	g.POST("/create", handle.Create)
	g.GET("/list", handle.List)
	g.GET("/detail", handle.Detail)
	g.POST("/draft/save", handle.SaveDraft)
	g.GET("/revision/list", handle.ListRevisions)
	g.POST("/build", handle.Build)
	g.POST("/publish", handle.Publish)
	g.POST("/rollback", handle.Rollback)
	g.POST("/url/update", handle.UpdateURL)
	return svc
}
