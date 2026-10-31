package masterdatadto

type ChangeResp struct {
	ID              string `json:"id"`
	ProjectID       string `json:"projectId"`
	EntityType      string `json:"entityType"`
	EntityTypeLabel string `json:"entityTypeLabel"`
	EntityID        string `json:"entityId"`

	EntityLabel string `json:"entityLabel"`
	Action      string `json:"action"`
	ActionLabel string `json:"actionLabel"`
	Field       string `json:"field"`
	FieldLabel  string `json:"fieldLabel"`
	OldValue    string `json:"oldValue"`
	NewValue    string `json:"newValue"`
	Origin      string `json:"origin"`
	OperatorID  string `json:"operatorId"`
	CreatedAt   string `json:"createdAt"`
}

type EntityHistoryResp struct {
	EntityType      string `json:"entityType"`
	EntityTypeLabel string `json:"entityTypeLabel"`
	EntityID        string `json:"entityId"`
	EntityLabel     string `json:"entityLabel"`
	ChangeCount     int64  `json:"changeCount"`
	LastAction      string `json:"lastAction"`
	LastActionLabel string `json:"lastActionLabel"`
	LastField       string `json:"lastField"`
	LastFieldLabel  string `json:"lastFieldLabel"`
	LastOperatorID  string `json:"lastOperatorId"`
	LastAt          string `json:"lastAt"`
}

type EntityTimelineResp struct {
	EntityType      string `json:"entityType"`
	EntityTypeLabel string `json:"entityTypeLabel"`
	EntityID        string `json:"entityId"`

	EntityLabel string        `json:"entityLabel"`
	Total       int64         `json:"total"`
	Changes     []*ChangeResp `json:"changes"`
}
