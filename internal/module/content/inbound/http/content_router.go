package contenthttp

import (
	"github.com/gin-gonic/gin"
	"go_wp/internal/middleware/builtin"
	"gorm.io/gorm"
	"time"

	contentcontract "go_wp/internal/module/content/contract"
	contentenums "go_wp/internal/module/content/enums"
	contentmodel "go_wp/internal/module/content/model"
	contentservice "go_wp/internal/module/content/service"
	"go_wp/pkg/builder/core"
	"go_wp/pkg/i18n"
	mw "go_wp/pkg/middleware"
)

func SetupContentRoutes(rg *mw.RouteGroup, db *gorm.DB, collections core.CollectionSchemaProvider) contentcontract.ContentService {
	svc := contentservice.NewService(contentmodel.NewModel(db))

	svc.SetContentStore(i18n.NewDBContentStore(db))
	handle := NewHandle(svc)
	handle.SetCollectionSchemas(collections)

	g := rg.Group("/content")
	g.POST("/create", contentenums.ContentCreate, handle.Create)
	g.POST("/update", contentenums.ContentUpdate, handle.Update)
	g.GET("/get", contentenums.ContentGet, handle.Get)
	g.GET("/list", contentenums.ContentList, handle.List)

	g.GET("/collections", contentenums.ContentCollections, handle.Collections)
	g.POST("/delete", contentenums.ContentDelete, handle.Delete)
	return svc
}

func SetupContentPublicRoutes(router *gin.Engine, reader contentcontract.PublicSearchReader) {
	mw.NewRouteGroup(router.Group(""), mw.RouteProtected).GET("/api/site/search", "", builtin.RequestRateLimitMiddleware(90, time.Minute), NewContentPublicHandle(reader).Search)
}
