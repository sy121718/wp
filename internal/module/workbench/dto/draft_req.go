package workbenchdto

type InspectorPanelReq struct {
	NodeID    string `form:"nodeId"`
	Document  string `form:"document"`
	Tab       string `form:"tab"`
	ProjectID string `form:"projectId"`
}

type OutlineTreeReq struct {
	Document   string `form:"document"`
	SelectedID string `form:"selectedId"`
	Filter     string `form:"filter"`
}

type TemplatePreviewDraftReq struct {
	InstanceID    string `form:"instance"`
	ID            string `form:"id"`
	DraftDocument string `form:"draftDocument"`
	ProjectID     string `form:"projectId"`
	EntityType    string `form:"entityType"`
	EntityID      string `form:"entityId"`
}

type NavigationCreateReq struct {
	ProjectID string `form:"projectId"`
	Title     string `form:"title"`
	Path      string `form:"path"`
	Kind      string `form:"kind"`
}

type SEOScoreReq struct {
	DraftDocument string `form:"draftDocument"`
	URL           string `form:"url"`
}

type PreviewDraftReq struct {
	ID              string `form:"id"`
	DraftDocument   string `form:"draftDocument"`
	ExpectedVersion string `form:"expectedVersion"`
}

type BlockPreviewDraftReq struct {
	ID              string `form:"id"`
	ProjectID       string `form:"projectId"`
	DraftDocument   string `form:"draftDocument"`
	ExpectedVersion string `form:"expectedVersion"`
}
