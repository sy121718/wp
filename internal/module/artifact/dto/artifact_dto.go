package artifactdto

import (
	"encoding/json"
	"go_wp/pkg/utils"
)

// RecordReq 归档一条已落盘产物的元数据；闭包对象由服务端从 manifest 提取。
//
// Lang 是产物行唯一键 (pageId, version, lang) 的第三维（多语言 P3）：
// 空 = 全局默认语言（sys_config 的 i18n 组 default_lang），service 落库前统一归一化，绝不留空。
type RecordReq struct {
	ArtifactID       string          `json:"artifactId" binding:"required"`
	PageID           string          `json:"pageId" binding:"required"`
	Version          int64           `json:"version" binding:"required,min=1"`
	Lang             string          `json:"lang"`
	SourceDocument   json.RawMessage `json:"sourceDocument" binding:"required"`
	SchemaVersion    int             `json:"schemaVersion" binding:"required,min=1"`
	SourceHash       string          `json:"sourceHash" binding:"required"`
	BuildInputHash   string          `json:"buildInputHash" binding:"required"`
	ArtifactProvider string          `json:"artifactProvider" binding:"required"`
	ArtifactKey      string          `json:"artifactKey" binding:"required"`
	ArtifactHash     string          `json:"artifactHash" binding:"required"`
	CompilerVersion  string          `json:"compilerVersion" binding:"required"`
	RegistryVersion  string          `json:"registryVersion" binding:"required"`
	Manifest         json.RawMessage `json:"manifest" binding:"required"`
	CreatedBy        string          `json:"createdBy"`
}

// DetailReq 按 pageId + hash 查询产物。
// 不带 lang：产物 hash 覆盖 Manifest（含 lang），同 hash 必同语言。
type DetailReq struct {
	PageID string `form:"pageId" json:"pageId" binding:"required"`
	Hash   string `form:"hash" json:"hash" binding:"required"`
}

// DetailByIDReq 按产物行 ID 查询。
type DetailByIDReq struct {
	ID string `form:"id" json:"id" binding:"required"`
}

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

// ContentObjectGCReq 共享内容对象（content_objects）孤儿回收请求。
//
// 与产物 GC 同形：DryRun 默认 true，真删必须显式传 false；保留窗口同样按天给。
// Limit 是本批处理上限（标记清除分批做，避免一次干掉几十万行造成长事务）。
type ContentObjectGCReq struct {
	// RetentionDays 保留窗口（天）：早于 now-retentionDays 创建的对象才可能被回收。
	// 留空或 <=0 按默认 30 天。
	RetentionDays int `json:"retentionDays"`
	// DryRun 只统计与列出候选、不实际删除。留空视为 true（安全默认）。
	DryRun *bool `json:"dryRun"`
	// Limit 本批处理行数上限，留空按默认批次。
	Limit int `json:"limit"`
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
