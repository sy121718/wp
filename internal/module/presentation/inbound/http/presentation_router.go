package presentationhttp

// presentation_router.go — presentation 模块路由自装配（0-A2）。

import (
	"go_wp/internal/builder/core"
	blockcontract "go_wp/internal/module/block/contract"
	contenttemplatecontract "go_wp/internal/module/contenttemplate/contract"
	presentationcontract "go_wp/internal/module/presentation/contract"
	presentationmodel "go_wp/internal/module/presentation/model"
	presentationservice "go_wp/internal/module/presentation/service"
	projectcontract "go_wp/internal/module/project/contract"
	pubcontract "go_wp/internal/module/publication/contract"
	"go_wp/internal/permission"

	"gorm.io/gorm"
)

// SetupPresentationRoutes 装配 presentation 模块路由，返回模块契约。
// project 用于解析实例所属工程（presentation_instances.project_id 为 NOT NULL 外键）；
// blocks 用于内容模板内部的全局块引用展开（core.globalref，构建期内联）；
// collections 为集合源解析器（issue #9）：模板内的集合类组件构建期展开为静态列表。
func SetupPresentationRoutes(rg *permission.RouteGroup, db *gorm.DB,
	templates contenttemplatecontract.ContentTemplateService,
	registry core.EntitySourceRegistry,
	project projectcontract.ProjectService,
	blocks blockcontract.BlockService,
	collections core.CollectionResolver,
	publication pubcontract.PublicationService) presentationcontract.PresentationService {
	svc := presentationservice.NewService(presentationmodel.NewModel(db), templates, registry, project, blocks, publication)
	svc.SetCollectionResolver(collections)
	handle := NewHandle(svc)

	g := rg.Group("/presentation")
	g.POST("/create", permission.PresentationCreate, handle.CreateInstance)
	g.POST("/rebuild", permission.PresentationRebuild, handle.Rebuild)
	g.GET("/get", permission.PresentationGet, handle.Get)
	g.GET("/list", permission.PresentationList, handle.List)
	g.POST("/delete", permission.PresentationDelete, handle.Delete)
	// 按内容实体查实例（issue #14）：后台「详情页模板」页读当前绑定的模板与发布状态。
	g.GET("/get-by-entity", permission.PresentationGetByEntity, handle.GetByEntity)
	// 发布前预览（issue #14 验收 3）：按指定/默认模板只读渲染，不落库不激活。
	// 与 create/rebuild 分开，便于按「只读预览」单独授权。
	g.POST("/preview", permission.PresentationPreview, handle.Preview)
	// 改 URL（已发布详情页的线上路径变更）：新路径激活 + 旧路径 301/取消激活。
	// 独立端点而非复用 create：改 URL 不是重建，它还要处置旧路径与路由占用。
	g.POST("/update-url", permission.PresentationUpdateURL, handle.UpdateURL)
	// 多语言发布回执的收敛调度（审计 AR2-004 + 与 page 侧对称的定时兜底）：
	// 上次进程若在多语言发布的「已切换访问面、未登记路由 / 未结案」之间失败或崩溃，
	// 按符号链接的实际指向补齐路由登记并结案（判据不足则结案为未生效）。
	//
	// 入口只有一个：调度器内部先做一次全量启动恢复（不限批 + 5 分钟预算），
	// 之后由 ticker 与写路径快通道共同驱动（见 presentation/service/presentation_publish.go 的 ConvergePendingReceipts）。
	// 不再另起一个裸启动恢复 goroutine —— 同一段实现有两个「启动时跑一次」的驱动源，
	// 启动瞬间会有两个 goroutine 并发重放同一批回执。
	presentationservice.StartPendingReceiptConvergenceScheduler(svc)
	return svc
}
