// Package artifactcontract 定义 artifact 模块对外能力。
package artifactcontract

import (
	"context"
	"time"

	artifactdto "go_wp/internal/module/artifact/dto"
)

// 请求/响应 DTO 重导出：跨模块调用方只依赖 contract，不直接 import artifact/dto。
type (
	RecordReq     = artifactdto.RecordReq
	DetailReq     = artifactdto.DetailReq
	DetailByIDReq = artifactdto.DetailByIDReq
	ArtifactResp  = artifactdto.ArtifactResp

	GCCandidateResp = artifactdto.GCCandidateResp
)

// 产物负载状态（跨模块传值用；与 page_artifacts 的 CHECK 约束逐字对应）。
const (
	PayloadStateAvailable = "available"
	PayloadStateGCPending = "gc_pending"
	PayloadStateDeleted   = "deleted"
)

// ArtifactService 不可变构建产物归档能力。
type ArtifactService interface {
	// Record 把已落盘的 pipeline Artifact 元数据与内容对象闭包写入数据库。
	// req.Lang 为空时按站点默认语言归档（i18n.default_lang）。
	Record(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)
	// EnsureRecord 幂等归档：同 hash 返回现记录；同 (pageId, version, lang) 重构建
	// 时替换该行产物指针（编译器升级场景）；否则新建。
	// 语言维度：替换只在同一语言内发生，同页不同语言各占一行、互不覆盖。
	EnsureRecord(ctx context.Context, req *artifactdto.RecordReq) (res *artifactdto.ArtifactResp, err error)
	// Detail 按 (pageId, hash) 查询产物元数据（hash 覆盖 Manifest.lang，无需语言参数）。
	Detail(ctx context.Context, req *artifactdto.DetailReq) (res *artifactdto.ArtifactResp, err error)
	// DetailByID 按产物行 ID 查询产物元数据。
	DetailByID(ctx context.Context, req *artifactdto.DetailByIDReq) (res *artifactdto.ArtifactResp, err error)
	// ListPageIDsByOtherRegistryVersion 返回「存在 registry_version 与 current 不同的
	// 可用产物」的页面 ID（去重、字典序）。
	// 用途：部署新组件后的全站待重建识别 —— 组件编译进二进制，没有运行时事件能
	// 提示「已有产物由旧组件产出」，只能靠产物元数据里的版本号比对。
	ListPageIDsByOtherRegistryVersion(ctx context.Context, current string) (ids []string, err error)
	// ListGCCandidates 列出可回收候选：payload_state=available、早于 before、
	// 且不在 excludeIDs（保护集合）内。
	// excludeIDs 为空表示调用方无法确定保护集合 —— 此时返回空列表（宁可不回收也不误删）。
	ListGCCandidates(ctx context.Context, before time.Time, excludeIDs []string) (list []artifactdto.GCCandidateResp, err error)
	// CountOtherAvailableByHash 统计同 hash 的其他 available 行数。
	// 产物是内容寻址的（artifacts/<hash>/），多条元数据行可能指向同一份文件 ——
	// 只有返回 0 时删除物理文件才安全。
	CountOtherAvailableByHash(ctx context.Context, hash, excludeID string) (n int64, err error)
	// MarkPayloadState 批量更新负载状态（gc_pending / deleted），返回受影响行数。
	MarkPayloadState(ctx context.Context, ids []string, state string) (n int64, err error)
}
