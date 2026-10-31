package contenthttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	"gorm.io/gorm"
	"time"

	"go_wp/internal/builder/core"
	contentcontract "go_wp/internal/module/content/contract"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	"go_wp/internal/permission"
	"go_wp/pkg/i18n"
)

func SetupContentRoutes(rg *permission.RouteGroup, db *gorm.DB, collections core.CollectionSchemaProvider) contentcontract.ContentService {
	svc := contentservice.NewService(contentmodel.NewModel(db))

	svc.SetContentStore(i18n.NewDBContentStore(db))
	handle := NewHandle(svc)
	handle.SetCollectionSchemas(collections)

	g := rg.Group("/content")
	g.POST("/create", permission.ContentCreate, handle.Create)
	g.POST("/update", permission.ContentUpdate, handle.Update)
	g.GET("/get", permission.ContentGet, handle.Get)
	g.GET("/list", permission.ContentList, handle.List)

	g.GET("/collections", permission.ContentCollections, handle.Collections)
	g.POST("/delete", permission.ContentDelete, handle.Delete)
	return svc
}

func SetupContentPublicRoutes(router *gin.Engine, reader contentcontract.PublicSearchReader) {
	permission.NewRouteGroup(router.Group("")).GET("/api/site/search", permission.Exempt, builtin.RequestRateLimitMiddleware(90, time.Minute), NewContentPublicHandle(reader).Search)
}
