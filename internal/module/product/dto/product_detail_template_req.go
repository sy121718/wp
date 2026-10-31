package productdto

type DetailTemplateModeReapplyReq struct {
	InstanceID string `json:"instanceId" form:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId" form:"projectId"`

	TemplateID string `json:"templateId" form:"templateId"`
}

type DetailTemplateModeRollbackReq struct {
	InstanceID string `json:"instanceId" form:"instanceId" binding:"required"`
	ProjectID  string `json:"projectId" form:"projectId"`
	SnapshotID string `json:"snapshotId" form:"snapshotId" binding:"required"`
}

type DetailTemplateCreateReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ProductID string `json:"productId" form:"productId" binding:"required"`
	Name      string `json:"name" form:"name" binding:"required"`

	CopyFrom string `json:"copyFrom" form:"copyFrom"`
}

type DetailTemplatePublishReq struct {
	ProjectID  string `json:"projectId" form:"projectId"`
	ProductID  string `json:"productId" form:"productId" binding:"required"`
	TemplateID string `json:"templateId" form:"templateId"`
	URLPath    string `json:"urlPath" form:"urlPath"`
}

type DetailTemplateApplyReq struct {
	ProjectID  string `json:"projectId" form:"projectId"`
	ProductID  string `json:"productId" form:"productId" binding:"required"`
	TemplateID string `json:"templateId" form:"templateId"`
}

type DetailTemplateUpdateURLReq struct {
	ProjectID string `json:"projectId" form:"projectId"`
	ProductID string `json:"productId" form:"productId" binding:"required"`
	NewPath   string `json:"newPath" form:"newPath" binding:"required"`

	WithRedirect bool `json:"withRedirect" form:"withRedirect"`
}
