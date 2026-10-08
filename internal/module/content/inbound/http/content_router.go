package contenthttp

// 与原 dashboard 版 router_article.go 的差异只有两处：
//   - 内容模板页（/admin/content-templates*）归 contenttemplate 模块，不在这里注册；
//   - 页面注册从「模块 Setup 内部」提到独立导出函数，并落到 content_page_router.go。
//
// 权限点一个字符都没动：页面注册见 content_page_router.go（页面 GET 的 Casbin 待补，
// 见 docs/02-Z-admin-menu-code-and-page-authz.md §4.3），写动作复用既有
// content:* / page:create / presentation:* 权限点（迁移 033）。

// 挂 authorizedAPI 三层链（SessionAuth + CSRF + Casbin）。

import (
	"gorm.io/gorm"

	"go_wp/internal/builder/core"
	"go_wp/internal/module/content/contract"
	"go_wp/internal/module/content/model"
	"go_wp/internal/module/content/service"
	"go_wp/internal/permission"
	"go_wp/pkg/i18n"
)

// SetupContentRoutes 装配 content 模块路由，返回模块契约。
//
// collections 为集合源元数据聚合端口（装配期的集合源注册表）：集合源已跨模块
// （商品同样是集合源，issue #9），元数据接口必须返回全量 —— 注册表在装配期
// 后续步骤才填充完成，这里只持有指针，请求到来时已是全量。
func SetupContentRoutes(rg *permission.RouteGroup, db *gorm.DB, collections core.CollectionSchemaProvider) contentcontract.ContentService {
	svc := contentservice.NewService(contentmodel.NewModel(db))
	// 内容译文存储（审计 I18N-006）：构建期按语言取字段译文。
	// 与商品域同一注入方式、同一张表（sys_translation）—— 两个模块取词口径一致，
	// 运营也在同一个翻译工作台里维护，不必区分「这条是商品的还是文章的」。
	svc.SetContentStore(i18n.NewDBContentStore(db))
	handle := NewHandle(svc)
	handle.SetCollectionSchemas(collections)

	g := rg.Group("/content")
	g.POST("/create", permission.ContentCreate, handle.Create)
	g.POST("/update", permission.ContentUpdate, handle.Update)
	g.GET("/get", permission.ContentGet, handle.Get)
	g.GET("/list", permission.ContentList, handle.List)
	// 集合源元数据：内置组件集合字段白名单 + 工作台字段下拉。
	g.GET("/collections", permission.ContentCollections, handle.Collections)
	g.POST("/delete", permission.ContentDelete, handle.Delete)
	return svc
}
