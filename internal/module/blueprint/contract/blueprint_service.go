// Package blueprintcontract blueprint 模块对外契约（0-B）。
package blueprintcontract

import (
	"context"
	"encoding/json"

	blueprintdto "go_wp/internal/module/blueprint/dto"
)

// BlueprintService Page 初始化工具 Blueprint 契约（docs/02-domain.md §1.2）。
// Blueprint 是创建 Page Document 的版本化初始化输入，用完即弃：创建 Page 时
// 经 InitPageDocument 复制完整 AST 并递归生成新 Node ID，得到不再依赖
// Blueprint 的独立 Page Document；后续修改不传播、不参与构建期。
type BlueprintService interface {
	// Create 创建 Blueprint（初始 draft_version=1 并写入 version=1 快照）。
	Create(ctx context.Context, req *blueprintdto.CreateReq) (res *blueprintdto.BlueprintResp, err error)
	// Update 修改草稿 → draft_version 递增 + 写入新不可变版本。
	Update(ctx context.Context, req *blueprintdto.UpdateReq) (res *blueprintdto.BlueprintResp, err error)
	// Publish 发布：基于当前草稿生成不可变版本（版本号 = draft_version）。
	Publish(ctx context.Context, req *blueprintdto.PublishReq) (res *blueprintdto.BlueprintResp, err error)
	// Get 按 ID 查询。
	Get(ctx context.Context, req *blueprintdto.GetReq) (res *blueprintdto.BlueprintResp, err error)
	// List 按 kind 列表。
	List(ctx context.Context, req *blueprintdto.ListReq) (list []*blueprintdto.BlueprintResp, err error)
	// Delete 删除 Blueprint。
	Delete(ctx context.Context, req *blueprintdto.DeleteReq) (err error)
	// InitPageDocument 取最新版本 document，递归复制 AST 并为每个节点生成新
	// UUID，返回不再依赖 Blueprint 的完整 Page Document（page 模块 CreatePage 调用）。
	InitPageDocument(ctx context.Context, blueprintID string) (doc json.RawMessage, err error)
}
