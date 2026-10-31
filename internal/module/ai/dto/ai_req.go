package aidto

type SaveProviderReq struct {
	ID          int64  `json:"id" form:"id"`
	ProviderKey string `json:"providerKey" form:"providerKey"`
	DisplayName string `json:"displayName" form:"displayName"`
	BaseURL     string `json:"baseUrl" form:"baseUrl"`
	Protocol    string `json:"protocol" form:"protocol"`
	APIKey      string `json:"apiKey" form:"apiKey"`
	Status      *int   `json:"status" form:"status"`
	Sort        int    `json:"sort" form:"sort"`
	Version     int64  `json:"version" form:"version"`

	UpdateBy int64 `json:"-"`
}

type SetStatusReq struct {
	ID       int64 `json:"id" form:"id"`
	Status   int   `json:"status" form:"status"`
	Version  int64 `json:"version" form:"version"`
	UpdateBy int64 `json:"-"`
}

type SaveModelsReq struct {
	ProviderID int64        `json:"providerId" form:"providerId"`
	Version    int64        `json:"version" form:"version"`
	Models     []ModelEntry `json:"models" form:"models"`
	UpdateBy   int64        `json:"-"`
}

type ProviderActionReq struct {
	ProviderID int64 `json:"providerId" form:"providerId"`
	Version    int64 `json:"version" form:"version"`
	UpdateBy   int64 `json:"-"`
}

type ChatReq struct {
	ProviderKey     string `json:"provider_key" form:"provider_key" binding:"required,max=50"`
	Model           string `json:"model"        form:"model"        binding:"required,max=200"`
	Input           string `json:"input"        form:"input"        binding:"required,max=200000"`
	MaxOutputTokens int64  `json:"max_output_tokens" form:"max_output_tokens"`

	SessionID int64 `json:"session_id" form:"session_id"`
	UserID    int64 `json:"user_id"    form:"user_id"`

	Messages []ChatMessage `json:"-" form:"-"`

	Tools []ToolSpec `json:"-" form:"-"`
}
