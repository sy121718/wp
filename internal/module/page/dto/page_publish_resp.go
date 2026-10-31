package pagedto

type LangPublishResp struct {
	Lang string `json:"lang"`

	Status string `json:"status"`

	ActiveHash string `json:"activeHash,omitempty"`

	Error string `json:"error,omitempty"`
}

type PublishAllResp struct {
	PageID  string            `json:"pageId"`
	Results []LangPublishResp `json:"results"`

	Published int `json:"published"`

	Skipped int `json:"skipped"`
}

type PublishResp struct {
	PageID      string `json:"pageId"`
	Status      string `json:"status"`
	StagedHash  string `json:"stagedHash,omitempty"`
	ActiveHash  string `json:"activeHash,omitempty"`
	OldPath     string `json:"oldPath,omitempty"`
	DraftPath   string `json:"draftPath"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

type RebuildArtifactResp struct {
	ArtifactID   string `json:"artifactId"`
	Lang         string `json:"lang"`
	Path         string `json:"path"`
	ExpectedHash string `json:"expectedHash"`
	ActualHash   string `json:"actualHash"`

	Restored bool `json:"restored"`

	HashMatched bool `json:"hashMatched"`

	AlreadyThere bool `json:"alreadyThere"`

	Reason string `json:"reason"`
}

type PublicationIssueResp struct {
	URLPath string `json:"urlPath"`
	Link    string `json:"link"`
	Reason  string `json:"reason"`
}

type PublicationAuditResp struct {
	Checked int                    `json:"checked"`
	Issues  []PublicationIssueResp `json:"issues"`
	Healthy bool                   `json:"healthy"`

	Orphans []OrphanArtifactResp `json:"orphans"`

	OrphanChecked int `json:"orphanChecked"`
}

type OrphanArtifactResp struct {
	Hash  string `json:"hash"`
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}

type GCRecoveredArtifactResp struct {
	ID           string `json:"id"`
	ArtifactHash string `json:"artifactHash"`
	Lang         string `json:"lang"`

	Action string `json:"action"`
	Reason string `json:"reason"`
}

type GCArtifactsResp struct {
	RetentionDays int                       `json:"retentionDays"`
	DryRun        bool                      `json:"dryRun"`
	Scanned       int                       `json:"scanned"`
	Deleted       int                       `json:"deleted"`
	SkippedShared int                       `json:"skippedShared"`
	Failed        int                       `json:"failed"`
	Items         []GCRecoveredArtifactResp `json:"items"`

	OrphanObjects          int64 `json:"orphanObjects"`
	ObjectsDeleted         int64 `json:"objectsDeleted"`
	ObjectsSkippedExternal int   `json:"objectsSkippedExternal"`
	ObjectsFailed          int   `json:"objectsFailed"`
}
