package pagedto

type RedirectListReq struct {
	ProjectID string `form:"project" json:"projectId"`
}

type RedirectCreateReq struct {
	ProjectID string `form:"project" json:"projectId" binding:"required"`

	SourcePath string `form:"sourcePath" json:"sourcePath" binding:"required"`

	TargetPath string `form:"targetPath" json:"targetPath" binding:"required"`
}

type RedirectDeleteReq struct {
	ProjectID string `form:"project" json:"projectId" binding:"required"`
	Path      string `form:"path" json:"path" binding:"required"`
}

type RedirectMergeReq struct {
	ProjectID string `form:"project" json:"projectId" binding:"required"`
	Path      string `form:"path" json:"path" binding:"required"`
}
