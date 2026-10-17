package artifactdto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

// ArtifactResp 产物元数据投影；CanonicalPath 从 manifest 提取。
type ArtifactResp struct {
	ID               string          `json:"id"`
	PageID           string          `json:"pageId"`
	Version          int64           `json:"version"`
	Lang             string          `json:"lang"`
	SourceDocument   json.RawMessage `json:"sourceDocument,omitempty"`
	SourceHash       string          `json:"sourceHash"`
	BuildInputHash   string          `json:"buildInputHash"`
	ArtifactProvider string          `json:"artifactProvider"`
	ArtifactKey      string          `json:"artifactKey"`
	ArtifactHash     string          `json:"artifactHash"`
	CanonicalPath    string          `json:"canonicalPath"`
	CompilerVersion  string          `json:"compilerVersion"`
	RegistryVersion  string          `json:"registryVersion"`
	Manifest         json.RawMessage `json:"manifest,omitempty"`
	PayloadState     string          `json:"payloadState"`
	CreatedBy        string          `json:"createdBy"`
	CreatedAt        utils.JSONTime  `json:"createdAt"`
}

// GCCandidateResp 单条可回收产物候选（产物 GC 用）。
type GCCandidateResp struct {
	ID           string         `json:"id"`
	PageID       string         `json:"pageId"`
	Version      int64          `json:"version"`
	Lang         string         `json:"lang"`
	ArtifactHash string         `json:"artifactHash"`
	ArtifactKey  string         `json:"artifactKey"`
	CreatedAt    utils.JSONTime `json:"createdAt"`
}

// OrphanContentObjectResp 单条孤儿内容对象。
type OrphanContentObjectResp struct {
	ContentHash string         `json:"contentHash"`
	Provider    string         `json:"provider"`
	ObjectKey   string         `json:"objectKey"`
	ByteSize    int64          `json:"byteSize"`
	CreatedAt   utils.JSONTime `json:"createdAt"`
	// Action: would_delete / deleted / kept_external / kept_reclaimed / delete_failed
	Action string `json:"action"`
	Reason string `json:"reason"`
}

// ContentObjectGCResp 内容对象回收报告。
type ContentObjectGCResp struct {
	RetentionDays int   `json:"retentionDays"`
	DryRun        bool  `json:"dryRun"`
	Orphans       int64 `json:"orphans"`
	Scanned       int   `json:"scanned"`
	Deleted       int64 `json:"deleted"`
	SkippedExtern int   `json:"skippedExternal"`
	Failed        int   `json:"failed"`
	// FailedRate 失败率（Failed ÷ Scanned）。
	//
	// 显式放进响应而不是让监控自己算：GC 的失败口径一向是「记进统计 + 打日志」，
	// 调用方拿不到 error，外键挡删那条因此长期静默（内容对象表只增不减）。
	// 有这个比率，外部按 FailedRate > 0 轮询告警即可 —— 正常一轮应为 0，
	// 非零一定是异常（要么整批删除报错，要么单条删除没生效）。
	FailedRate float64                   `json:"failedRate"`
	Items      []OrphanContentObjectResp `json:"items"`
}

// PageArtifactMissRow 一个（页面 × 语言）最新产物的缺译计数。
//
// **两个量的量纲不同，不能相除**：Candidates 是去重后的可翻译字段数，
// Misses 是渲染期取词未命中的**调用次数**（同一字段渲染多次就计多次），
// 因此展示只给条数、不给百分比（misses > candidates 是正常的）。
//
// 不含 draft_path：那个列在 pages 表（调用方自己的表），本模块不替它读。
type PageArtifactMissRow struct {
	PageID     string
	Lang       string
	Version    int64
	Misses     int64
	Candidates int64
}
