package aidto

type AppendEventReq struct {
	SessionKey string
	SessionID  int64
	Kind       string
	Content    string

	Tokens int64
	Meta   map[string]any
	UserID int64

	ProviderKey string
	ModelID     string

	Title string
}

type FoldPlanReq struct {
	SessionID int64

	KeepRecent int
}

type FoldReq struct {
	SessionID int64
	FromSeq   int64
	ToSeq     int64
	Summary   string

	SummaryTokens int64
	UserID        int64
}

type RenameSessionReq struct {
	ID          int64
	Title       string
	ProviderKey string
	ModelID     string
	Status      int16
	Version     int64
	UserID      int64
}

type SendMessageReq struct {
	SessionID   int64  `json:"sessionId" form:"sessionId"`
	SessionKey  string `json:"sessionKey" form:"sessionKey"`
	ProviderKey string `json:"providerKey" form:"providerKey" binding:"max=50"`
	Model       string `json:"model" form:"model" binding:"max=200"`
	Input       string `json:"input" form:"input" binding:"max=200000"`

	UserText string `json:"-"`

	Images []string `json:"images,omitempty" form:"images"`

	ImageLabels     []string `json:"imageLabels,omitempty" form:"imageLabels"`
	MaxOutputTokens int64    `json:"maxOutputTokens" form:"maxOutputTokens"`
	UserID          int64    `json:"-"`
}
