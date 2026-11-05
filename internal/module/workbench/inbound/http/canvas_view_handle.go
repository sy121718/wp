package workbenchhttp

type CanvasPageResp struct {
	Title      string `view:"title"`
	PageID     string `view:"pageId"`
	IsBlock    bool   `view:"isBlock"`
	IsTemplate bool   `view:"isTemplate"`

	IsStructureTemplate bool   `view:"isStructureTemplate"`
	DraftPath           string `view:"draftPath"`
	Version             int64  `view:"version"`

	Document string `view:"document"`
	Meta     string `view:"meta"`
	Schemas  string `view:"schemas"`

	JSVer string `view:"jsVer"`

	PreviewQS string `view:"previewQS"`

	InstanceMode    bool   `view:"instanceMode"`
	RenderMode      string `view:"renderMode"`
	RenderModeLabel string `view:"renderModeLabel"`
}

type DashboardPageResp struct {
	Title string `view:"title"`
	Menu  string

	Overview any
}
