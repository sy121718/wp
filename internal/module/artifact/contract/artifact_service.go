// Package artifactcontract 定义 artifact 模块对外能力。
package artifactcontract

import (
	"context"

	artifactdto "go_wp/internal/module/artifact/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import artifact/dto。
type (
	RecordReq     = artifactdto.RecordReq
	DetailReq     = artifactdto.DetailReq
	DetailByIDReq = artifactdto.DetailByIDReq
	ArtifactResp  = artifactdto.ArtifactResp
)

// ArtifactService 不可变构建产物归档能力。
type ArtifactService interface {
	// Record 把已落盘的 pipeline Artifact 元数据与内容对象闭包写入数据库。
	Record(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)
	// EnsureRecord 幂等归档：同 hash 返回现记录；同 (pageId, version) 重构建
	// 时替换该行产物指针（编译器升级场景）；否则新建。
	EnsureRecord(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)
	// Detail 按 (pageId, hash) 查询产物元数据。
	Detail(ctx context.Context, req *artifactdto.DetailReq) (res *artifactdto.ArtifactResp, err error)
	// DetailByID 按产物行 ID 查询产物元数据。
	DetailByID(ctx context.Context, req *artifactdto.DetailByIDReq) (res *artifactdto.ArtifactResp, err error)
}
