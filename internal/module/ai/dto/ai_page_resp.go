package aidto

type McpPageResp struct {
	Title        string
	Lead         string
	Endpoint     string
	Tokens       []any
	Tools        []any
	ScopeOptions []any
	NewToken     string
	ErrText      string
	DoneText     string

	McpEnabled bool

	McpTone string
}

type McpTokenRowResp struct {
	ID        string
	Name      string
	Scope     string
	CreatedAt any
	LastUsed  any
	Revoked   bool
}

type PickerPageResp struct {
	Title  string
	Picker *PickerResp
}

type PickerResp struct {
	Provider   any
	Candidates []string
	Existing   map[string]bool
}

type SessionEventRowResp struct {
	Seq       int64
	Kind      string
	Tone      string
	Content   string
	Views     []any
	HasViews  bool
	TimeLabel string

	Tool  string
	Phase string

	ViewsCtx *FabViewsCtx
}

type ModelRowResp struct {
	Index           int
	ID              string
	DisplayName     string
	ContextWindow   int64
	MaxOutputTokens int64
	TextChecked     bool
	ImageChecked    bool
}

type ModelsDataResp struct {
	Provider any
	Rows     []ModelRowResp

	ModelsNotice    string
	ModelsNoticeErr bool

	ModelsNoticeTone string
	ModelsNoticeRole string
}

type ProviderCardBase struct {
	CSRFToken string
	T         any
	PermSet   any

	Buttons any
}

type ProviderCardResp struct {
	Provider        any
	Rows            []ModelRowResp
	ProtocolOptions any
	DefaultBaseURL  string
	BuiltinKeys     any
	ModelsNotice    string
	ModelsNoticeErr bool

	StatusTone       string
	ModelsNoticeTone string
	ModelsNoticeRole string

	CSRFToken string `view:"csrf_token"`

	T       any `view:"t"`
	PermSet any `view:"PermSet"`
}
