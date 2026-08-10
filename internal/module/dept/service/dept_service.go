// Package deptservice 部门业务逻辑层。
// 提供部门的增删改查、树形结构管理，以及部门与用户的关联管理。
package deptservice

import (
	admincontract "go_wp/internal/module/admin/contract"
	deptcontract "go_wp/internal/module/dept/contract"
	deptmodel "go_wp/internal/module/dept/model"
)

var _ deptcontract.DeptService = (*Service)(nil)

// Service 部门业务逻辑。
// 依赖 deptmodel 操作 sys_dept 表，依赖 admincontract 操作 sys_admin 表的部门归属。
type Service struct {
	dm       *deptmodel.DeptModel
	adminSvc admincontract.AdminService
}

// NewService 创建部门 Service。
// dm 是部门数据库模型，负责 sys_dept 表的持久化操作。
// adminSvc 是 admin 模块契约，用于查询/分配部门下的用户。
func NewService(dm *deptmodel.DeptModel, adminSvc admincontract.AdminService) *Service {
	return &Service{dm: dm, adminSvc: adminSvc}
}