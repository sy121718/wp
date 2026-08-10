// Package dataruleservice datarule 模块业务逻辑实现。
package dataruleservice

import (
	datarulecontract "go_wp/internal/module/datarule/contract"
	datamodel "go_wp/internal/module/datarule/model"
	deptcontract "go_wp/internal/module/dept/contract"
	rolecontract "go_wp/internal/module/role/contract"
	"go_wp/pkg/datarule"
)

// 编译期断言：Service 实现了 datarulecontract.DataRuleService 和 datarule.RuleProvider 接口。
var _ datarulecontract.DataRuleService = (*Service)(nil)
var _ datarule.RuleProvider = (*Service)(nil)

// Service 数据规则业务逻辑。
type Service struct {
	rm      *datamodel.SysRuleModel
	ram     *datamodel.SysRuleAssignmentModel
	roleSvc rolecontract.RoleService
	deptSvc deptcontract.DeptService
}

// NewService 创建数据规则服务实例。
func NewService(
	rm *datamodel.SysRuleModel,
	ram *datamodel.SysRuleAssignmentModel,
	roleSvc rolecontract.RoleService,
	deptSvc deptcontract.DeptService,
) *Service {
	return &Service{
		rm:      rm,
		ram:     ram,
		roleSvc: roleSvc,
		deptSvc: deptSvc,
	}
}
