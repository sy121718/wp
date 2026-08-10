// Package datarulecontract 数据权限模块对外暴露的契约接口。
package datarulecontract

import (
	"context"

	dataruledto "go_wp/internal/module/datarule/dto"
)

// DataRuleService 定义数据权限模块对外暴露的业务能力。
type DataRuleService interface {
	// List 规则列表（分页 + 筛选）。
	List(ctx context.Context, req *dataruledto.ListReq) (*dataruledto.ListResp, error)
	// Detail 规则详情。
	Detail(ctx context.Context, req *dataruledto.DetailReq) (*dataruledto.DetailResp, error)
	// Create 新建规则。
	Create(ctx context.Context, req *dataruledto.CreateReq) error
	// Update 更新规则。
	Update(ctx context.Context, req *dataruledto.UpdateReq) error
	// Delete 批量删除规则。
	Delete(ctx context.Context, req *dataruledto.DeleteReq) error
	// SchemaList 返回所有已注册 domain。
	SchemaList(ctx context.Context) ([]dataruledto.DomainItem, error)
	// SchemaDetail 返回 domain 的字段白名单。
	SchemaDetail(ctx context.Context, req *dataruledto.SchemaDetailReq) (*dataruledto.DomainDetail, error)
	// AssignmentList 查询规则分配列表。
	AssignmentList(ctx context.Context, req *dataruledto.AssignmentListReq) (*dataruledto.AssignmentListResp, error)
	// AssignmentSave 批量保存规则分配。
	AssignmentSave(ctx context.Context, req *dataruledto.AssignmentSaveReq) error
}