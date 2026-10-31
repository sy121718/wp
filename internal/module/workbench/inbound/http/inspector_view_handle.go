package workbenchhttp

type InspectorPanelResp struct {
	NodeID   string
	NodeType string

	Sections any
}

type OutlineTreeResp struct {
	HTML string
}

type WorkbenchErrorPageResp struct {
	Lang     string `view:"lang"`
	T        any    `view:"t"`
	Title    string `view:"title"`
	Message  string `view:"message"`
	BackPath string `view:"backPath"`
}
