package artifactdto

import (
	"encoding/json"
	"time"
)

// RecordReq 归档一条已落盘产物的元数据；闭包对象由服务端从 manifest 提取。
type RecordReq struct {
	ArtifactID       string          `json:"artifactId" binding:"required"`
	PageID           string          `json:"pageId" binding:"required"`
	Version          int64           `json:"version" binding:"required,min=1"`
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
	CreatedAt        time.Time       `json:"createdAt"`
}
