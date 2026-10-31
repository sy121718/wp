package pagedto

type BuildReq struct {
	ID              string `json:"id" form:"id" binding:"required"`
	ExpectedVersion int64  `json:"expectedVersion" form:"expectedVersion"`

	Lang string `json:"lang" form:"lang"`
}

type PageBuildJobReq struct {
	ID   string `json:"id" form:"id" binding:"required"`
	Lang string `json:"lang" form:"lang"`

	Intent         string `json:"intent" form:"intent"`
	DraftVersion   int64  `json:"draftVersion" form:"draftVersion"`
	BuildInputHash string `json:"buildInputHash" form:"buildInputHash"`
}

type PublishReq struct {
	ID string `json:"id" form:"id" binding:"required"`

	Lang string `json:"lang" form:"lang"`

	AllLangs bool `json:"allLangs" form:"allLangs"`
}

type RollbackReq struct {
	ID         string `json:"id" form:"id" binding:"required"`
	TargetHash string `json:"targetHash" form:"targetHash" binding:"required"`

	Lang string `json:"lang" form:"lang"`
}

type UpdateURLReq struct {
	ID           string `json:"id" form:"id" binding:"required"`
	NewPath      string `json:"newPath" form:"newPath" binding:"required"`
	WithRedirect bool   `json:"withRedirect" form:"withRedirect"`

	Lang string `json:"lang" form:"lang"`
}

type RebuildArtifactReq struct {
	ArtifactID string `json:"artifactId" form:"artifactId" binding:"required"`
}

type GCArtifactsReq struct {
	RetentionDays int `json:"retentionDays" form:"retentionDays"`

	DryRun *bool `json:"dryRun" form:"dryRun"`
}
