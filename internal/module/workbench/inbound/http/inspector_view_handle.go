package workbenchhttp

type InspectorPanelResp struct {
	NodeID   string
	NodeType string

	Sections any
}

type OutlineTreeResp struct {
	HTML string
}
