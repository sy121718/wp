package artifactdto

import (
	json "go_wp/pkg/jsonx"
	"go_wp/pkg/utils"
)

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

type GCCandidateResp struct {
	ID           string         `json:"id"`
	PageID       string         `json:"pageId"`
	Version      int64          `json:"version"`
	Lang         string         `json:"lang"`
	ArtifactHash string         `json:"artifactHash"`
	ArtifactKey  string         `json:"artifactKey"`
	CreatedAt    utils.JSONTime `json:"createdAt"`
}

type OrphanContentObjectResp struct {
	ContentHash string         `json:"contentHash"`
	Provider    string         `json:"provider"`
	ObjectKey   string         `json:"objectKey"`
	ByteSize    int64          `json:"byteSize"`
	CreatedAt   utils.JSONTime `json:"createdAt"`

	Action string `json:"action"`
	Reason string `json:"reason"`
}

type ContentObjectGCResp struct {
	RetentionDays int   `json:"retentionDays"`
	DryRun        bool  `json:"dryRun"`
	Orphans       int64 `json:"orphans"`
	Scanned       int   `json:"scanned"`
	Deleted       int64 `json:"deleted"`
	SkippedExtern int   `json:"skippedExternal"`
	Failed        int   `json:"failed"`

	FailedRate float64                   `json:"failedRate"`
	Items      []OrphanContentObjectResp `json:"items"`
}

type PageArtifactMissRow struct {
	PageID     string
	Lang       string
	Version    int64
	Misses     int64
	Candidates int64
}
