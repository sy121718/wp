package pagehttp

import (
	"go_wp/internal/middleware/builtin"

	blockcontract "go_wp/internal/module/block/contract"

	"go_wp/internal/builder/core"
	mediacontract "go_wp/internal/module/media/contract"
	navigationcontract "go_wp/internal/module/navigation/contract"
	pagecontract "go_wp/internal/module/page/contract"
	pagemodel "go_wp/internal/module/page/model"
	pageservice "go_wp/internal/module/page/service"
	plugincontract "go_wp/internal/module/plugin/contract"
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
	blocks blockcontract.BlockService,
	plugins plugincontract.PluginService,
	content core.CollectionResolver,
	navigation navigationcontract.NavigationService,
	media mediacontract.MediaService) pagecontract.PageService {
	model := pagemodel.NewPageModel(db)
	svc := pageservice.NewService(model, artifacts, routes, projectService, blocks, plugins, content, navigation, media)
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
	g.POST("/delete", handle.Delete)
	// 灾难恢复：按产物元数据重建丢失的产物文件 + 激活面巡检。
	g.POST("/artifact/rebuild", handle.RebuildArtifact)
	g.GET("/publication/audit", handle.AuditPublication)
	// 产物回收：默认 dryRun（只列候选），需显式传 dryRun=false 才实际删除。
	g.POST("/artifact/gc", handle.GarbageCollectArtifacts)
	return svc
}
