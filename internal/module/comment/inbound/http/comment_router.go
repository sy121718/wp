// comment_router.go — comment 模块路由自装配（BIZ-5）。
//
// 两组路由：
//
//	· rg（authorizedAPI，前缀 /api，三层链 Session + CSRF + Casbin）—— JSON 接口；
//	· pages（后台页面组，前缀 /admin，Session + CSRF + 权限上下文由装配层统一挂）—— 整页渲染。
//
// 页面上的写动作走 pages 组 + builtin.CasbinMiddlewareForPath("/api/comment/review")，
// **复用接口的权限点**：权限点声明的真源是 rg 那边的注册动作，一个字符都不能改
// （路径换了而权限点没换，结果是页面按钮人人可见但提交必 403，或者更糟 —— 提交成功
// 但用的是另一个权限）。形态与 membership_router.go 一致。
//
// 实体类型白名单**不在这里定义**：由装配层从拥有该实体的模块（content / product）取
// 常量后传进来（见 SetupCommentRoutes 的 entityTypes 参数）。本模块不抄一份取值表 ——
// 抄一份的下场是「新增一种可评论实体时改了拥有者、忘了这里」，表现为新实体静默不可评论。
package commenthttp

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	commentcontract "go_wp/internal/module/comment/contract"
	commentmodel "go_wp/internal/module/comment/model"
	commentservice "go_wp/internal/module/comment/service"
	"go_wp/internal/permission"
)

// SetupCommentRoutes 装配 comment 模块路由，返回模块契约。
//
// rg 为 nil 时早退（与本仓其它模块的 Setup 同构）；pages 为 nil 时跳过页面注册。
// entityTypes 为空时模块仍装配成功，但**所有**提交与列表都会被拒（fail-closed：
// 没人注册实体类型说明装配漏了，而静默接受任意类型会让白名单形同虚设）。
func SetupCommentRoutes(rg *permission.RouteGroup, pages *gin.RouterGroup, db *gorm.DB,
	projects commentProjectLister, entityTypes []commentcontract.EntityType) commentcontract.CommentService {
	if rg == nil {
		return nil
	}
	svc := commentservice.NewService(commentmodel.NewModel(db), entityTypes)
	handle := NewHandle(svc)

	g := rg.Group("/comment")
	// 审核队列（读）。
	g.GET("/list", permission.CommentList, handle.AdminList)
	// 批量通过 / 驳回（写）。
	g.POST("/review", permission.CommentReview, handle.Review)

	// 后台页面：注册在 comment_page_router.go（同一入口调用，装配顺序不变）。
	SetupCommentPages(pages, svc, projects)

	return svc
}
