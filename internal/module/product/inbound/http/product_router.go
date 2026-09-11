// product_router.go — product 模块路由自装配（issue #5 / T3a）。
// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。
package producthttp

import (
	productcontract "go_wp/internal/module/product/contract"
	productmodel "go_wp/internal/module/product/model"
	productservice "go_wp/internal/module/product/service"
	projectcontract "go_wp/internal/module/project/contract"
	"go_wp/pkg/i18n"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupProductRoutes 装配 product 模块路由，返回模块契约。
// project 用于解析商品所属工程（products.project_id 为 NOT NULL 外键）。
//
// 装配同时注入内容译文读取端口（sys_translation）：构建期商品可翻译字段
// （name/subtitle/description）按构建语言取译文，语境 product.<字段名>。
// 端口在这里注入是因为本模块的 service 不持有 *gorm.DB（表隔离约定）。
func SetupProductRoutes(rg *gin.RouterGroup, db *gorm.DB, project projectcontract.ProjectService) productcontract.ProductService {
	svc := productservice.NewService(productmodel.NewModel(db), project)
	svc.SetContentStore(i18n.NewDBContentStore(db))
	handle := NewHandle(svc)

	g := rg.Group("/product")
	g.GET("/list", handle.List)
	g.GET("/get", handle.Get)
	g.POST("/create", handle.Create)
	g.POST("/update", handle.Update)
	g.POST("/delete", handle.Delete)
	g.POST("/variant/create", handle.CreateVariant)
	g.POST("/variant/update", handle.UpdateVariant)
	g.POST("/variant/delete", handle.DeleteVariant)
	return svc
}
