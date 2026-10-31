package aidto

type FabMsgFact struct {
	Key      string
	Fallback string
	Args     []string
}

type FabResultView struct {
	FabError FabMsgFact

	FabAskKey string

	FabAnswer    string
	FabSessionID int64

	FabReasoning string

	FabTruncatedText string

	FabViewsCtx *FabViewsCtx

	FabToolsCount  int
	FabToolsPrefix string
	FabToolsSuffix string
	FabThinkLabel  string
	FabEmptyText   string
}

type FabViewsCtx struct {
	Views []any
}

type FabErrorView struct {
	FabError  FabMsgFact
	FabAskKey string
}
