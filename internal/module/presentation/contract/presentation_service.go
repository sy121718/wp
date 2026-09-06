// Package presentationcontract presentation 模块对外契约（0-A2）。
package presentationcontract

import (
	"context"

	"go_wp/internal/module/presentation/dto"
)

// PresentationService 内容实体自动发布实例契约（docs/02-domain.md §3）。
// 与手工 Page 共享 Publish Compiler + ArtifactStore + PublicationStore，
// 但管理路径由 CMS 实体驱动（创建/编辑/删除，不经 Visual Builder）。
type PresentationService interface {
	// CreateInstance 为内容实体创建自动发布实例：解析模板 → 派生快照 →
	// 编译 → 发布激活（urlPath 由实体 slug 推导）。
	CreateInstance(ctx context.Context, req *presentationdto.CreateInstanceReq) (res *presentationdto.InstanceResp, err error)
	// Rebuild 实体数据更新（revision 变化）后重建：重解析模板+实体 →
	// 新快照 → 重新编译发布。
	Rebuild(ctx context.Context, req *presentationdto.RebuildReq) (res *presentationdto.InstanceResp, err error)
	// Get 按 ID 查询。
	Get(ctx context.Context, req *presentationdto.GetReq) (res *presentationdto.InstanceResp, err error)
	// List 按类型列表。
	List(ctx context.Context, req *presentationdto.ListReq) (list []*presentationdto.InstanceResp, err error)
	// Delete 删除实例（级联删快照 + 反激活 URL）。
	Delete(ctx context.Context, req *presentationdto.DeleteReq) (err error)
}
