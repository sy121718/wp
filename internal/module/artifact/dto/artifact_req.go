package artifactdto

import (
	json "go_wp/pkg/jsonx"
)

type RecordReq struct {
	ArtifactID       string          `json:"artifactId" form:"artifactId" binding:"required"`
	PageID           string          `json:"pageId" form:"pageId" binding:"required"`
	Version          int64           `json:"version" form:"version" binding:"required,min=1"`
	Lang             string          `json:"lang" form:"lang"`
	SourceDocument   json.RawMessage `json:"sourceDocument" form:"sourceDocument" binding:"required"`
	SchemaVersion    int             `json:"schemaVersion" form:"schemaVersion" binding:"required,min=1"`
	SourceHash       string          `json:"sourceHash" form:"sourceHash" binding:"required"`
	BuildInputHash   string          `json:"buildInputHash" form:"buildInputHash" binding:"required"`
	ArtifactProvider string          `json:"artifactProvider" form:"artifactProvider" binding:"required"`
	ArtifactKey      string          `json:"artifactKey" form:"artifactKey" binding:"required"`
	ArtifactHash     string          `json:"artifactHash" form:"artifactHash" binding:"required"`
	CompilerVersion  string          `json:"compilerVersion" form:"compilerVersion" binding:"required"`
	RegistryVersion  string          `json:"registryVersion" form:"registryVersion" binding:"required"`
	Manifest         json.RawMessage `json:"manifest" form:"manifest" binding:"required"`
	CreatedBy        string          `json:"createdBy" form:"createdBy"`
}

type DetailReq struct {
	PageID string `form:"pageId" json:"pageId" binding:"required"`
	Hash   string `form:"hash" json:"hash" binding:"required"`
}

type DetailByIDReq struct {
	ID string `form:"id" json:"id" binding:"required"`
}

type ContentObjectGCReq struct {
	RetentionDays int `json:"retentionDays" form:"retentionDays"`

	DryRun *bool `json:"dryRun" form:"dryRun"`

	Limit int `json:"limit" form:"limit"`
}
