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

	// PreviewURL 是画布 iframe 的初始地址：页面/块/结构模板/实例四种模式由应用决定，
	// 工具布局只消费这个事实。
	PreviewURL string `view:"previewURL"`

	InstanceMode    bool   `view:"instanceMode"`
	RenderMode      string `view:"renderMode"`
	RenderModeLabel string `view:"renderModeLabel"`
}

type DashboardPageResp struct {
	Title string `view:"title"`
	Menu  string

	Overview any
}
