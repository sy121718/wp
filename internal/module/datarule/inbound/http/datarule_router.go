// Package datarulehttp datarule 模块 HTTP 路由注册与处理器装配。
package datarulehttp

import (
	"log"

	"go_wp/internal/middleware/builtin"
	datarulecontract "go_wp/internal/module/datarule/contract"
	datamodel "go_wp/internal/module/datarule/model"
	dataruleservice "go_wp/internal/module/datarule/service"
	deptcontract "go_wp/internal/module/dept/contract"
	rolecontract "go_wp/internal/module/role/contract"
	datarulepkg "go_wp/pkg/datarule"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// SetupResult 包含 datarule 模块对外暴露的契约实例。
type SetupResult struct {
	Service      datarulecontract.DataRuleService
	RuleProvider datarulepkg.RuleProvider
}

// SetupDataRuleRoutes 装配 model/service/handle 并注册路由。
// 初始化时注册域白名单，返回 Service 契约和 RuleProvider 给 pkg/datarule 引擎使用。
func SetupDataRuleRoutes(
	rg *gin.RouterGroup,
	db *gorm.DB,
	roleSvc rolecontract.RoleService,
	deptSvc deptcontract.DeptService,
) *SetupResult {
	// 注册域白名单
	registerDomains()

	// 装配
	rm := datamodel.NewSysRuleModel(db)
	ram := datamodel.NewSysRuleAssignmentModel(db)
	svc := dataruleservice.NewService(rm, ram, roleSvc, deptSvc)
	handle := NewHandle(svc)

	// 注册 RuleProvider 到 datarule 引擎
	datarulepkg.SetProvider(svc)
	if err := datarulepkg.RegisterPluginWithDB(db); err != nil {
		log.Printf("注册 datarule GORM 插件失败: %v", err)
	}

	// 注册路由
	if rg != nil {
		g := rg.Group("/datarule").Use(
			builtin.JWTAuthMiddleware(),
			builtin.CasbinMiddleware(),
		)
		{
			g.GET("/list", handle.List)
			g.GET("/detail", handle.Detail)
			g.POST("/create", handle.Create)
			g.POST("/update", handle.Update)
			g.POST("/delete", handle.Delete)
			g.GET("/schema/list", handle.SchemaList)
			g.GET("/schema/detail", handle.SchemaDetail)
			g.GET("/assignment/list", handle.AssignmentList)
			g.POST("/assignment/save", handle.AssignmentSave)
		}
	}

	return &SetupResult{
		Service:      svc,
		RuleProvider: svc,
	}
}

// registerDomains 注册所有数据域及字段白名单。
func registerDomains() {
	datarulepkg.RegisterDomain(datarulepkg.DomainConfig{
		Domain:      "ADMIN",
		DomainLabel: "管理员",
		TableName:   "sys_admin",
		WhiteList: []datarulepkg.FieldDef{
			{Field: "username", Label: "用户名", DataType: "varchar", Operators: []string{"EQ", "NEQ", "LIKE", "NOT_LIKE"}},
			{Field: "email", Label: "邮箱", DataType: "varchar", Operators: []string{"EQ", "NEQ", "LIKE"}},
			{Field: "phone", Label: "手机号", DataType: "varchar", Operators: []string{"EQ", "NEQ"}},
			{Field: "status", Label: "状态", DataType: "tinyint", Operators: []string{"EQ", "NEQ", "IN", "NOT_IN"}},
			{Field: "dept_id", Label: "所属部门", DataType: "bigint", Operators: []string{"EQ", "NEQ", "IN", "NOT_IN"}},
		},
	})
}
