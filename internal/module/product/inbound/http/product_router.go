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
	// 组合生成（issue #8）：勾选属性值 → 笛卡尔积写变体（幂等，超上限整体拒绝）。
	g.POST("/variant/generate", handle.GenerateVariants)
	g.POST("/variant/update", handle.UpdateVariant)
	g.POST("/variant/delete", handle.DeleteVariant)

	// 属性组与属性值（issue #7）：属性组可跨商品复用，故独立于商品资源。
	// 属性值用 set-values 整体保存（全量替换），比逐行接口少一半请求、也避免半截状态。
	g.GET("/attribute/list", handle.ListAttributes)
	g.GET("/attribute/get", handle.GetAttribute)
	g.POST("/attribute/create", handle.CreateAttribute)
	g.POST("/attribute/update", handle.UpdateAttribute)
	g.POST("/attribute/set-values", handle.SetAttributeValues)
	g.POST("/attribute/delete", handle.DeleteAttribute)

	// 分类与品牌（issue #10）：分类是树（list 返回树、get 取单节点），品牌是平铺列表。
	// 分类与品牌都可跨商品复用，故与商品资源并列而不是嵌在商品路径下。
	g.GET("/category/list", handle.ListCategories)
	g.GET("/category/get", handle.GetCategory)
	g.POST("/category/create", handle.CreateCategory)
	g.POST("/category/update", handle.UpdateCategory)
	g.POST("/category/delete", handle.DeleteCategory)
	g.GET("/brand/list", handle.ListBrands)
	g.GET("/brand/get", handle.GetBrand)
	g.POST("/brand/create", handle.CreateBrand)
	g.POST("/brand/update", handle.UpdateBrand)
	g.POST("/brand/delete", handle.DeleteBrand)
	return svc
}
