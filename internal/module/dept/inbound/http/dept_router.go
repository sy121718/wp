// Package depthttp 部门模块 HTTP 层。
// 提供部门管理的 REST 接口：树形查询、详情、增删改、部门用户列表与分配。
package depthttp

import (
	"go_wp/internal/middleware/builtin"
	admincontract "go_wp/internal/module/admin/contract"
	deptcontract "go_wp/internal/module/dept/contract"
	deptmodel "go_wp/internal/module/dept/model"
	deptservice "go_wp/internal/module/dept/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupDeptRoutes 装配部门模块路由。
// 创建 model、service、handle，在 /dept 分组下注册所有接口，统一使用 JWT 认证中间件。
func SetupDeptRoutes(rg *gin.RouterGroup, db *gorm.DB, adminSvc admincontract.AdminService) deptcontract.DeptService {
	if rg == nil {
		return nil
	}

	m := deptmodel.NewDeptModel(db)
	svc := deptservice.NewService(m, adminSvc)
	handle := NewHandle(svc)

	g := rg.Group("/dept").Use(
		builtin.JWTAuthMiddleware(),
		builtin.CasbinMiddleware(),
	)
	{
		g.GET("/tree", handle.Tree)
		g.GET("/detail", handle.Detail)
		g.POST("/create", handle.Create)
		g.POST("/update", handle.Update)
		g.POST("/delete", handle.Delete)
		g.GET("/user/list", handle.UserList)
		g.POST("/user/save", handle.UserSave)
	}

	return svc
}
